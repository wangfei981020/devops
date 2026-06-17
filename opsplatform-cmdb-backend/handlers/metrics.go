package handlers

import (
	"database/sql"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

// cmdbCollector 每次 scrape 查 DB 输出证书/域名到期与创建时间 gauge。
// 固定维度 project/env/module/name/cn/ca/domain/registrar；自定义 label 仅导出 settings 白名单内的 key（控高基数）。
type cmdbCollector struct {
	db *sql.DB
}

func NewCMDBCollector(db *sql.DB) prometheus.Collector { return &cmdbCollector{db: db} }

func (c *cmdbCollector) Describe(ch chan<- *prometheus.Desc) {}

// 固定维度集合（白名单里这些即便列出也不算"额外自定义 label"）
var fixedLabelKeys = map[string]bool{
	"project": true, "env": true, "module": true, "name": true,
	"cn": true, "ca": true, "domain": true, "registrar": true,
}

func (c *cmdbCollector) exportKeys() []string {
	var raw string
	_ = c.db.QueryRow(`SELECT v FROM settings WHERE k='export_label_whitelist'`).Scan(&raw)
	var keys []string
	for _, k := range strings.Split(raw, ",") {
		k = strings.TrimSpace(k)
		if k != "" && !fixedLabelKeys[k] {
			keys = append(keys, k)
		}
	}
	return keys
}

// 一次性加载所有 CI 的自定义标签
func (c *cmdbCollector) allLabels() map[int64]map[string]string {
	m := map[int64]map[string]string{}
	rows, err := c.db.Query(`SELECT ci_id, k, v FROM ci_labels`)
	if err != nil {
		return m
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var k, v string
		if rows.Scan(&id, &k, &v) == nil {
			if m[id] == nil {
				m[id] = map[string]string{}
			}
			m[id][k] = v
		}
	}
	return m
}

func customVals(labels map[string]string, keys []string) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = labels[k]
	}
	return out
}

func (c *cmdbCollector) Collect(ch chan<- prometheus.Metric) {
	exportKeys := c.exportKeys()
	labelsByCI := c.allLabels()

	// ---- 证书：到期 + 创建 ----
	certNames := append([]string{"project", "env", "module", "name", "cn", "ca", "domain"}, exportKeys...)
	expiryDesc := prometheus.NewDesc("cmdb_cert_expiry_timestamp_seconds", "证书到期时间(unix 秒)", certNames, nil)
	createdDesc := prometheus.NewDesc("cmdb_cert_created_timestamp_seconds", "证书创建时间(unix 秒)", certNames, nil)

	rows, err := c.db.Query(`
		SELECT c.id, c.project, c.env, c.module, c.name, t.cn, t.ca,
		       UNIX_TIMESTAMP(t.expiry_at), UNIX_TIMESTAMP(t.created_at), COALESCE(dc.name,'')
		FROM cis c JOIN certificates t ON t.ci_id=c.id
		LEFT JOIN ci_relations r ON r.src_ci_id=c.id AND r.rel_type='protects'
		LEFT JOIN cis dc ON dc.id=r.dst_ci_id
		WHERE c.type='certificate' AND t.expiry_at IS NOT NULL`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id int64
			var project, env, module, name, cn, ca, domain string
			var expiry, created sql.NullFloat64
			if rows.Scan(&id, &project, &env, &module, &name, &cn, &ca, &expiry, &created, &domain) != nil {
				continue
			}
			base := []string{project, env, module, name, cn, ca, domain}
			vals := append(base, customVals(labelsByCI[id], exportKeys)...)
			if expiry.Valid {
				ch <- prometheus.MustNewConstMetric(expiryDesc, prometheus.GaugeValue, expiry.Float64, vals...)
			}
			if created.Valid {
				ch <- prometheus.MustNewConstMetric(createdDesc, prometheus.GaugeValue, created.Float64, vals...)
			}
		}
	}

	// ---- 域名：到期 ----
	domNames := append([]string{"project", "env", "module", "name", "registrar"}, exportKeys...)
	domDesc := prometheus.NewDesc("cmdb_domain_expiry_timestamp_seconds", "域名到期时间(unix 秒)", domNames, nil)
	drows, err := c.db.Query(`
		SELECT c.id, c.project, c.env, c.module, c.name, COALESCE(reg.name,''), UNIX_TIMESTAMP(d.expiry_at)
		FROM cis c JOIN domains d ON d.ci_id=c.id
		LEFT JOIN registrars reg ON reg.id=d.registrar_id
		WHERE c.type='domain' AND d.expiry_at IS NOT NULL`)
	if err == nil {
		defer drows.Close()
		for drows.Next() {
			var id int64
			var project, env, module, name, registrar string
			var expiry sql.NullFloat64
			if drows.Scan(&id, &project, &env, &module, &name, &registrar, &expiry) != nil {
				continue
			}
			base := []string{project, env, module, name, registrar}
			vals := append(base, customVals(labelsByCI[id], exportKeys)...)
			if expiry.Valid {
				ch <- prometheus.MustNewConstMetric(domDesc, prometheus.GaugeValue, expiry.Float64, vals...)
			}
		}
	}
}
