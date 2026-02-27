package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"opsplatform/database"
	"opsplatform/models"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

var merchantCols = `id, project, env, website_name, COALESCE(contact_emails,'[]'), COALESCE(website_urls,'[]'), 
	COALESCE(player_regions,'[]'), COALESCE(estimated_players,''), COALESCE(game_types,'[]'), COALESCE(handicaps,'[]'),
	COALESCE(languages,'[]'), COALESCE(currencies,'[]'), COALESCE(supported_ports,'[]'), COALESCE(wallet_types,'[]'),
	COALESCE(callback_domains,'[]'), COALESCE(whitelist_ips,''), COALESCE(hall_domains,'[]'), COALESCE(site_domains,'[]'),
	COALESCE(site_accounts,'[]'), COALESCE(app_keys,'[]'), COALESCE(app_secrets,''), COALESCE(game_domains,'[]'),
	COALESCE(redirect_domains,'[]'), COALESCE(custom_fields,'{}'), COALESCE(remark,''), COALESCE(status,'active'),
	created_at, COALESCE(created_by,''), COALESCE(updated_at,''), COALESCE(updated_by,'')`

func scanMerchant(scanner interface{ Scan(...interface{}) error }) (*models.Merchant, error) {
	var m models.Merchant
	err := scanner.Scan(&m.ID, &m.Project, &m.Env, &m.WebsiteName, &m.ContactEmails, &m.WebsiteUrls,
		&m.PlayerRegions, &m.EstimatedPlayers, &m.GameTypes, &m.Handicaps,
		&m.Languages, &m.Currencies, &m.SupportedPorts, &m.WalletTypes,
		&m.CallbackDomains, &m.WhitelistIPs, &m.HallDomains, &m.SiteDomains,
		&m.SiteAccounts, &m.AppKeys, &m.AppSecrets, &m.GameDomains,
		&m.RedirectDomains, &m.CustomFields, &m.Remark, &m.Status,
		&m.CreatedAt, &m.CreatedBy, &m.UpdatedAt, &m.UpdatedBy)
	return &m, err
}

// HandleGetMerchants 获取商户列表
func HandleGetMerchants(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	env := r.URL.Query().Get("env")

	query := "SELECT " + merchantCols + " FROM merchants WHERE 1=1"
	args := []interface{}{}

	if project != "" {
		query += " AND project = ?"
		args = append(args, project)
	}
	if env != "" {
		query += " AND env = ?"
		args = append(args, env)
	}
	query += " ORDER BY created_at DESC"

	rows, err := database.DB.Query(query, args...)
	if err != nil {
		log.Printf("[商户] 查询失败: %v", err)
		sendError(w, "查询失败", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	merchants := make([]*models.Merchant, 0)
	for rows.Next() {
		m, err := scanMerchant(rows)
		if err != nil {
			log.Printf("扫描商户失败: %v", err)
			continue
		}
		m.AppSecrets = "" // 不返回密钥
		merchants = append(merchants, m)
	}

	respondJSON(w, http.StatusOK, merchants)
}

// checkMerchantExists 检查同一网站方+环境是否已存在
func checkMerchantExists(websiteName, env, excludeID string) (bool, error) {
	var count int
	query := "SELECT COUNT(*) FROM merchants WHERE website_name = ? AND env = ?"
	args := []interface{}{websiteName, env}
	if excludeID != "" {
		query += " AND id != ?"
		args = append(args, excludeID)
	}
	log.Printf("[商户检查] SQL: %s, Args: %v", query, args)
	err := database.DB.QueryRow(query, args...).Scan(&count)
	log.Printf("[商户检查] Count: %d, Error: %v", count, err)
	return count > 0, err
}

func normalizeMerchantIdentity(websiteName, env string) (string, string) {
	normalizedWebsite := strings.TrimSpace(websiteName)
	normalizedEnv := strings.ToUpper(strings.TrimSpace(env))
	return normalizedWebsite, normalizedEnv
}

func loadMerchantByID(id string) (*models.Merchant, error) {
	row := database.DB.QueryRow("SELECT "+merchantCols+" FROM merchants WHERE id = ?", id)
	return scanMerchant(row)
}

func hasJSONField(raw map[string]json.RawMessage, key string) bool {
	_, ok := raw[key]
	return ok
}

// HandleCreateMerchant 创建商户
func HandleCreateMerchant(w http.ResponseWriter, r *http.Request) {
	var m models.Merchant
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		sendError(w, "请求参数无效", http.StatusBadRequest)
		return
	}

	m.WebsiteName, m.Env = normalizeMerchantIdentity(m.WebsiteName, m.Env)
	if m.WebsiteName == "" {
		sendError(w, "网站方名称不能为空", http.StatusBadRequest)
		return
	}

	if m.Env == "" {
		m.Env = "PROD"
	}

	// 检查唯一性：同一网站方+环境只能存在一个
	exists, err := checkMerchantExists(m.WebsiteName, m.Env, "")
	if err != nil {
		log.Printf("[商户] 检查失败: %v", err)
		sendError(w, "检查失败", http.StatusInternalServerError)
		return
	}
	if exists {
		sendError(w, fmt.Sprintf("商户 [%s] 在环境 [%s] 中已存在", m.WebsiteName, m.Env), http.StatusConflict)
		return
	}

	m.ID = uuid.New().String()
	now := time.Now().Format("2006-01-02 15:04:05")
	user := r.Header.Get("X-Operator")
	if m.Status == "" {
		m.Status = "active"
	}

	// 加密 appsecret
	encSecret := ""
	if m.AppSecrets != "" {
		encSecret, err = encryptPassword(m.AppSecrets)
		if err != nil {
			encSecret = m.AppSecrets
		}
	}

	// 处理 custom_fields：空字符串转为 null
	var customFields interface{}
	if m.CustomFields == "" {
		customFields = nil
	} else {
		customFields = m.CustomFields
	}

	_, err = database.DB.Exec(`INSERT INTO merchants (id, project, env, website_name, contact_emails, website_urls, player_regions, estimated_players, game_types, handicaps, languages, currencies, supported_ports, wallet_types, callback_domains, whitelist_ips, hall_domains, site_domains, site_accounts, app_keys, app_secrets, game_domains, redirect_domains, custom_fields, remark, status, created_at, created_by, updated_at, updated_by) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		m.ID, m.Project, m.Env, m.WebsiteName, m.ContactEmails, m.WebsiteUrls,
		m.PlayerRegions, m.EstimatedPlayers, m.GameTypes, m.Handicaps,
		m.Languages, m.Currencies, m.SupportedPorts, m.WalletTypes,
		m.CallbackDomains, m.WhitelistIPs, m.HallDomains, m.SiteDomains,
		m.SiteAccounts, m.AppKeys, encSecret, m.GameDomains,
		m.RedirectDomains, customFields, m.Remark, m.Status,
		now, user, now, user)
	if err != nil {
		log.Printf("[商户] 创建失败: %v", err)
		sendError(w, "创建失败", http.StatusInternalServerError)
		return
	}

	m.CreatedAt = now
	m.CreatedBy = user
	m.AppSecrets = ""

	AddAuditLogFromRequest(r, "CREATE_MERCHANT", m.ID, user, "", "", fmt.Sprintf("创建商户: %s", m.WebsiteName))

	respondJSON(w, http.StatusCreated, m)
}

// HandleUpdateMerchant 更新商户
func HandleUpdateMerchant(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	body, err := io.ReadAll(r.Body)
	if err != nil {
		sendError(w, "读取请求失败", http.StatusBadRequest)
		return
	}

	var m models.Merchant
	if err := json.Unmarshal(body, &m); err != nil {
		sendError(w, "请求参数无效", http.StatusBadRequest)
		return
	}

	rawPayload := map[string]json.RawMessage{}
	if err := json.Unmarshal(body, &rawPayload); err != nil {
		sendError(w, "请求参数无效", http.StatusBadRequest)
		return
	}

	current, err := loadMerchantByID(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			sendError(w, "商户不存在", http.StatusNotFound)
			return
		}
		log.Printf("[商户] 读取当前记录失败: %v", err)
		sendError(w, "查询当前商户失败", http.StatusInternalServerError)
		return
	}

	// 按字段是否出现在请求中进行合并，支持安全的部分更新
	if !hasJSONField(rawPayload, "project") {
		m.Project = current.Project
	}
	if !hasJSONField(rawPayload, "env") {
		m.Env = current.Env
	}
	if !hasJSONField(rawPayload, "website_name") {
		m.WebsiteName = current.WebsiteName
	}
	if !hasJSONField(rawPayload, "contact_emails") {
		m.ContactEmails = current.ContactEmails
	}
	if !hasJSONField(rawPayload, "website_urls") {
		m.WebsiteUrls = current.WebsiteUrls
	}
	if !hasJSONField(rawPayload, "player_regions") {
		m.PlayerRegions = current.PlayerRegions
	}
	if !hasJSONField(rawPayload, "estimated_players") {
		m.EstimatedPlayers = current.EstimatedPlayers
	}
	if !hasJSONField(rawPayload, "game_types") {
		m.GameTypes = current.GameTypes
	}
	if !hasJSONField(rawPayload, "handicaps") {
		m.Handicaps = current.Handicaps
	}
	if !hasJSONField(rawPayload, "languages") {
		m.Languages = current.Languages
	}
	if !hasJSONField(rawPayload, "currencies") {
		m.Currencies = current.Currencies
	}
	if !hasJSONField(rawPayload, "supported_ports") {
		m.SupportedPorts = current.SupportedPorts
	}
	if !hasJSONField(rawPayload, "wallet_types") {
		m.WalletTypes = current.WalletTypes
	}
	if !hasJSONField(rawPayload, "callback_domains") {
		m.CallbackDomains = current.CallbackDomains
	}
	if !hasJSONField(rawPayload, "whitelist_ips") {
		m.WhitelistIPs = current.WhitelistIPs
	}
	if !hasJSONField(rawPayload, "hall_domains") {
		m.HallDomains = current.HallDomains
	}
	if !hasJSONField(rawPayload, "site_domains") {
		m.SiteDomains = current.SiteDomains
	}
	if !hasJSONField(rawPayload, "site_accounts") {
		m.SiteAccounts = current.SiteAccounts
	}
	if !hasJSONField(rawPayload, "app_keys") {
		m.AppKeys = current.AppKeys
	}
	if !hasJSONField(rawPayload, "game_domains") {
		m.GameDomains = current.GameDomains
	}
	if !hasJSONField(rawPayload, "redirect_domains") {
		m.RedirectDomains = current.RedirectDomains
	}
	if !hasJSONField(rawPayload, "custom_fields") {
		m.CustomFields = current.CustomFields
	}
	if !hasJSONField(rawPayload, "remark") {
		m.Remark = current.Remark
	}
	if !hasJSONField(rawPayload, "status") {
		m.Status = current.Status
	}
	if m.Status == "" {
		m.Status = "active"
	}

	// 检查唯一性：同一网站方+环境只能存在一个（排除当前记录）
	// 仅在网站方或环境发生变化时检查，避免历史重复数据导致无法编辑其它字段
	m.WebsiteName, m.Env = normalizeMerchantIdentity(m.WebsiteName, m.Env)
	oldWebsiteName, oldEnv := normalizeMerchantIdentity(current.WebsiteName, current.Env)
	if m.WebsiteName == "" || m.Env == "" {
		sendError(w, "网站方和环境不能为空", http.StatusBadRequest)
		return
	}
	identityChanged := oldWebsiteName != m.WebsiteName || oldEnv != m.Env
	log.Printf("[商户] 更新检查 - ID: %s, 新网站方: %s, 新环境: %s, 原网站方: %s, 原环境: %s, 关键字段是否变更: %v",
		id, m.WebsiteName, m.Env, oldWebsiteName, oldEnv, identityChanged)
	if identityChanged {
		exists, err := checkMerchantExists(m.WebsiteName, m.Env, id)
		if err != nil {
			log.Printf("[商户] 检查失败: %v", err)
			sendError(w, "检查失败", http.StatusInternalServerError)
			return
		}
		log.Printf("[商户] 检查结果 - exists: %v", exists)
		if exists {
			sendError(w, fmt.Sprintf("商户 [%s] 在环境 [%s] 中已存在", m.WebsiteName, m.Env), http.StatusConflict)
			return
		}
	}

	user := r.Header.Get("X-Operator")
	now := time.Now().Format("2006-01-02 15:04:05")

	if m.AppSecrets != "" && m.AppSecrets != "******" {
		encSecret, err := encryptPassword(m.AppSecrets)
		if err != nil {
			encSecret = m.AppSecrets
		}
		database.DB.Exec("UPDATE merchants SET app_secrets=? WHERE id=?", encSecret, id)
	}

	// 处理 custom_fields：空字符串转为 null
	var customFields interface{}
	if m.CustomFields == "" {
		customFields = nil
	} else {
		customFields = m.CustomFields
	}

	_, err = database.DB.Exec(`UPDATE merchants SET project=?, env=?, website_name=?, contact_emails=?, website_urls=?, player_regions=?, estimated_players=?, game_types=?, handicaps=?, languages=?, currencies=?, supported_ports=?, wallet_types=?, callback_domains=?, whitelist_ips=?, hall_domains=?, site_domains=?, site_accounts=?, app_keys=?, game_domains=?, redirect_domains=?, custom_fields=?, remark=?, status=?, updated_at=?, updated_by=? WHERE id=?`,
		m.Project, m.Env, m.WebsiteName, m.ContactEmails, m.WebsiteUrls,
		m.PlayerRegions, m.EstimatedPlayers, m.GameTypes, m.Handicaps,
		m.Languages, m.Currencies, m.SupportedPorts, m.WalletTypes,
		m.CallbackDomains, m.WhitelistIPs, m.HallDomains, m.SiteDomains,
		m.SiteAccounts, m.AppKeys, m.GameDomains,
		m.RedirectDomains, customFields, m.Remark, m.Status,
		now, user, id)
	if err != nil {
		log.Printf("[商户] 更新失败: %v", err)
		sendError(w, "更新失败", http.StatusInternalServerError)
		return
	}

	AddAuditLogFromRequest(r, "UPDATE_MERCHANT", id, user, "", "", fmt.Sprintf("更新商户: %s", m.WebsiteName))

	respondJSON(w, http.StatusOK, map[string]string{"message": "更新成功"})
}

// HandleBatchCreateMerchants 批量创建商户
func HandleBatchCreateMerchants(w http.ResponseWriter, r *http.Request) {
	var merchants []models.Merchant
	if err := json.NewDecoder(r.Body).Decode(&merchants); err != nil {
		sendError(w, "请求参数无效", http.StatusBadRequest)
		return
	}
	if len(merchants) == 0 {
		sendError(w, "商户列表不能为空", http.StatusBadRequest)
		return
	}

	user := r.Header.Get("X-Operator")
	now := time.Now().Format("2006-01-02 15:04:05")
	successCount := 0
	failCount := 0

	var skipMessages []string
	for _, m := range merchants {
		if m.WebsiteName == "" {
			failCount++
			skipMessages = append(skipMessages, "网站方名称为空")
			continue
		}
		if m.Status == "" { m.Status = "active" }
		if m.Env == "" { m.Env = "prod" }

		// 检查唯一性
		exists, _ := checkMerchantExists(m.WebsiteName, m.Env, "")
		if exists {
			failCount++
			skipMessages = append(skipMessages, fmt.Sprintf("[%s] 在环境 [%s] 已存在", m.WebsiteName, m.Env))
			continue
		}

		m.ID = uuid.New().String()

		encSecret := ""
		if m.AppSecrets != "" {
			encSecret, _ = encryptPassword(m.AppSecrets)
		}

		_, err := database.DB.Exec(`INSERT INTO merchants (id, project, env, website_name, contact_emails, website_urls, player_regions, estimated_players, game_types, handicaps, languages, currencies, supported_ports, wallet_types, callback_domains, whitelist_ips, hall_domains, site_domains, site_accounts, app_keys, app_secrets, game_domains, redirect_domains, custom_fields, remark, status, created_at, created_by, updated_at, updated_by) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			m.ID, m.Project, m.Env, m.WebsiteName, m.ContactEmails, m.WebsiteUrls,
			m.PlayerRegions, m.EstimatedPlayers, m.GameTypes, m.Handicaps,
			m.Languages, m.Currencies, m.SupportedPorts, m.WalletTypes,
			m.CallbackDomains, m.WhitelistIPs, m.HallDomains, m.SiteDomains,
			m.SiteAccounts, m.AppKeys, encSecret, m.GameDomains,
			m.RedirectDomains, m.CustomFields, m.Remark, m.Status,
			now, user, now, user)
		if err != nil {
			failCount++
			skipMessages = append(skipMessages, fmt.Sprintf("[%s] 插入失败: %v", m.WebsiteName, err))
			continue
		}
		successCount++
	}

	result := map[string]interface{}{
		"success_count": successCount,
		"fail_count":    failCount,
		"message":       fmt.Sprintf("成功 %d 个，失败 %d 个", successCount, failCount),
	}
	if len(skipMessages) > 0 {
		result["skip_details"] = skipMessages
	}
	respondJSON(w, http.StatusOK, result)
}

// HandleDeleteMerchant 删除商户
func HandleDeleteMerchant(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	user := r.Header.Get("X-Operator")
	_, err := database.DB.Exec("DELETE FROM merchants WHERE id = ?", id)
	if err != nil {
		sendError(w, "删除失败", http.StatusInternalServerError)
		return
	}

	AddAuditLogFromRequest(r, "DELETE_MERCHANT", id, user, "", "", "删除商户")

	respondJSON(w, http.StatusOK, map[string]string{"message": "删除成功"})
}

// HandleExportMerchants 导出商户
func HandleExportMerchants(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query("SELECT " + merchantCols + " FROM merchants ORDER BY created_at DESC")
	if err != nil {
		sendError(w, "导出失败", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=merchants_%s.csv", time.Now().Format("20060102")))
	w.Write([]byte("\xEF\xBB\xBF"))
	w.Write([]byte("项目,环境,网站方,对接邮箱,网站方网址,玩家地区,预计在线玩家,游戏种类,盘口,语言,币种,支持端口,钱包类型,三方回调域名,三方白名单,厅房域名,站点域名,站点账号,AppKey,游戏域名,301域名,备注,状态\n"))

	for rows.Next() {
		m, err := scanMerchant(rows)
		if err != nil {
			continue
		}
		line := fmt.Sprintf("\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\"\n",
			m.Project, m.Env, m.WebsiteName, m.ContactEmails, m.WebsiteUrls, m.PlayerRegions,
			m.EstimatedPlayers, m.GameTypes, m.Handicaps, m.Languages, m.Currencies,
			m.SupportedPorts, m.WalletTypes, m.CallbackDomains, m.WhitelistIPs,
			m.HallDomains, m.SiteDomains, m.SiteAccounts, m.AppKeys,
			m.GameDomains, m.RedirectDomains, m.Remark, m.Status)
		w.Write([]byte(line))
	}
}

// ========== 商户自定义列管理（全局共享） ==========

// MerchantCustomColumn 自定义列结构
type MerchantCustomColumn struct {
	ID        string `json:"id"`
	ColKey    string `json:"col_key"`
	ColTitle  string `json:"col_title"`
	ColType   string `json:"col_type"`
	ColWidth  string `json:"col_width"`
	SortOrder int    `json:"sort_order"`
	IsActive  bool   `json:"is_active"`
	CreatedAt string `json:"created_at"`
	CreatedBy string `json:"created_by"`
}

// HandleGetMerchantColumns 获取所有自定义列
func HandleGetMerchantColumns(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`
		SELECT id, col_key, col_title, col_type, col_width, sort_order, is_active, 
		       created_at, COALESCE(created_by,'')
		FROM merchant_custom_columns 
		WHERE is_active = TRUE
		ORDER BY sort_order ASC, created_at ASC
	`)
	if err != nil {
		log.Printf("[商户列] 查询失败: %v", err)
		sendError(w, "查询失败", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	columns := make([]MerchantCustomColumn, 0)
	for rows.Next() {
		var c MerchantCustomColumn
		err := rows.Scan(&c.ID, &c.ColKey, &c.ColTitle, &c.ColType, &c.ColWidth,
			&c.SortOrder, &c.IsActive, &c.CreatedAt, &c.CreatedBy)
		if err != nil {
			log.Printf("[商户列] 扫描失败: %v", err)
			continue
		}
		columns = append(columns, c)
	}

	respondJSON(w, http.StatusOK, columns)
}

// HandleCreateMerchantColumn 创建自定义列
func HandleCreateMerchantColumn(w http.ResponseWriter, r *http.Request) {
	var c MerchantCustomColumn
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		sendError(w, "请求参数无效", http.StatusBadRequest)
		return
	}

	if c.ColKey == "" {
		sendError(w, "列标识不能为空", http.StatusBadRequest)
		return
	}

	// 自动添加 custom_ 前缀
	if len(c.ColKey) < 7 || c.ColKey[:7] != "custom_" {
		c.ColKey = "custom_" + c.ColKey
	}

	// 检查是否已存在
	var count int
	database.DB.QueryRow("SELECT COUNT(*) FROM merchant_custom_columns WHERE col_key = ?", c.ColKey).Scan(&count)
	if count > 0 {
		sendError(w, "列标识已存在", http.StatusConflict)
		return
	}

	c.ID = uuid.New().String()
	if c.ColType == "" {
		c.ColType = "text"
	}
	if c.ColWidth == "" {
		c.ColWidth = "120px"
	}
	user := r.Header.Get("X-Operator")

	_, err := database.DB.Exec(`
		INSERT INTO merchant_custom_columns (id, col_key, col_title, col_type, col_width, sort_order, is_active, created_by)
		VALUES (?, ?, ?, ?, ?, ?, TRUE, ?)
	`, c.ID, c.ColKey, c.ColTitle, c.ColType, c.ColWidth, c.SortOrder, user)

	if err != nil {
		log.Printf("[商户列] 创建失败: %v", err)
		sendError(w, "创建失败", http.StatusInternalServerError)
		return
	}

	AddAuditLogFromRequest(r, "CREATE_MERCHANT_COLUMN", c.ID, user, "", "", fmt.Sprintf("创建商户列: %s", c.ColTitle))

	respondJSON(w, http.StatusCreated, c)
}

// HandleUpdateMerchantColumn 更新自定义列
func HandleUpdateMerchantColumn(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var c MerchantCustomColumn
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		sendError(w, "请求参数无效", http.StatusBadRequest)
		return
	}

	user := r.Header.Get("X-Operator")

	_, err := database.DB.Exec(`
		UPDATE merchant_custom_columns 
		SET col_title = ?, col_type = ?, col_width = ?, sort_order = ?
		WHERE id = ?
	`, c.ColTitle, c.ColType, c.ColWidth, c.SortOrder, id)

	if err != nil {
		log.Printf("[商户列] 更新失败: %v", err)
		sendError(w, "更新失败", http.StatusInternalServerError)
		return
	}

	AddAuditLogFromRequest(r, "UPDATE_MERCHANT_COLUMN", id, user, "", "", fmt.Sprintf("更新商户列: %s", c.ColTitle))

	respondJSON(w, http.StatusOK, map[string]string{"message": "更新成功"})
}

// HandleDeleteMerchantColumn 删除自定义列（软删除）
func HandleDeleteMerchantColumn(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	user := r.Header.Get("X-Operator")

	// 软删除：设置 is_active = FALSE
	_, err := database.DB.Exec("UPDATE merchant_custom_columns SET is_active = FALSE WHERE id = ?", id)
	if err != nil {
		sendError(w, "删除失败", http.StatusInternalServerError)
		return
	}

	AddAuditLogFromRequest(r, "DELETE_MERCHANT_COLUMN", id, user, "", "", "删除商户列")

	respondJSON(w, http.StatusOK, map[string]string{"message": "删除成功"})
}
