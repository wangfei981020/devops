package handlers

import (
	"database/sql"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/logx"
)

// ─────────────────────────────────────────────────────────────────
// 域名访问质量：台账 vs **客户端实际拿到的东西**（OPSCMDB-035）。
//
// # 这一层只有 CMDB 做得了
//
//	Grafana / blackbox  看得到客户端**实际拿到**的证书，看不到我们登记的是什么
//	CMDB                看得到台账，看不到客户端到底拿到了什么
//
//	两边对不上 = 出事了，而且是最难查的那种：
//	  · 证书换了但没生效 —— 台账是新的，客户端还在拿旧的
//	  · 改在了 NS 没指向的那一方 —— CMDB 给原因，blackbox 给证据，
//	    单独任何一边都只是猜
//
// 🔴 **不做监控墙**。判据会打架：监控墙问「现在这一秒怎么样」，
//
//	CMDB 问「这份台账有多旧」。同一页上半屏写"实时 5s 前"、
//	下半屏写"主机数据 49 小时没更新"，读的人不知道该信哪个。
//	而且夜莺 + OpsAlert 已经在做告警了，再做一个就是第三份真相。
//
// # ⚠️ 前置条件（做之前实测确认过，不成立就不该做）
//
//	blackbox 的 instance 是**完整 URL**（https://game.dragontiger-game.com），
//	主机名可机械提取并按后缀匹配台账根域名 —— **不需要人工映射表**。
//	（实测：dragontiger-game.com / g01prod.com / mopaxdruen.com 等逐个对得上。）
//
//	🔴 如果哪天 target 改成了对不上的形式，正确的做法是**停掉这个功能**，
//	而不是加一张「指标 instance ↔ 台账域名」的手工映射表：
//	那种表一定会烂（人加了域名不会记得来改表），烂掉之后的表现是
//	**对账结果凭空少几条，而界面上看不出来** —— 又一个"不报错只是答非所问"。
// ─────────────────────────────────────────────────────────────────

type DomainQualityHandler struct {
	DB *sql.DB
	*ObsQueryHandler
}

// probeTarget 一个拨测目标与它对上的台账域名。
type probeTarget struct {
	// Instance blackbox 的原始 instance（完整 URL），原样给出 —— 人要拿它去 Grafana 里对
	Instance string `json:"instance"`
	Host     string `json:"host"`
	// RootDomain 匹配到的台账根域名。空 = 台账里没有这个域名（失管）
	RootDomain string `json:"root_domain,omitempty"`
	// CertExpiryAt 客户端**实际拿到**的证书到期时刻。
	// ⚠️ null = 这个目标没有 TLS 拨测（http:// 的目标），不是"证书没到期"
	CertExpiryAt *string `json:"cert_expiry_at,omitempty"`
	CertDaysLeft *int    `json:"cert_days_left,omitempty"`
	// ProbeOK 最近一次拨测成功与否。⚠️ null = 没采到，与 false（探测失败）不同
	ProbeOK *bool `json:"probe_ok,omitempty"`
}

// DomainQuality GET /api/domains/quality
//
// 三个桶，各自回答一个不同的问题 —— 合成一个数字就什么都答不了：
//
//	matched     拨测到了、台账里也有 → 可以对账
//	unledgered  拨测到了、台账里没有 → **失管域名**，有人在跑但没人登记
//	unprobed    台账里有解析、但没有任何拨测 → **监控盲区**
func (h *DomainQualityHandler) DomainQuality(c *gin.Context) {
	base, token, _, err := resolveEndpointFull(h.DB, h.Cipher, "prometheus", anyEnv, anyCluster)
	if err != nil || base == "" {
		// 「没接数据源」和「没有拨测数据」必须分开：前者要去配数据源，
		// 后者说明 blackbox 没在采。混成一句"暂无数据"两件事都没法处置
		c.JSON(http.StatusOK, gin.H{
			"ok": false, "configured": false,
			"hint": "尚未接入 Prometheus/VictoriaMetrics 数据源，拿不到拨测数据。" +
				"请到「管理 → 观测端点」添加一个 type=prometheus 的接入点",
		})
		return
	}

	// 客户端实际拿到的证书到期时刻（秒级 unix）
	certRows, certErr := promInstant(base, token, `probe_ssl_earliest_cert_expiry`)
	// 拨测成功与否
	okRows, okErr := promInstant(base, token, `probe_success`)
	if certErr != nil && okErr != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"ok": false, "configured": true,
			"error": "拉取拨测指标失败：" + certErr.Error(),
		})
		return
	}

	// 台账根域名集合
	ledger := map[string]bool{}
	if rows, e := h.DB.Query(`SELECT c.name FROM cis c WHERE c.type='domain'`); e == nil {
		defer rows.Close()
		for rows.Next() {
			var n string
			if rows.Scan(&n) == nil && n != "" {
				ledger[strings.ToLower(n)] = true
			}
		}
	}

	now := time.Now()
	byHost := map[string]*probeTarget{}
	get := func(instance string) *probeTarget {
		host := hostFromProbeInstance(instance)
		if host == "" {
			return nil
		}
		if t := byHost[host]; t != nil {
			return t
		}
		t := &probeTarget{Instance: instance, Host: host, RootDomain: matchRootDomain(host, ledger)}
		byHost[host] = t
		return t
	}

	for _, s := range certRows {
		t := get(s.Metric["instance"])
		if t == nil || s.Value <= 0 {
			continue
		}
		exp := time.Unix(int64(s.Value), 0)
		str := exp.Format(time.RFC3339)
		days := int(exp.Sub(now).Hours() / 24)
		t.CertExpiryAt, t.CertDaysLeft = &str, &days
	}
	for _, s := range okRows {
		t := get(s.Metric["instance"])
		if t == nil {
			continue
		}
		v := s.Value == 1
		t.ProbeOK = &v
	}

	matched := []*probeTarget{}
	unledgered := []*probeTarget{}
	probedRoots := map[string]bool{}
	for _, t := range byHost {
		if t.RootDomain != "" {
			matched = append(matched, t)
			probedRoots[t.RootDomain] = true
		} else {
			unledgered = append(unledgered, t)
		}
	}

	// 台账里有解析、却没有任何拨测的域名 = 监控盲区。
	// ⚠️ 只算**有解析记录**的：已迁走/未使用的域名没有拨测是正常的，
	//	把它们也算成盲区会让这个数字失去意义（噪音多了人就不看了）。
	unprobed := []string{}
	if rows, e := h.DB.Query(`SELECT c.name, (SELECT COUNT(*) FROM dns_records dr WHERE dr.domain_ci_id=c.id)
	                          FROM cis c WHERE c.type='domain'`); e == nil {
		defer rows.Close()
		for rows.Next() {
			var n string
			var n2 int
			if rows.Scan(&n, &n2) == nil && n2 > 0 && !probedRoots[strings.ToLower(n)] {
				unprobed = append(unprobed, n)
			}
		}
	}

	// 排序：证书快到期的排最前，这一页的用途就是"先看哪个"
	sort.SliceStable(matched, func(i, j int) bool {
		a, b := matched[i].CertDaysLeft, matched[j].CertDaysLeft
		if a == nil {
			return false
		}
		if b == nil {
			return true
		}
		return *a < *b
	})
	sort.Strings(unprobed)
	sort.SliceStable(unledgered, func(i, j int) bool { return unledgered[i].Host < unledgered[j].Host })

	expiring := 0
	failing := 0
	for _, t := range matched {
		if t.CertDaysLeft != nil && *t.CertDaysLeft <= 30 {
			expiring++
		}
		if t.ProbeOK != nil && !*t.ProbeOK {
			failing++
		}
	}

	logx.J("domain_quality", "recon", map[string]any{
		"probed": len(byHost), "matched": len(matched),
		"unledgered": len(unledgered), "unprobed": len(unprobed),
		"cert_expiring_30d": expiring, "probe_failing": failing,
	})

	out := gin.H{
		"ok": true, "configured": true,
		"summary": gin.H{
			"probed": len(byHost), "matched": len(matched),
			"unledgered": len(unledgered), "unprobed": len(unprobed),
			"cert_expiring_30d": expiring, "probe_failing": failing,
		},
		"matched":    matched,
		"unledgered": unledgered,
		"unprobed":   unprobed,
	}
	if len(byHost) == 0 {
		// 数据源接了但一个拨测目标都没有 —— 说明 blackbox 没在采，
		// 不是"域名都很健康"。这两件事的下一步完全不同
		out["empty_hint"] = "数据源里没有任何 probe_* 指标。这说明 blackbox exporter 没在采集，" +
			"而不是「所有域名都正常」。先确认 blackbox 的 job 是否已配置并被抓取。"
	}
	c.JSON(http.StatusOK, out)
}

// hostFromProbeInstance 从 blackbox 的 instance 里取主机名。
//
// instance 形如 `https://game.dragontiger-game.com/config.js`、
// `https://crav.onstpleihdky.com:7855`，也可能是纯 `host:port`（TCP 拨测）。
//
// ⚠️ 取不出来时返回空串并**丢掉这一条**，不要瞎猜：
//
//	猜错的结果是把 A 域名的证书算到 B 域名头上，而那不会报错。
func hostFromProbeInstance(instance string) string {
	s := strings.TrimSpace(instance)
	if s == "" {
		return ""
	}
	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err != nil {
			return ""
		}
		return strings.ToLower(u.Hostname())
	}
	// host:port —— TCP 拨测的形态
	if i := strings.LastIndex(s, ":"); i > 0 {
		s = s[:i]
	}
	// 纯 IP 的目标没有域名可对，直接丢掉
	if strings.Count(s, ".") == 3 && strings.IndexFunc(s, func(r rune) bool {
		return r != '.' && (r < '0' || r > '9')
	}) < 0 {
		return ""
	}
	return strings.ToLower(s)
}

// matchRootDomain 把 FQDN 按后缀对到台账里的根域名。
//
// ⚠️ 必须是**后缀边界**匹配（`.` 开头），不能用 strings.Contains：
//
//	`evil-g01prod.com` 会 Contains 到 `g01prod.com`，
//	于是别人的域名被算成我们的 —— 而对账结果看起来完全正常。
func matchRootDomain(host string, ledger map[string]bool) string {
	if ledger[host] {
		return host
	}
	parts := strings.Split(host, ".")
	for i := 1; i < len(parts)-1; i++ {
		cand := strings.Join(parts[i:], ".")
		if ledger[cand] {
			return cand
		}
	}
	return ""
}

func NewDomainQualityHandler(db *sql.DB, obs *ObsQueryHandler) *DomainQualityHandler {
	return &DomainQualityHandler{DB: db, ObsQueryHandler: obs}
}

func (h *DomainQualityHandler) Register(r *gin.RouterGroup) {
	r.GET("/domains/quality", h.DomainQuality)
}
