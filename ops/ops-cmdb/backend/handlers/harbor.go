package handlers

import (
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"ops-cmdb-backend/internal/httpx"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/crypto"
	"ops-cmdb-backend/internal/store"
	"ops-cmdb-backend/logx"
)

// Harbor 接入（只读）。
//
// 补 CMDB-004 的环节 6（推送 Harbor）与环节 8（拉取镜像）：
//   - 配额快满了 → 推送会失败，此前只能等发布报错才知道
//   - Harbor 自身不健康 → ImagePullBackOff，此前分不清是凭证问题还是仓库挂了
//     （配合 config_audit 就能分清：拉取密钥缺失 vs Harbor 组件异常）
//   - GC 有没有在回收 → 存储只涨不降时的第一嫌疑
//
// 全部实时查询、不落表：配额和健康状态变化快，落表的快照反而会让人拿着旧数用。

const harborTimeout = 20 * time.Second

type HarborHandler struct {
	// 迁移期同时持有：Store 用于有租户语义的读写，DB 供尚未迁移的内部函数。
	Store  *store.Store
	DB     *sql.DB
	Cipher *crypto.Cipher
}

func NewHarborHandler(st *store.Store, db *sql.DB, cipher *crypto.Cipher) *HarborHandler {
	return &HarborHandler{Store: st, DB: db, Cipher: cipher}
}

// Register 只读查询（供 MCP 与前端展示）。
func (h *HarborHandler) Register(r *gin.RouterGroup) {
	r.GET("/harbor/status", h.Status)             // registry_id? → 健康 + 存储统计 + GC
	r.GET("/harbor/projects", h.Projects)         // registry_id? → 项目 + 配额用量
	r.GET("/harbor/repositories", h.Repositories) // registry_id?, project
}

// RegisterAdmin 接入配置管理（登录态）。
func (h *HarborHandler) RegisterAdmin(r *gin.RouterGroup) {
	r.GET("/harbor/registries", h.List)
	r.POST("/harbor/registries", h.Save)
	r.PUT("/harbor/registries/:id", h.Save)
	r.DELETE("/harbor/registries/:id", h.Delete)
	r.POST("/harbor/registries/:id/test", h.Test)
}

type harborReg struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	URL        string `json:"url"`
	Username   string `json:"username"`
	Env        string `json:"env"`
	ClusterID  int    `json:"cluster_id"`
	SkipVerify bool   `json:"skip_verify"`
	Enabled    bool   `json:"enabled"`
	HasSecret  bool   `json:"has_secret"` // 只暴露"有没有配密码"，绝不回传密文或明文
}

func (h *HarborHandler) List(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	rows, err := sc.Query(`SELECT id,name,url,username,env,cluster_id,skip_verify,enabled,
		CASE WHEN password_enc IS NULL OR password_enc='' THEN 0 ELSE 1 END
		FROM harbor_registries WHERE tenant_id = ? ORDER BY id`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []harborReg{}
	for rows.Next() {
		var r harborReg
		var skip, en, has int
		if rows.Scan(&r.ID, &r.Name, &r.URL, &r.Username, &r.Env, &r.ClusterID, &skip, &en, &has) != nil {
			continue
		}
		r.SkipVerify, r.Enabled, r.HasSecret = skip == 1, en == 1, has == 1
		out = append(out, r)
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func (h *HarborHandler) Save(c *gin.Context) {
	var in struct {
		Name       string `json:"name"`
		URL        string `json:"url"`
		Username   string `json:"username"`
		Password   string `json:"password"`
		Env        string `json:"env"`
		ClusterID  int    `json:"cluster_id"`
		SkipVerify bool   `json:"skip_verify"`
		Enabled    *bool  `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	in.URL = strings.TrimRight(strings.TrimSpace(in.URL), "/")
	if in.Name == "" || in.URL == "" {
		httpx.RequiredAll(c, "name", "url")
		return
	}
	enabled := 1
	if in.Enabled != nil && !*in.Enabled {
		enabled = 0
	}
	skip := 0
	if in.SkipVerify {
		skip = 1
	}

	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	if id == 0 {
		enc := ""
		if in.Password != "" {
			var err error
			if enc, err = h.Cipher.Encrypt(in.Password); err != nil {
				httpx.Fail(c, httpx.CodeInternal, fmt.Errorf("加密失败: %w", err), nil)
				return
			}
		}
		res, err := sc.Insert(`INSERT INTO harbor_registries(tenant_id,name,url,username,password_enc,env,cluster_id,skip_verify,enabled)
			VALUES(?,?,?,?,?,?,?,?,?)`, in.Name, in.URL, in.Username, nullIfEmpty(enc), in.Env, in.ClusterID, skip, enabled)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		nid, _ := res.LastInsertId()
		AuditCreated(c, "harbor_registries", nid)
		c.JSON(http.StatusOK, gin.H{"ok": true, "id": nid})
		return
	}

	// 密码留空 = 不改动已存的密码。否则每次编辑其它字段都得重输密码。
	if in.Password == "" {
		// ★ 越权修复：原为 `WHERE id=?`，任何租户都能改别人的镜像仓库配置
		_, err := sc.Exec(`UPDATE harbor_registries SET name=?,url=?,username=?,env=?,cluster_id=?,skip_verify=?,enabled=? WHERE tenant_id = ? AND id=?`,
			in.Name, in.URL, in.Username, in.Env, in.ClusterID, skip, enabled, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	} else {
		enc, err := h.Cipher.Encrypt(in.Password)
		if err != nil {
			httpx.Fail(c, httpx.CodeInternal, fmt.Errorf("加密失败: %w", err), nil)
			return
		}
		if _, err := sc.Exec(`UPDATE harbor_registries SET name=?,url=?,username=?,password_enc=?,env=?,cluster_id=?,skip_verify=?,enabled=? WHERE tenant_id = ? AND id=?`,
			in.Name, in.URL, in.Username, enc, in.Env, in.ClusterID, skip, enabled, id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "id": id})
}

func (h *HarborHandler) Delete(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	res, err := sc.Exec(`DELETE FROM harbor_registries WHERE tenant_id = ? AND id=?`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.NotFound(c, "registry")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Test 连通性验证。用 /api/v2.0/systeminfo 而非根路径——根路径 200 只能证明有个 HTTP 服务在，
// 证明不了它是 Harbor、更证明不了凭证有效。
func (h *HarborHandler) Test(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	cl, err := h.clientByID(id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	code, body, err := cl.get("/systeminfo")
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "连接失败: " + err.Error(), "error_key": "error.connectFailed"})
		return
	}
	if code == 401 || code == 403 {
		c.JSON(http.StatusOK, gin.H{"ok": false,
			"error": fmt.Sprintf("认证失败 (HTTP %d)：用户名或密码不对。机器人账号的用户名要带 robot$ 前缀", code), "error_key": "error.harborAuthFailed", "error_params": gin.H{"status": code}})
		return
	}
	if code != 200 {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("HTTP %d", code), "detail": truncate(body, 200)})
		return
	}
	var info struct {
		HarborVersion string `json:"harbor_version"`
		AuthMode      string `json:"auth_mode"`
	}
	_ = json.Unmarshal([]byte(body), &info)

	// 连得上不代表读得到想读的东西：Harbor 的机器人账号权限是分项的，
	// 只给了系统权限而没给项目权限时 /projects 会返回空，那才是真正影响用途的。
	pcode, pbody, _ := cl.get("/projects?page_size=1")
	readable := pcode == 200 && strings.TrimSpace(pbody) != "[]" && strings.TrimSpace(pbody) != "null"
	out := gin.H{"ok": true, "harbor_version": info.HarborVersion, "auth_mode": info.AuthMode,
		"projects_readable": readable}
	if !readable {
		out["warn"] = fmt.Sprintf("凭证有效，但读不到任何项目（/projects HTTP %d）。"+
			"请给该账号加「项目只读」权限，否则配额和仓库信息都查不到", pcode)
	}
	c.JSON(http.StatusOK, out)
}

// ---------- Harbor API 客户端 ----------

type harborClient struct {
	base, user, pass string
	skipVerify       bool
	name             string
}

func (c *harborClient) get(path string) (int, string, error) {
	req, err := http.NewRequest("GET", c.base+"/api/v2.0"+path, nil)
	if err != nil {
		return 0, "", err
	}
	if c.user != "" {
		req.SetBasicAuth(c.user, c.pass)
	}
	req.Header.Set("Accept", "application/json")
	cl := &http.Client{Timeout: harborTimeout}
	if c.skipVerify {
		cl.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}
	resp, err := cl.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return resp.StatusCode, string(b), nil
}

func (h *HarborHandler) clientByID(id int) (*harborClient, error) {
	q := `SELECT id,name,url,username,COALESCE(password_enc,''),skip_verify FROM harbor_registries WHERE enabled=1`
	args := []any{}
	if id > 0 {
		q += " AND id=?"
		args = append(args, id)
	}
	q += " ORDER BY id LIMIT 1"
	var rid, skip int
	var name, u, user, enc string
	if err := h.DB.QueryRow(q, args...).Scan(&rid, &name, &u, &user, &enc, &skip); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("没有可用的 Harbor 接入配置（未添加或未启用）")
		}
		return nil, err
	}
	pass := ""
	if enc != "" {
		var err error
		if pass, err = h.Cipher.Decrypt(enc); err != nil {
			return nil, fmt.Errorf("解密凭证失败: %w", err)
		}
	}
	return &harborClient{base: strings.TrimRight(u, "/"), user: user, pass: pass, skipVerify: skip == 1, name: name}, nil
}

func (h *HarborHandler) client(c *gin.Context) (*harborClient, bool) {
	id, _ := strconv.Atoi(c.Query("registry_id"))
	cl, err := h.clientByID(id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return nil, false
	}
	return cl, true
}

// ---------- 只读查询 ----------

// Status 汇总 Harbor 是否健康、存了多少、GC 有没有在回收。
func (h *HarborHandler) Status(c *gin.Context) {
	cl, ok := h.client(c)
	if !ok {
		return
	}
	out := gin.H{"ok": true, "registry": cl.name, "url": cl.base}

	// 健康：逐组件列出。整体 healthy 但某个组件挂了的情况必须能看见。
	if code, body, err := cl.get("/health"); err == nil && code == 200 {
		var hr struct {
			Status     string `json:"status"`
			Components []struct {
				Name   string `json:"name"`
				Status string `json:"status"`
				Error  string `json:"error"`
			} `json:"components"`
		}
		if json.Unmarshal([]byte(body), &hr) == nil {
			unhealthy := []gin.H{}
			for _, comp := range hr.Components {
				if !strings.EqualFold(comp.Status, "healthy") {
					unhealthy = append(unhealthy, gin.H{"name": comp.Name, "status": comp.Status, "error": comp.Error})
				}
			}
			out["health"] = hr.Status
			out["component_count"] = len(hr.Components)
			out["unhealthy_components"] = unhealthy
			if len(unhealthy) > 0 {
				out["issue"] = fmt.Sprintf("%d 个组件不健康——推拉镜像可能随机失败", len(unhealthy))
			}
		}
	} else {
		out["health"] = "unknown"
		out["health_error"] = fmt.Sprintf("取健康状态失败 (HTTP %d): %v", code, err)
	}

	// ⚠️ /statistics 统计的是**当前账号可见范围**，而且 total_* 这几个字段
	// 对非管理员账号会返回 0。用 robot 账号接入时（推荐做法）就会出现
	// 「projects: 0 / repositories: 0」而 /projects 明明能列出一堆的矛盾——
	// 生产实测：status 说 0/0，harbor_projects 返回 15 个项目 576 个仓库。
	//
	// 🔴 同一个数据源的两个接口给出相反结论，是最难排查的一类问题：
	// 看 status 的人以为仓库是空的，看 projects 的人以为一切正常，谁也不会去怀疑对方。
	// 所以这里拿不到可信值时**不填 0**，而是明说这个数字取不到、该看哪里。
	statsOK := false
	if code, body, err := cl.get("/statistics"); err == nil && code == 200 {
		var st struct {
			TotalProjectCount int   `json:"total_project_count"`
			TotalRepoCount    int   `json:"total_repo_count"`
			TotalStorage      int64 `json:"total_storage_consumption"`
		}
		if json.Unmarshal([]byte(body), &st) == nil &&
			(st.TotalProjectCount > 0 || st.TotalRepoCount > 0 || st.TotalStorage > 0) {
			out["projects"] = st.TotalProjectCount
			out["repositories"] = st.TotalRepoCount
			out["storage_used_gb"] = round2(float64(st.TotalStorage) / (1 << 30))
			statsOK = true
		}
	}
	if !statsOK {
		// 退回到按项目列表实测——robot 账号读得到 /projects，只是读不到全局 statistics
		if n, repos, ok := cl.countByProjects(); ok {
			out["projects"] = n
			out["repositories"] = repos
			out["stats_source"] = "按 /projects 逐项统计（该账号读不到 /statistics 的全局数字）"
		} else {
			// 🔴 取不到就说取不到，不要填 0 —— 0 会被读成「仓库是空的」
			out["projects"] = nil
			out["repositories"] = nil
			out["stats_note"] = "读不到项目统计（/statistics 需要管理员权限，robot 账号通常没有）。" +
				"⚠️ 这不代表仓库是空的，用 harbor_projects 看真实项目与仓库数"
		}
		out["storage_used_gb"] = nil
		out["storage_note"] = "存储用量需要管理员权限才能读，robot 账号取不到"
	}

	// GC：存储只涨不降时的第一嫌疑。Harbor 不跑 GC，删了 tag 也不会释放磁盘。
	if code, body, err := cl.get("/system/gc?page_size=1"); err == nil && code == 200 {
		var gc []struct {
			ID           int    `json:"id"`
			JobStatus    string `json:"job_status"`
			UpdateTime   string `json:"update_time"`
			CreationTime string `json:"creation_time"`
		}
		if json.Unmarshal([]byte(body), &gc) == nil && len(gc) > 0 {
			g := gin.H{"last_status": gc[0].JobStatus, "last_time": gc[0].UpdateTime}
			if age := daysSince(gc[0].UpdateTime); age >= 0 {
				g["days_ago"] = age
				if age > 30 {
					g["issue"] = fmt.Sprintf("上次 GC 是 %d 天前——删掉的镜像不跑 GC 不会真正释放磁盘", age)
				}
			}
			out["gc"] = g
		} else {
			out["gc"] = gin.H{"last_status": "never", "issue": "从未执行过 GC——删掉的镜像一直占着磁盘"}
		}
	}
	c.JSON(http.StatusOK, out)
}

// Projects 项目 + 配额用量，按用量比例倒序——快满的排前面。
func (h *HarborHandler) Projects(c *gin.Context) {
	cl, ok := h.client(c)
	if !ok {
		return
	}
	code, body, err := cl.get("/projects?page_size=100&with_detail=true")
	if err != nil || code != 200 {
		c.JSON(http.StatusOK, gin.H{"ok": false,
			"error": fmt.Sprintf("取项目列表失败 (HTTP %d): %v", code, err), "error_key": "error.harborListProjectsFailed", "error_params": gin.H{"status": code}})
		return
	}
	var ps []struct {
		ProjectID int    `json:"project_id"`
		Name      string `json:"name"`
		RepoCount int    `json:"repo_count"`
		Metadata  struct {
			Public string `json:"public"`
		} `json:"metadata"`
	}
	if json.Unmarshal([]byte(body), &ps) != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "解析项目列表失败", "error_key": "error.harborParseProjectsFailed"})
		return
	}

	// 配额单独一个接口，一次拿全再按 project 对应，避免逐项目请求。
	//
	// ⚠️ **取不到配额和"用量为 0"必须分开。**
	//
	//	/quotas 需要管理员权限，而接入 Harbor 推荐用 robot 账号（最小权限，
	//	那是正确做法）—— 于是这个接口对 robot 返回 403，quota 表是空的。
	//	原来项目级 UsedGB 是 float64，取不到就保持零值，界面显示「0 GB」：
	//	  g32      0 GB   未设配额   198 个仓库
	//	  library  0 GB   未设配额   166 个仓库
	//	198 个仓库、每个 20 个版本的项目，存储用量不可能是 0（OPSCMDB-031 P1-25）。
	//
	//	而「0 GB + 未设配额」的组合尤其误导：读起来是"这个项目没占空间、也没有限制"。
	//
	//	⚠️ 全局那处（storage_used_gb）在 v0.80.0 已经改成"取不到就给 null"，
	//	但项目级是**另一处**，没被覆盖到 —— 同一个判断在两个地方各写一份的老问题。
	quotaOK := false
	quota := map[int]struct{ used, hard int64 }{}
	if qc, qb, qe := cl.get("/quotas?page_size=100"); qe == nil && qc == 200 {
		quotaOK = true
		var qs []struct {
			Ref struct {
				ID int `json:"id"`
			} `json:"ref"`
			Hard map[string]int64 `json:"hard"`
			Used map[string]int64 `json:"used"`
		}
		if json.Unmarshal([]byte(qb), &qs) == nil {
			for _, q := range qs {
				quota[q.Ref.ID] = struct{ used, hard int64 }{q.Used["storage"], q.Hard["storage"]}
			}
		}
	}

	type projOut struct {
		Name      string `json:"name"`
		RepoCount int    `json:"repo_count"`
		Public    bool   `json:"public"`
		// UsedGB 存储用量。⚠️ **指针**：nil = 取不到（robot 账号没有配额读权限），
		// 不是"占了 0 GB"。零值会被读成"这个项目是空的"
		UsedGB   *float64 `json:"used_gb"`
		QuotaGB  float64  `json:"quota_gb"` // -1 = 不限
		UsedPct  float64  `json:"used_pct"` // -1 = 不限，无从计算
		Severity string   `json:"severity"` // 快满时才有
		Issue    string   `json:"issue,omitempty"`
	}
	out := make([]projOut, 0, len(ps))
	for _, p := range ps {
		o := projOut{Name: p.Name, RepoCount: p.RepoCount, Public: p.Metadata.Public == "true", QuotaGB: -1, UsedPct: -1}
		if q, ok := quota[p.ProjectID]; ok {
			used := round2(float64(q.used) / (1 << 30))
			o.UsedGB = &used
			if q.hard > 0 {
				o.QuotaGB = round2(float64(q.hard) / (1 << 30))
				o.UsedPct = round2(float64(q.used) / float64(q.hard) * 100)
				switch {
				case o.UsedPct >= 90:
					o.Severity, o.Issue = "high", "配额即将耗尽，推送很快会失败"
				case o.UsedPct >= 75:
					o.Severity, o.Issue = "medium", "配额用量偏高，建议清理旧 tag 并执行 GC"
				}
			}
		}
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UsedPct > out[j].UsedPct })
	resp := gin.H{"ok": true, "registry": cl.name, "count": len(out), "projects": out,
		"note": "quota_gb = -1 表示该项目未设配额限制，用量再高也不会被 Harbor 拦；" +
			"此时应看整体存储水位而非百分比"}
	// ⚠️ 读不到配额时要**在响应里说出来**。
	//	不说的话，界面上一列 null 会被当成"这些项目都是空的"，
	//	而真相是这个账号没有权限读（P1-25）。
	if !quotaOK {
		resp["quota_note"] = "读不到配额与存储用量（/quotas 需要管理员权限，" +
			"而接入 Harbor 推荐用 robot 账号，那是正确做法）。" +
			"⚠️ 用量为空**不代表项目是空的** —— 仓库数是真实的，存储数字只是这个账号看不到"
		resp["quota_readable"] = false
	} else {
		resp["quota_readable"] = true
	}
	c.JSON(http.StatusOK, resp)
}

// Repositories 某项目下的仓库列表（含 tag 数与更新时间），用来确认「镜像到底推上去没有」。
func (h *HarborHandler) Repositories(c *gin.Context) {
	project := strings.TrimSpace(c.Query("project"))
	if project == "" {
		httpx.Required(c, "project")
		return
	}
	cl, ok := h.client(c)
	if !ok {
		return
	}
	code, body, err := cl.get("/projects/" + url.PathEscape(project) + "/repositories?page_size=100")
	if err != nil || code != 200 {
		msg := fmt.Sprintf("取仓库列表失败 (HTTP %d): %v", code, err)
		if code == 404 {
			msg = "项目不存在或无权访问：" + project
		}
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": msg})
		return
	}
	var rs []struct {
		Name          string `json:"name"`
		ArtifactCount int    `json:"artifact_count"`
		PullCount     int    `json:"pull_count"`
		UpdateTime    string `json:"update_time"`
	}
	if json.Unmarshal([]byte(body), &rs) != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "解析仓库列表失败", "error_key": "error.harborParseReposFailed"})
		return
	}
	items := make([]gin.H, 0, len(rs))
	for _, r := range rs {
		it := gin.H{"name": r.Name, "artifacts": r.ArtifactCount, "pulls": r.PullCount, "updated": r.UpdateTime}
		if d := daysSince(r.UpdateTime); d >= 0 {
			it["days_since_push"] = d
		}
		items = append(items, it)
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "project": project, "count": len(items), "repositories": items})
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// daysSince 解析 Harbor 的 RFC3339 时间，返回距今天数；解析不了返回 -1（而不是 0——
// 0 会被读成"今天刚发生"，是个会误导人的默认值）。
func daysSince(ts string) int {
	if ts == "" {
		return -1
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		logx.J("harbor", "time_parse_fail", map[string]any{"value": ts, "warn": "无法解析 Harbor 返回的时间格式"})
		return -1
	}
	return int(time.Since(t).Hours() / 24)
}

// countByProjects 用 /projects 逐项统计项目数与仓库数。
//
// 为什么需要：/statistics 的 total_* 对非管理员返回 0，而接入 Harbor 推荐用
// robot 账号（最小权限）。此时 /projects 是读得到的，repo_count 就在里面。
func (c *harborClient) countByProjects() (projects, repos int, ok bool) {
	code, body, err := c.get("/projects?page_size=100")
	if err != nil || code != 200 {
		return 0, 0, false
	}
	var list []struct {
		RepoCount int `json:"repo_count"`
	}
	if json.Unmarshal([]byte(body), &list) != nil {
		return 0, 0, false
	}
	for _, p := range list {
		repos += p.RepoCount
	}
	return len(list), repos, true
}
