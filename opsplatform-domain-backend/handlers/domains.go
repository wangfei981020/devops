package handlers

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"domain-platform/database"
	"domain-platform/models"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/likexian/whois"
	whoisparser "github.com/likexian/whois-parser"
)

func HandleGetDomains(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	q := r.URL.Query()
	configID := q.Get("config_id")
	provider := q.Get("provider")
	project := q.Get("project")
	env := q.Get("env")
	module := q.Get("module")
	cdnProvider := q.Get("cdn_provider")
	status := q.Get("status")
	search := q.Get("search")

	query := `SELECT id,
		COALESCE(config_id,''), COALESCE(provider,''), COALESCE(source,''),
		COALESCE(project,''), COALESCE(env,''),
		COALESCE(root_domain,''), COALESCE(host,''), COALESCE(full_domain,''),
		COALESCE(record_type,''), COALESCE(record_value,''),
		COALESCE(DATE_FORMAT(expire_time,'%Y-%m-%d'),''),
		COALESCE(DATE_FORMAT(cert_expire_time,'%Y-%m-%d'),''),
		COALESCE(module,''), COALESCE(origin,''), COALESCE(origin_ip,''),
		COALESCE(cdn_provider,''), COALESCE(status,'active'), COALESCE(remark,''),
		COALESCE(DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s'),''),
		COALESCE(DATE_FORMAT(updated_at,'%Y-%m-%d %H:%i:%s'),'')
		FROM registrar_domains WHERE 1=1`

	var args []interface{}

	if configID != "" {
		query += " AND config_id=?"
		args = append(args, configID)
	}
	if provider != "" {
		query += " AND provider=?"
		args = append(args, provider)
	}
	if project != "" {
		query += " AND project=?"
		args = append(args, project)
	}
	if env != "" {
		query += " AND env=?"
		args = append(args, env)
	}
	if module != "" {
		query += " AND module=?"
		args = append(args, module)
	}
	if cdnProvider != "" {
		query += " AND cdn_provider=?"
		args = append(args, cdnProvider)
	}
	if status != "" {
		query += " AND status=?"
		args = append(args, status)
	}
	if search != "" {
		query += " AND (full_domain LIKE ? OR module LIKE ? OR remark LIKE ? OR project LIKE ?)"
		like := "%" + search + "%"
		args = append(args, like, like, like, like)
	}

	query += " ORDER BY created_at DESC"

	rows, err := database.DB.Query(query, args...)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": err.Error()})
		return
	}
	defer rows.Close()

	var domains []models.RegistrarDomain
	for rows.Next() {
		var d models.RegistrarDomain
		if err := rows.Scan(
			&d.ID, &d.ConfigID, &d.Provider, &d.Source,
			&d.Project, &d.Env,
			&d.RootDomain, &d.Host, &d.FullDomain,
			&d.RecordType, &d.RecordValue,
			&d.ExpireTime, &d.CertExpireTime,
			&d.Module, &d.Origin, &d.OriginIP,
			&d.CDNProvider, &d.Status, &d.Remark,
			&d.CreatedAt, &d.UpdatedAt,
		); err != nil {
			continue
		}
		domains = append(domains, d)
	}
	if domains == nil {
		domains = []models.RegistrarDomain{}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "data": domains, "total": len(domains)})
}

// HandleAddDomain 手动添加域名记录
func HandleAddDomain(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var d models.RegistrarDomain
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "请求格式错误"})
		return
	}
	if d.FullDomain == "" {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "域名不能为空"})
		return
	}

	d.ID = uuid.New().String()
	d.Source = "manual"
	if d.Status == "" {
		d.Status = "active"
	}
	if d.RecordType == "" {
		d.RecordType = "A"
	}
	now := time.Now().Format("2006-01-02 15:04:05")

	_, err := database.DB.Exec(`
		INSERT INTO registrar_domains
		(id, config_id, provider, source, project, env, root_domain, host, full_domain,
		 record_type, record_value, expire_time, cert_expire_time,
		 module, origin, origin_ip, cdn_provider, status, remark, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		d.ID, "", "", d.Source, d.Project, d.Env,
		d.RootDomain, d.Host, d.FullDomain,
		d.RecordType, d.RecordValue,
		nullableDate(d.ExpireTime), nullableDate(d.CertExpireTime),
		d.Module, d.Origin, d.OriginIP, d.CDNProvider, d.Status, d.Remark,
		now, now,
	)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "添加成功", "id": d.ID})
}

func HandleUpdateDomain(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	vars := mux.Vars(r)
	id := vars["id"]

	var d models.RegistrarDomain
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "请求格式错误"})
		return
	}

	now := time.Now().Format("2006-01-02 15:04:05")

	_, err := database.DB.Exec(
		`UPDATE registrar_domains SET
		 full_domain=?, root_domain=?, host=?, record_type=?,
		 project=?, env=?, module=?, origin=?, origin_ip=?, cdn_provider=?,
		 cert_expire_time=?, expire_time=?, status=?, remark=?, updated_at=?
		 WHERE id=?`,
		d.FullDomain, d.RootDomain, d.Host, d.RecordType,
		d.Project, d.Env, d.Module, d.Origin, d.OriginIP, d.CDNProvider,
		nullableDate(d.CertExpireTime), nullableDate(d.ExpireTime),
		d.Status, d.Remark, now, id,
	)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "更新成功"})
}

func HandleDeleteDomain(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	vars := mux.Vars(r)
	id := vars["id"]

	_, err := database.DB.Exec("DELETE FROM registrar_domains WHERE id=?", id)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "删除成功"})
}

// HandleGetDomainOptions 获取过滤用的可选值（项目、环境、模块、CDN厂商列表）
func HandleGetDomainOptions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	queryDistinct := func(col string) []string {
		rows, err := database.DB.Query(fmt.Sprintf(
			"SELECT DISTINCT %s FROM registrar_domains WHERE %s IS NOT NULL AND %s!='' ORDER BY %s", col, col, col, col))
		if err != nil {
			return []string{}
		}
		defer rows.Close()
		var vals []string
		for rows.Next() {
			var v string
			rows.Scan(&v)
			vals = append(vals, v)
		}
		if vals == nil {
			return []string{}
		}
		return vals
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":      true,
		"projects":     queryDistinct("project"),
		"envs":         queryDistinct("env"),
		"modules":      queryDistinct("module"),
		"cdn_providers": queryDistinct("cdn_provider"),
	})
}

func HandleCheckCert(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	domain := r.URL.Query().Get("domain")
	if domain == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "domain参数缺失"})
		return
	}

	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.TrimPrefix(domain, "http://")
	if idx := strings.Index(domain, "/"); idx != -1 {
		domain = domain[:idx]
	}

	dialer := &tls.Dialer{
		Config: &tls.Config{InsecureSkipVerify: true},
	}

	timeoutCtx, cancel := timeoutContext(10 * time.Second)
	defer cancel()

	conn, err := dialer.DialContext(timeoutCtx, "tcp", fmt.Sprintf("%s:443", domain))
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": fmt.Sprintf("连接失败: %v", err)})
		return
	}
	defer conn.Close()

	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "TLS连接类型错误"})
		return
	}

	certs := tlsConn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "未找到证书"})
		return
	}

	expiry := certs[0].NotAfter.Format("2006-01-02")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":          true,
		"cert_expire_time": expiry,
		"message":          fmt.Sprintf("证书到期时间: %s", expiry),
	})
}

func HandleCheckExpiry(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	domain := r.URL.Query().Get("domain")
	if domain == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "domain参数缺失"})
		return
	}

	// Strip to root domain for WHOIS (remove subdomain)
	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.TrimPrefix(domain, "http://")
	if idx := strings.Index(domain, "/"); idx != -1 {
		domain = domain[:idx]
	}
	// Use root domain if subdomain provided
	parts := strings.Split(domain, ".")
	if len(parts) > 2 {
		domain = strings.Join(parts[len(parts)-2:], ".")
	}

	result, err := whois.Whois(domain)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": fmt.Sprintf("WHOIS查询失败: %v", err)})
		return
	}

	parsed, err := whoisparser.Parse(result)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": fmt.Sprintf("WHOIS解析失败: %v", err)})
		return
	}

	expiry := parsed.Domain.ExpirationDate
	if expiry == "" {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "未能获取到期时间"})
		return
	}

	// Parse and reformat date
	var expireFormatted string
	formats := []string{
		time.RFC3339, "2006-01-02T15:04:05Z", "2006-01-02T15:04:05",
		"2006-01-02", "2006-01-02 15:04:05",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, expiry); err == nil {
			expireFormatted = t.Format("2006-01-02")
			break
		}
	}
	if expireFormatted == "" {
		expireFormatted = expiry
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"expire_time": expireFormatted,
		"message":     fmt.Sprintf("域名到期时间: %s", expireFormatted),
	})
}

func nullableDate(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
