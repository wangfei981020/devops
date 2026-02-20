package handlers

import (
	"crypto/tls"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"opsplatform/database"
	"opsplatform/models"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// HandleGetDomains 获取域名列表
func HandleGetDomains(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`
		SELECT id, project, module, domain_name, origin, COALESCE(origin_ip,''), cdn_provider, env,
		       expire_time, cert_expire_time, status, remark,
		       created_at, created_by, updated_at, updated_by 
		FROM domains ORDER BY created_at DESC
	`)
	if err != nil {
		log.Printf("查询域名失败: %v", err)
		http.Error(w, "查询失败", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	domains := []models.Domain{}
	for rows.Next() {
		var d models.Domain
		var module, origin, originIP, cdnProvider, env, expireTime, certExpireTime, remark, updatedAt, updatedBy sql.NullString
		err := rows.Scan(&d.ID, &d.Project, &module, &d.DomainName, &origin, &originIP, &cdnProvider, &env,
			&expireTime, &certExpireTime, &d.Status, &remark,
			&d.CreatedAt, &d.CreatedBy, &updatedAt, &updatedBy)
		if err != nil {
			log.Printf("扫描域名数据失败: %v", err)
			continue
		}
		d.Module = module.String
		d.Origin = origin.String
		d.OriginIP = originIP.String
		d.CDNProvider = cdnProvider.String
		d.Env = env.String
		if d.Env == "" {
			d.Env = "PROD"
		}
		d.ExpireTime = expireTime.String
		d.CertExpireTime = certExpireTime.String
		d.Remark = remark.String
		d.UpdatedAt = updatedAt.String
		d.UpdatedBy = updatedBy.String
		domains = append(domains, d)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"domains": domains,
		"total":   len(domains),
	})
}

// HandleAddDomain 添加域名
func HandleAddDomain(w http.ResponseWriter, r *http.Request) {
	var domain models.Domain
	if err := json.NewDecoder(r.Body).Decode(&domain); err != nil {
		http.Error(w, "无效的请求数据", http.StatusBadRequest)
		return
	}

	if domain.DomainName == "" {
		http.Error(w, "域名不能为空", http.StatusBadRequest)
		return
	}
	if domain.Project == "" {
		http.Error(w, "项目不能为空", http.StatusBadRequest)
		return
	}

	domain.ID = uuid.New().String()
	domain.CreatedAt = time.Now().Format("2006-01-02 15:04:05")
	if domain.Status == "" {
		domain.Status = "active"
	}

	// 自动获取证书到期时间和域名到期时间
	if domain.DomainName != "" {
		if domain.CertExpireTime == "" {
			certExpire, err := getCertExpireTime(domain.DomainName)
			if err == nil {
				domain.CertExpireTime = certExpire
			}
		}
		if domain.ExpireTime == "" {
			domainExpire, err := getDomainExpireTime(domain.DomainName)
			if err == nil {
				domain.ExpireTime = domainExpire
			}
		}
	}

	if domain.Env == "" {
		domain.Env = "PROD"
	}
	_, err := database.DB.Exec(`
		INSERT INTO domains (id, project, module, domain_name, origin, origin_ip, cdn_provider, env,
		                     expire_time, cert_expire_time, status, remark, created_at, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, domain.ID, domain.Project, domain.Module, domain.DomainName, domain.Origin, domain.OriginIP, domain.CDNProvider, domain.Env,
		nullIfEmpty(domain.ExpireTime), nullIfEmpty(domain.CertExpireTime), domain.Status, domain.Remark,
		domain.CreatedAt, domain.CreatedBy)

	if err != nil {
		log.Printf("添加域名失败: %v", err)
		http.Error(w, fmt.Sprintf("添加失败: %v", err), http.StatusInternalServerError)
		return
	}

	// 审计日志
	AddAuditLogFromRequest(r, "create", domain.ID, domain.CreatedBy, "", toJSON(domain), "创建域名: "+domain.DomainName)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "域名添加成功",
		"domain":  domain,
	})
}

// HandleUpdateDomain 更新域名
func HandleUpdateDomain(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var domain models.Domain
	if err := json.NewDecoder(r.Body).Decode(&domain); err != nil {
		http.Error(w, "无效的请求数据", http.StatusBadRequest)
		return
	}

	// 获取旧数据
	var oldDomain models.Domain
	var module, origin, cdnProvider, expireTime, certExpireTime, remark, updatedAt, updatedBy sql.NullString
	err := database.DB.QueryRow(`
		SELECT id, project, module, domain_name, origin, cdn_provider, 
		       expire_time, cert_expire_time, status, remark,
		       created_at, created_by, updated_at, updated_by 
		FROM domains WHERE id = ?
	`, id).Scan(&oldDomain.ID, &oldDomain.Project, &module, &oldDomain.DomainName, &origin, &cdnProvider,
		&expireTime, &certExpireTime, &oldDomain.Status, &remark,
		&oldDomain.CreatedAt, &oldDomain.CreatedBy, &updatedAt, &updatedBy)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "域名不存在", http.StatusNotFound)
			return
		}
		http.Error(w, "查询失败", http.StatusInternalServerError)
		return
	}
	oldDomain.Module = module.String
	oldDomain.Origin = origin.String
	oldDomain.CDNProvider = cdnProvider.String
	oldDomain.ExpireTime = expireTime.String
	oldDomain.CertExpireTime = certExpireTime.String
	oldDomain.Remark = remark.String

	domain.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")

	if domain.Env == "" {
		domain.Env = "PROD"
	}
	_, err = database.DB.Exec(`
		UPDATE domains SET project = ?, module = ?, domain_name = ?, origin = ?, origin_ip = ?, cdn_provider = ?, env = ?,
		                   expire_time = ?, cert_expire_time = ?, status = ?, remark = ?,
		                   updated_at = ?, updated_by = ?
		WHERE id = ?
	`, domain.Project, domain.Module, domain.DomainName, domain.Origin, domain.OriginIP, domain.CDNProvider, domain.Env,
		nullIfEmpty(domain.ExpireTime), nullIfEmpty(domain.CertExpireTime), domain.Status, domain.Remark,
		domain.UpdatedAt, domain.UpdatedBy, id)

	if err != nil {
		log.Printf("更新域名失败: %v", err)
		http.Error(w, "更新失败", http.StatusInternalServerError)
		return
	}

	// 审计日志
	AddAuditLogFromRequest(r, "update", id, domain.UpdatedBy, toJSON(oldDomain), toJSON(domain), "更新域名: "+domain.DomainName)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "域名更新成功",
	})
}

// HandleDeleteDomain 删除域名
func HandleDeleteDomain(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	operator := r.Header.Get("X-Operator")

	// 获取旧数据
	var oldDomain models.Domain
	err := database.DB.QueryRow(`SELECT id, domain_name FROM domains WHERE id = ?`, id).
		Scan(&oldDomain.ID, &oldDomain.DomainName)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "域名不存在", http.StatusNotFound)
			return
		}
		http.Error(w, "查询失败", http.StatusInternalServerError)
		return
	}

	_, err = database.DB.Exec("DELETE FROM domains WHERE id = ?", id)
	if err != nil {
		log.Printf("删除域名失败: %v", err)
		http.Error(w, "删除失败", http.StatusInternalServerError)
		return
	}

	// 审计日志
	AddAuditLogFromRequest(r, "delete", id, operator, toJSON(oldDomain), "", "删除域名: "+oldDomain.DomainName)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "域名删除成功",
	})
}

// BatchDomainRequest 批量操作请求
type BatchDomainRequest struct {
	IDs      []string `json:"ids"`
	Action   string   `json:"action"` // delete, enable, disable
	Operator string   `json:"operator"`
}

// HandleBatchDomains 批量操作域名
func HandleBatchDomains(w http.ResponseWriter, r *http.Request) {
	var req BatchDomainRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "无效的请求数据", http.StatusBadRequest)
		return
	}

	if len(req.IDs) == 0 {
		http.Error(w, "请选择至少一个域名", http.StatusBadRequest)
		return
	}

	tx, err := database.DB.Begin()
	if err != nil {
		http.Error(w, "操作失败", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	successCount := 0
	for _, id := range req.IDs {
		var domainName string
		database.DB.QueryRow("SELECT domain_name FROM domains WHERE id = ?", id).Scan(&domainName)

		switch req.Action {
		case "delete":
			_, err = tx.Exec("DELETE FROM domains WHERE id = ?", id)
		case "enable":
			_, err = tx.Exec("UPDATE domains SET status = 'active', updated_at = ?, updated_by = ? WHERE id = ?",
				time.Now().Format("2006-01-02 15:04:05"), req.Operator, id)
		case "disable":
			_, err = tx.Exec("UPDATE domains SET status = 'inactive', updated_at = ?, updated_by = ? WHERE id = ?",
				time.Now().Format("2006-01-02 15:04:05"), req.Operator, id)
		default:
			http.Error(w, "无效的操作类型", http.StatusBadRequest)
			return
		}

		if err == nil {
			successCount++
			AddAuditLogFromRequest(r, req.Action, id, req.Operator, "", "", fmt.Sprintf("批量%s域名: %s", req.Action, domainName))
		}
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, "操作失败", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("成功操作 %d 个域名", successCount),
		"count":   successCount,
	})
}

// HandleBatchAddDomains 批量添加域名
func HandleBatchAddDomains(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Domains     []models.Domain `json:"domains"`
		CreatedBy   string          `json:"created_by"`
		FetchExpiry bool            `json:"fetch_expiry"` // 是否自动获取到期时间
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "无效的请求数据", http.StatusBadRequest)
		return
	}

	if len(req.Domains) == 0 {
		http.Error(w, "请提供至少一个域名", http.StatusBadRequest)
		return
	}

	successCount := 0
	failedDomains := []string{}

	for _, domain := range req.Domains {
		if domain.DomainName == "" || domain.Project == "" {
			failedDomains = append(failedDomains, domain.DomainName+" (缺少必填字段)")
			continue
		}

		domain.ID = uuid.New().String()
		domain.CreatedAt = time.Now().Format("2006-01-02 15:04:05")
		domain.CreatedBy = req.CreatedBy
		if domain.Status == "" {
			domain.Status = "active"
		}

		// 只有在 FetchExpiry 为 true 时才自动获取到期时间
		if req.FetchExpiry && domain.DomainName != "" {
			if domain.CertExpireTime == "" {
				certExpire, err := getCertExpireTime(domain.DomainName)
				if err == nil {
					domain.CertExpireTime = certExpire
				}
			}
			if domain.ExpireTime == "" {
				domainExpire, err := getDomainExpireTime(domain.DomainName)
				if err == nil {
					domain.ExpireTime = domainExpire
				}
			}
		}

		if domain.Env == "" {
			domain.Env = "PROD"
		}
		_, err := database.DB.Exec(`
			INSERT INTO domains (id, project, module, domain_name, origin, origin_ip, cdn_provider, env,
			                     expire_time, cert_expire_time, status, remark, created_at, created_by)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, domain.ID, domain.Project, domain.Module, domain.DomainName, domain.Origin, domain.OriginIP, domain.CDNProvider, domain.Env,
			nullIfEmpty(domain.ExpireTime), nullIfEmpty(domain.CertExpireTime), domain.Status, domain.Remark,
			domain.CreatedAt, domain.CreatedBy)

		if err != nil {
			failedDomains = append(failedDomains, domain.DomainName+" ("+err.Error()+")")
			continue
		}

		successCount++
	}

	// 审计日志
	AddAuditLogFromRequest(r, "batch_create", "domains", req.CreatedBy, "", "",
		fmt.Sprintf("批量添加域名: 成功 %d 个, 失败 %d 个", successCount, len(failedDomains)))

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":        true,
		"message":        fmt.Sprintf("成功添加 %d 个域名", successCount),
		"success_count":  successCount,
		"failed_count":   len(failedDomains),
		"failed_domains": failedDomains,
	})
}

// HandleCheckCert 检查域名证书到期时间
func HandleCheckCert(w http.ResponseWriter, r *http.Request) {
	domain := r.URL.Query().Get("domain")
	if domain == "" {
		http.Error(w, "请提供域名", http.StatusBadRequest)
		return
	}

	// 清理域名格式
	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.TrimPrefix(domain, "http://")
	domain = strings.Split(domain, "/")[0]

	certExpireTime, certErr := getCertExpireTime(domain)
	domainExpireTime, domainErr := getDomainExpireTime(domain)

	result := map[string]interface{}{
		"success": true,
	}

	if certErr == nil {
		result["cert_expire_time"] = certExpireTime
	} else {
		result["cert_error"] = certErr.Error()
	}

	if domainErr == nil {
		result["expire_time"] = domainExpireTime
	} else {
		result["domain_error"] = domainErr.Error()
	}

	if certErr != nil && domainErr != nil {
		result["success"] = false
		result["message"] = "获取域名信息失败"
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(result)
}

// getCertExpireTime 获取域名证书到期时间
func getCertExpireTime(domain string) (string, error) {
	// 清理域名格式
	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.TrimPrefix(domain, "http://")
	domain = strings.Split(domain, "/")[0]

	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
	}

	conn, err := tls.DialWithDialer(
		dialer,
		"tcp",
		domain+":443",
		&tls.Config{InsecureSkipVerify: true},
	)
	if err != nil {
		return "", fmt.Errorf("连接失败: %v", err)
	}
	defer conn.Close()

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return "", fmt.Errorf("未获取到证书")
	}

	expireTime := certs[0].NotAfter.Format("2006-01-02")
	return expireTime, nil
}

// getDomainExpireTime 通过WHOIS查询获取域名到期时间
func getDomainExpireTime(domain string) (string, error) {
	// 清理域名格式
	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.TrimPrefix(domain, "http://")
	domain = strings.Split(domain, "/")[0]
	domain = strings.ToLower(domain)

	// 获取根域名
	parts := strings.Split(domain, ".")
	if len(parts) < 2 {
		return "", fmt.Errorf("无效的域名格式")
	}
	// 取最后两部分作为根域名（简化处理）
	rootDomain := strings.Join(parts[len(parts)-2:], ".")
	tld := parts[len(parts)-1]

	// 根据TLD选择WHOIS服务器
	whoisServer := getWhoisServer(tld)
	if whoisServer == "" {
		return "", fmt.Errorf("不支持的域名后缀: %s", tld)
	}

	// 连接WHOIS服务器
	conn, err := net.DialTimeout("tcp", whoisServer+":43", 10*time.Second)
	if err != nil {
		return "", fmt.Errorf("连接WHOIS服务器失败: %v", err)
	}
	defer conn.Close()

	// 设置读写超时
	conn.SetDeadline(time.Now().Add(15 * time.Second))

	// 发送查询
	_, err = conn.Write([]byte(rootDomain + "\r\n"))
	if err != nil {
		return "", fmt.Errorf("发送查询失败: %v", err)
	}

	// 读取响应
	buf := make([]byte, 8192)
	var response strings.Builder
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			response.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}

	// 解析过期时间
	expireTime := parseWhoisExpireTime(response.String())
	if expireTime == "" {
		return "", fmt.Errorf("未能解析到期时间")
	}

	return expireTime, nil
}

// getWhoisServer 根据TLD获取WHOIS服务器
func getWhoisServer(tld string) string {
	servers := map[string]string{
		"com":    "whois.verisign-grs.com",
		"net":    "whois.verisign-grs.com",
		"org":    "whois.pir.org",
		"cn":     "whois.cnnic.cn",
		"io":     "whois.nic.io",
		"co":     "whois.nic.co",
		"me":     "whois.nic.me",
		"info":   "whois.afilias.net",
		"biz":    "whois.biz",
		"cc":     "ccwhois.verisign-grs.com",
		"tv":     "tvwhois.verisign-grs.com",
		"xyz":    "whois.nic.xyz",
		"top":    "whois.nic.top",
		"club":   "whois.nic.club",
		"online": "whois.nic.online",
		"site":   "whois.nic.site",
		"wang":   "whois.gtld.knet.cn",
		"shop":   "whois.nic.shop",
		"vip":    "whois.nic.vip",
		"work":   "whois.nic.work",
		"ltd":    "whois.nic.ltd",
		"app":    "whois.nic.google",
		"dev":    "whois.nic.google",
	}
	return servers[tld]
}

// parseWhoisExpireTime 解析WHOIS响应中的过期时间
func parseWhoisExpireTime(whoisData string) string {
	// 常见的过期时间字段名
	patterns := []string{
		"Registry Expiry Date:",
		"Expiry Date:",
		"Expiration Date:",
		"paid-till:",
		"Registrar Registration Expiration Date:",
		"Domain Expiration Date:",
		"expire:",
		"Expiration Time:",
	}

	lines := strings.Split(whoisData, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		for _, pattern := range patterns {
			if strings.HasPrefix(strings.ToLower(line), strings.ToLower(pattern)) {
				// 提取日期部分
				dateStr := strings.TrimSpace(strings.TrimPrefix(line, pattern))
				dateStr = strings.TrimPrefix(dateStr, " ")

				// 尝试解析各种日期格式
				formats := []string{
					"2006-01-02T15:04:05Z",
					"2006-01-02T15:04:05.000Z",
					"2006-01-02 15:04:05",
					"2006-01-02",
					"02-Jan-2006",
					"2006.01.02",
					"2006/01/02",
					time.RFC3339,
				}

				for _, format := range formats {
					if t, err := time.Parse(format, strings.Split(dateStr, " ")[0]); err == nil {
						return t.Format("2006-01-02")
					}
					if t, err := time.Parse(format, dateStr); err == nil {
						return t.Format("2006-01-02")
					}
				}
			}
		}
	}
	return ""
}

// nullIfEmpty 如果字符串为空返回 nil
func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// toJSON 将对象转换为 JSON 字符串
func toJSON(v interface{}) string {
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(data)
}

// HandleExportDomains 导出域名列表为CSV
func HandleExportDomains(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`
		SELECT id, project, module, domain_name, origin, cdn_provider, 
		       expire_time, cert_expire_time, status, remark,
		       created_at, created_by, updated_at, updated_by 
		FROM domains ORDER BY created_at DESC
	`)
	if err != nil {
		log.Printf("查询域名失败: %v", err)
		http.Error(w, "查询失败", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	domains := []models.Domain{}
	for rows.Next() {
		var d models.Domain
		var module, origin, cdnProvider, expireTime, certExpireTime, remark, updatedAt, updatedBy sql.NullString
		err := rows.Scan(&d.ID, &d.Project, &module, &d.DomainName, &origin, &cdnProvider,
			&expireTime, &certExpireTime, &d.Status, &remark,
			&d.CreatedAt, &d.CreatedBy, &updatedAt, &updatedBy)
		if err != nil {
			log.Printf("扫描域名数据失败: %v", err)
			continue
		}
		d.Module = module.String
		d.Origin = origin.String
		d.CDNProvider = cdnProvider.String
		d.ExpireTime = expireTime.String
		d.CertExpireTime = certExpireTime.String
		d.Remark = remark.String
		d.UpdatedAt = updatedAt.String
		d.UpdatedBy = updatedBy.String
		domains = append(domains, d)
	}

	// 应用过滤条件
	project := r.URL.Query().Get("project")
	status := r.URL.Query().Get("status")
	cdnProvider := r.URL.Query().Get("cdn_provider")
	expireFilter := r.URL.Query().Get("expire_filter")
	search := r.URL.Query().Get("search")

	var filtered []models.Domain
	for _, d := range domains {
		if project != "" && d.Project != project {
			continue
		}
		if status != "" && d.Status != status {
			continue
		}
		if cdnProvider != "" && d.CDNProvider != cdnProvider {
			continue
		}
		// 到期时间过滤
		if expireFilter != "" && d.ExpireTime != "" {
			expireDate, err := time.Parse("2006-01-02", d.ExpireTime)
			if err == nil {
				diffDays := int(time.Until(expireDate).Hours() / 24)
				if expireFilter == "90+" {
					if diffDays <= 90 {
						continue
					}
				} else {
					days, _ := strconv.Atoi(expireFilter)
					if diffDays < 0 || diffDays > days {
						continue
					}
				}
			}
		}
		if search != "" {
			searchLower := strings.ToLower(search)
			found := false
			for _, field := range []string{d.DomainName, d.Project, d.Module, d.Origin} {
				if strings.Contains(strings.ToLower(field), searchLower) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		filtered = append(filtered, d)
	}

	timestamp := time.Now().Format("20060102_150405")
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=domains_%s.csv", timestamp))
	// 添加 BOM 以支持 Excel 打开中文
	w.Write([]byte{0xEF, 0xBB, 0xBF})

	writer := csv.NewWriter(w)
	writer.Write([]string{"ID", "项目", "模块", "域名", "回源", "源站IP", "CDN厂商", "域名到期时间", "证书到期时间", "状态", "备注", "创建时间", "创建人", "更新时间", "更新人"})
	for _, d := range filtered {
		statusText := "启用"
		if d.Status != "active" {
			statusText = "停用"
		}
		writer.Write([]string{
			d.ID, d.Project, d.Module, d.DomainName, d.Origin, d.OriginIP, d.CDNProvider,
			d.ExpireTime, d.CertExpireTime, statusText, d.Remark,
			d.CreatedAt, d.CreatedBy, d.UpdatedAt, d.UpdatedBy,
		})
	}
	writer.Flush()
}

// HandleBatchRefreshDomains 批量刷新所有域名的到期时间
func HandleBatchRefreshDomains(w http.ResponseWriter, r *http.Request) {
	// 获取所有启用的域名
	rows, err := database.DB.Query(`
		SELECT id, domain_name FROM domains WHERE status = 'active'
	`)
	if err != nil {
		http.Error(w, "查询域名失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var domains []struct {
		ID     string
		Domain string
	}
	for rows.Next() {
		var d struct {
			ID     string
			Domain string
		}
		if err := rows.Scan(&d.ID, &d.Domain); err != nil {
			continue
		}
		domains = append(domains, d)
	}

	total := len(domains)
	successCount := 0
	failCount := 0
	var failDetails []string

	// 分批处理，每批5个，控制并发
	batchSize := 5
	for i := 0; i < len(domains); i += batchSize {
		end := i + batchSize
		if end > len(domains) {
			end = len(domains)
		}
		batch := domains[i:end]

		// 并发处理当前批次
		type result struct {
			id      string
			domain  string
			success bool
			err     string
		}
		results := make(chan result, len(batch))

		for _, d := range batch {
			go func(id, domain string) {
				res := result{id: id, domain: domain, success: true}

				// 获取证书到期时间
				certExpire, certErr := getCertExpireTime(domain)
				if certErr != nil {
					res.success = false
					res.err = "证书: " + certErr.Error()
				}

				// 获取域名到期时间
				domainExpire, domainErr := getDomainExpireTime(domain)
				if domainErr != nil && certErr != nil {
					// 两个都失败才算失败
					results <- res
					return
				}

				// 更新数据库
				updateSQL := "UPDATE domains SET updated_at = NOW()"
				var args []interface{}
				if certErr == nil && certExpire != "" {
					updateSQL += ", cert_expire_time = ?"
					args = append(args, certExpire)
				}
				if domainErr == nil && domainExpire != "" {
					updateSQL += ", expire_time = ?"
					args = append(args, domainExpire)
				}
				updateSQL += " WHERE id = ?"
				args = append(args, id)

				if len(args) > 1 { // 至少有一个时间需要更新
					_, err := database.DB.Exec(updateSQL, args...)
					if err != nil {
						res.success = false
						res.err = "更新失败: " + err.Error()
					} else {
						res.success = true
					}
				}

				results <- res
			}(d.ID, d.Domain)
		}

		// 收集结果
		for range batch {
			res := <-results
			if res.success {
				successCount++
			} else {
				failCount++
				if len(failDetails) < 20 { // 最多记录20条失败详情
					failDetails = append(failDetails, res.domain+": "+res.err)
				}
			}
		}

		// 批次间短暂休息，避免请求过于密集
		if end < len(domains) {
			time.Sleep(500 * time.Millisecond)
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":      true,
		"message":      fmt.Sprintf("批量刷新完成：成功 %d 个，失败 %d 个，共 %d 个", successCount, failCount, total),
		"total":        total,
		"success_count": successCount,
		"fail_count":   failCount,
		"fail_details": failDetails,
	})
}

// RefreshAllDomainsExpire 定时任务：刷新所有域名到期时间
// 适合大量域名的场景，分批处理避免资源耗尽
func RefreshAllDomainsExpire() {
	log.Println("[定时任务] 开始刷新所有域名到期时间...")

	// 获取所有启用的域名
	rows, err := database.DB.Query(`
		SELECT id, domain_name FROM domains WHERE status = 'active'
	`)
	if err != nil {
		log.Printf("[定时任务] 查询域名失败: %v", err)
		return
	}
	defer rows.Close()

	var domains []struct {
		ID     string
		Domain string
	}
	for rows.Next() {
		var d struct {
			ID     string
			Domain string
		}
		if err := rows.Scan(&d.ID, &d.Domain); err != nil {
			continue
		}
		domains = append(domains, d)
	}

	total := len(domains)
	successCount := 0
	failCount := 0

	log.Printf("[定时任务] 共 %d 个域名需要刷新", total)

	// 分批处理，每批5个，每批间隔1秒
	batchSize := 5
	for i := 0; i < len(domains); i += batchSize {
		end := i + batchSize
		if end > len(domains) {
			end = len(domains)
		}
		batch := domains[i:end]

		// 串行处理当前批次，避免过多并发
		for _, d := range batch {
			// 获取证书到期时间
			certExpire, _ := getCertExpireTime(d.Domain)

			// 获取域名到期时间
			domainExpire, _ := getDomainExpireTime(d.Domain)

			// 更新数据库
			updateSQL := "UPDATE domains SET updated_at = NOW()"
			var args []interface{}
			if certExpire != "" {
				updateSQL += ", cert_expire_time = ?"
				args = append(args, certExpire)
			}
			if domainExpire != "" {
				updateSQL += ", expire_time = ?"
				args = append(args, domainExpire)
			}
			updateSQL += " WHERE id = ?"
			args = append(args, d.ID)

			if len(args) > 1 {
				_, err := database.DB.Exec(updateSQL, args...)
				if err != nil {
					failCount++
				} else {
					successCount++
				}
			} else {
				failCount++
			}
		}

		// 批次间休息1秒，避免请求过于密集
		if end < len(domains) {
			time.Sleep(1 * time.Second)
		}

		// 每处理100个打印进度
		if (i+batchSize)%100 == 0 || end == len(domains) {
			log.Printf("[定时任务] 进度: %d/%d (成功: %d, 失败: %d)", end, total, successCount, failCount)
		}
	}

	log.Printf("[定时任务] 域名到期时间刷新完成：成功 %d 个，失败 %d 个，共 %d 个", successCount, failCount, total)
}
