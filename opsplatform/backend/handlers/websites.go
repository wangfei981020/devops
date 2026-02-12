package handlers

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"opsplatform/database"
	"opsplatform/models"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// 加密密钥（生产环境应从环境变量读取）
var websiteEncryptKey = getWebsiteEncryptKey()

func getWebsiteEncryptKey() []byte {
	key := os.Getenv("WEBSITE_ENCRYPT_KEY")
	if key == "" {
		key = "ops-platform-website-key-32byte!" // 32 bytes for AES-256
	}
	// 确保是32字节
	if len(key) < 32 {
		key = key + "00000000000000000000000000000000"
	}
	return []byte(key[:32])
}

// encryptPassword 加密密码
func encryptPassword(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}

	block, err := aes.NewCipher(websiteEncryptKey)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decryptPassword 解密密码
func decryptPassword(encrypted string) (string, error) {
	if encrypted == "" {
		return "", nil
	}

	ciphertext, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(websiteEncryptKey)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// HandleGetWebsites 获取所有网站
func HandleGetWebsites(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`
		SELECT id, name, url, COALESCE(category, 'internal'), COALESCE(description, ''), 
		       COALESCE(icon, ''), COALESCE(biz_contact, ''), COALESCE(biz_phone, ''),
		       COALESCE(tech_contact, ''), COALESCE(tech_phone, ''),
		       COALESCE(username, ''), COALESCE(password, ''),
		       COALESCE(contract_no, ''), contract_start, contract_end, COALESCE(cost_info, ''),
		       COALESCE(sort_order, 0), COALESCE(status, 'active'),
		       COALESCE(created_at, NOW()), COALESCE(created_by, ''),
		       COALESCE(updated_at, NOW()), COALESCE(updated_by, '')
		FROM websites ORDER BY sort_order ASC, created_at DESC
	`)
	if err != nil {
		log.Printf("[ERROR] 查询网站列表失败: %v", err)
		sendError(w, "查询网站列表失败", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	websites := make([]*models.Website, 0)
	for rows.Next() {
		site := &models.Website{}
		var password string
		var contractStart, contractEnd sql.NullString
		err := rows.Scan(&site.ID, &site.Name, &site.URL, &site.Category, &site.Description,
			&site.Icon, &site.BizContact, &site.BizPhone, &site.TechContact, &site.TechPhone,
			&site.Username, &password, &site.ContractNo, &contractStart, &contractEnd, &site.CostInfo,
			&site.SortOrder, &site.Status,
			&site.CreatedAt, &site.CreatedBy, &site.UpdatedAt, &site.UpdatedBy)
		if contractStart.Valid {
			site.ContractStart = contractStart.String
		}
		if contractEnd.Valid {
			site.ContractEnd = contractEnd.String
		}
		if err != nil {
			log.Printf("[ERROR] 扫描网站数据失败: %v", err)
			continue
		}
		// 不返回密码到前端，只标记是否有密码
		if password != "" {
			site.Password = "******"
		}
		websites = append(websites, site)
	}

	sendSuccess(w, map[string]interface{}{"websites": websites})
}

// HandleCreateWebsite 创建网站
func HandleCreateWebsite(w http.ResponseWriter, r *http.Request) {
	var site models.Website
	if err := json.NewDecoder(r.Body).Decode(&site); err != nil {
		sendError(w, "无效的请求体", http.StatusBadRequest)
		return
	}

	if site.Name == "" || site.URL == "" {
		sendError(w, "网站名称和URL为必填项", http.StatusBadRequest)
		return
	}

	operator := r.Header.Get("X-Operator")
	if operator == "" {
		operator = "system"
	}

	site.ID = uuid.New().String()
	site.CreatedAt = time.Now().Format("2006-01-02 15:04:05")
	site.CreatedBy = operator
	site.UpdatedAt = site.CreatedAt
	site.UpdatedBy = operator

	if site.Status == "" {
		site.Status = "active"
	}
	if site.Category == "" {
		site.Category = "internal"
	}

	// 加密密码
	encryptedPassword := ""
	if site.Password != "" {
		var err error
		encryptedPassword, err = encryptPassword(site.Password)
		if err != nil {
			log.Printf("[ERROR] 加密密码失败: %v", err)
			sendError(w, "密码加密失败", http.StatusInternalServerError)
			return
		}
	}

	// 处理合同日期
	var contractStart, contractEnd interface{}
	if site.ContractStart != "" {
		contractStart = site.ContractStart
	} else {
		contractStart = nil
	}
	if site.ContractEnd != "" {
		contractEnd = site.ContractEnd
	} else {
		contractEnd = nil
	}

	_, err := database.DB.Exec(`
		INSERT INTO websites (id, name, url, category, description, icon, 
			biz_contact, biz_phone, tech_contact, tech_phone,
			username, password, contract_no, contract_start, contract_end, cost_info,
			sort_order, status, created_at, created_by, updated_at, updated_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, site.ID, site.Name, site.URL, site.Category, site.Description, site.Icon,
		site.BizContact, site.BizPhone, site.TechContact, site.TechPhone,
		site.Username, encryptedPassword, site.ContractNo, contractStart, contractEnd, site.CostInfo,
		site.SortOrder, site.Status, site.CreatedAt, site.CreatedBy, site.UpdatedAt, site.UpdatedBy)

	if err != nil {
		log.Printf("[ERROR] 创建网站失败: %v", err)
		sendError(w, "创建网站失败", http.StatusInternalServerError)
		return
	}

	// 记录审计日志
	AddAuditLogFromRequest(r, "CREATE_WEBSITE", site.ID, operator, "", "", fmt.Sprintf("创建网站: %s", site.Name))

	site.Password = "" // 不返回密码
	sendSuccess(w, site)
}

// HandleUpdateWebsite 更新网站
func HandleUpdateWebsite(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var site models.Website
	if err := json.NewDecoder(r.Body).Decode(&site); err != nil {
		sendError(w, "无效的请求体", http.StatusBadRequest)
		return
	}

	operator := r.Header.Get("X-Operator")
	if operator == "" {
		operator = "system"
	}

	site.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
	site.UpdatedBy = operator

	// 如果密码字段不为空且不是掩码，则加密新密码
	var updatePassword bool
	var encryptedPassword string
	if site.Password != "" && site.Password != "******" {
		var err error
		encryptedPassword, err = encryptPassword(site.Password)
		if err != nil {
			log.Printf("[ERROR] 加密密码失败: %v", err)
			sendError(w, "密码加密失败", http.StatusInternalServerError)
			return
		}
		updatePassword = true
	}

	var result sql.Result
	var err error

	// 处理合同日期
	var contractStart, contractEnd interface{}
	if site.ContractStart != "" {
		contractStart = site.ContractStart
	} else {
		contractStart = nil
	}
	if site.ContractEnd != "" {
		contractEnd = site.ContractEnd
	} else {
		contractEnd = nil
	}

	if updatePassword {
		result, err = database.DB.Exec(`
			UPDATE websites SET name=?, url=?, category=?, description=?, icon=?, 
			       biz_contact=?, biz_phone=?, tech_contact=?, tech_phone=?,
			       username=?, password=?, contract_no=?, contract_start=?, contract_end=?, cost_info=?,
			       sort_order=?, status=?, updated_at=?, updated_by=?
			WHERE id=?
		`, site.Name, site.URL, site.Category, site.Description, site.Icon,
			site.BizContact, site.BizPhone, site.TechContact, site.TechPhone,
			site.Username, encryptedPassword, site.ContractNo, contractStart, contractEnd, site.CostInfo,
			site.SortOrder, site.Status, site.UpdatedAt, site.UpdatedBy, id)
	} else {
		result, err = database.DB.Exec(`
			UPDATE websites SET name=?, url=?, category=?, description=?, icon=?, 
			       biz_contact=?, biz_phone=?, tech_contact=?, tech_phone=?,
			       username=?, contract_no=?, contract_start=?, contract_end=?, cost_info=?,
			       sort_order=?, status=?, updated_at=?, updated_by=?
			WHERE id=?
		`, site.Name, site.URL, site.Category, site.Description, site.Icon,
			site.BizContact, site.BizPhone, site.TechContact, site.TechPhone,
			site.Username, site.ContractNo, contractStart, contractEnd, site.CostInfo,
			site.SortOrder, site.Status, site.UpdatedAt, site.UpdatedBy, id)
	}

	if err != nil {
		log.Printf("[ERROR] 更新网站失败: %v", err)
		sendError(w, "更新网站失败", http.StatusInternalServerError)
		return
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		sendError(w, "网站不存在", http.StatusNotFound)
		return
	}

	AddAuditLogFromRequest(r, "UPDATE_WEBSITE", id, operator, "", "", fmt.Sprintf("更新网站: %s", site.Name))

	site.ID = id
	site.Password = ""
	sendSuccess(w, site)
}

// HandleDeleteWebsite 删除网站
func HandleDeleteWebsite(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	operator := r.Header.Get("X-Operator")
	if operator == "" {
		operator = "system"
	}

	// 获取网站名称用于审计
	var name string
	database.DB.QueryRow("SELECT name FROM websites WHERE id = ?", id).Scan(&name)

	result, err := database.DB.Exec("DELETE FROM websites WHERE id = ?", id)
	if err != nil {
		log.Printf("[ERROR] 删除网站失败: %v", err)
		sendError(w, "删除网站失败", http.StatusInternalServerError)
		return
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		sendError(w, "网站不存在", http.StatusNotFound)
		return
	}

	AddAuditLogFromRequest(r, "DELETE_WEBSITE", id, operator, "", "", fmt.Sprintf("删除网站: %s", name))

	sendSuccess(w, map[string]string{"message": "删除成功"})
}

// HandleGetWebsitePassword 获取网站密码（解密）
func HandleGetWebsitePassword(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	operator := r.Header.Get("X-Operator")
	if operator == "" {
		operator = "system"
	}

	var encryptedPassword string
	var name string
	err := database.DB.QueryRow("SELECT name, COALESCE(password, '') FROM websites WHERE id = ?", id).Scan(&name, &encryptedPassword)
	if err != nil {
		if err == sql.ErrNoRows {
			sendError(w, "网站不存在", http.StatusNotFound)
			return
		}
		log.Printf("[ERROR] 查询网站密码失败: %v", err)
		sendError(w, "查询失败", http.StatusInternalServerError)
		return
	}

	if encryptedPassword == "" {
		sendError(w, "该网站未设置密码", http.StatusBadRequest)
		return
	}

	password, err := decryptPassword(encryptedPassword)
	if err != nil {
		log.Printf("[ERROR] 解密密码失败: %v", err)
		sendError(w, "密码解密失败", http.StatusInternalServerError)
		return
	}

	// 记录审计日志
	AddAuditLogFromRequest(r, "VIEW_WEBSITE_PASSWORD", id, operator, "", "", fmt.Sprintf("查看网站密码: %s", name))

	sendSuccess(w, map[string]string{"password": password})
}

// ========== 厅方管理 ==========

// HandleGetWebsiteHalls 获取网站的所有厅方
func HandleGetWebsiteHalls(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	websiteID := vars["id"]

	rows, err := database.DB.Query(`
		SELECT id, website_id, hall_name, COALESCE(contact, ''), COALESCE(phone, ''),
		       COALESCE(username, ''), COALESCE(password, ''), COALESCE(remark, ''),
		       COALESCE(status, 'active'), COALESCE(created_at, NOW()), COALESCE(created_by, ''),
		       COALESCE(updated_at, NOW()), COALESCE(updated_by, '')
		FROM website_halls WHERE website_id = ? ORDER BY created_at DESC
	`, websiteID)
	if err != nil {
		log.Printf("[ERROR] 查询厅方列表失败: %v", err)
		sendError(w, "查询厅方列表失败", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	halls := make([]*models.WebsiteHall, 0)
	for rows.Next() {
		hall := &models.WebsiteHall{}
		var password string
		err := rows.Scan(&hall.ID, &hall.WebsiteID, &hall.HallName, &hall.Contact, &hall.Phone,
			&hall.Username, &password, &hall.Remark, &hall.Status,
			&hall.CreatedAt, &hall.CreatedBy, &hall.UpdatedAt, &hall.UpdatedBy)
		if err != nil {
			log.Printf("[ERROR] 扫描厅方数据失败: %v", err)
			continue
		}
		if password != "" {
			hall.Password = "******"
		}
		halls = append(halls, hall)
	}

	sendSuccess(w, map[string]interface{}{"halls": halls})
}

// HandleCreateWebsiteHall 创建厅方
func HandleCreateWebsiteHall(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	websiteID := vars["id"]

	var hall models.WebsiteHall
	if err := json.NewDecoder(r.Body).Decode(&hall); err != nil {
		sendError(w, "无效的请求体", http.StatusBadRequest)
		return
	}

	if hall.HallName == "" {
		sendError(w, "厅方名称为必填项", http.StatusBadRequest)
		return
	}

	operator := r.Header.Get("X-Operator")
	if operator == "" {
		operator = "system"
	}

	hall.ID = uuid.New().String()
	hall.WebsiteID = websiteID
	hall.CreatedAt = time.Now().Format("2006-01-02 15:04:05")
	hall.CreatedBy = operator
	hall.UpdatedAt = hall.CreatedAt
	hall.UpdatedBy = operator

	if hall.Status == "" {
		hall.Status = "active"
	}

	// 加密密码
	encryptedPassword := ""
	if hall.Password != "" {
		var err error
		encryptedPassword, err = encryptPassword(hall.Password)
		if err != nil {
			log.Printf("[ERROR] 加密密码失败: %v", err)
			sendError(w, "密码加密失败", http.StatusInternalServerError)
			return
		}
	}

	_, err := database.DB.Exec(`
		INSERT INTO website_halls (id, website_id, hall_name, contact, phone, username, password, remark, status, created_at, created_by, updated_at, updated_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, hall.ID, hall.WebsiteID, hall.HallName, hall.Contact, hall.Phone,
		hall.Username, encryptedPassword, hall.Remark, hall.Status,
		hall.CreatedAt, hall.CreatedBy, hall.UpdatedAt, hall.UpdatedBy)

	if err != nil {
		log.Printf("[ERROR] 创建厅方失败: %v", err)
		sendError(w, "创建厅方失败", http.StatusInternalServerError)
		return
	}

	AddAuditLogFromRequest(r, "CREATE_WEBSITE_HALL", hall.ID, operator, "", "", fmt.Sprintf("创建厅方: %s", hall.HallName))

	hall.Password = ""
	sendSuccess(w, hall)
}

// HandleUpdateWebsiteHall 更新厅方
func HandleUpdateWebsiteHall(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	hallID := vars["hallId"]

	var hall models.WebsiteHall
	if err := json.NewDecoder(r.Body).Decode(&hall); err != nil {
		sendError(w, "无效的请求体", http.StatusBadRequest)
		return
	}

	operator := r.Header.Get("X-Operator")
	if operator == "" {
		operator = "system"
	}

	hall.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
	hall.UpdatedBy = operator

	var updatePassword bool
	var encryptedPassword string
	if hall.Password != "" && hall.Password != "******" {
		var err error
		encryptedPassword, err = encryptPassword(hall.Password)
		if err != nil {
			log.Printf("[ERROR] 加密密码失败: %v", err)
			sendError(w, "密码加密失败", http.StatusInternalServerError)
			return
		}
		updatePassword = true
	}

	var result sql.Result
	var err error

	if updatePassword {
		result, err = database.DB.Exec(`
			UPDATE website_halls SET hall_name=?, contact=?, phone=?, username=?, password=?, remark=?, status=?, updated_at=?, updated_by=?
			WHERE id=?
		`, hall.HallName, hall.Contact, hall.Phone, hall.Username, encryptedPassword, hall.Remark, hall.Status,
			hall.UpdatedAt, hall.UpdatedBy, hallID)
	} else {
		result, err = database.DB.Exec(`
			UPDATE website_halls SET hall_name=?, contact=?, phone=?, username=?, remark=?, status=?, updated_at=?, updated_by=?
			WHERE id=?
		`, hall.HallName, hall.Contact, hall.Phone, hall.Username, hall.Remark, hall.Status,
			hall.UpdatedAt, hall.UpdatedBy, hallID)
	}

	if err != nil {
		log.Printf("[ERROR] 更新厅方失败: %v", err)
		sendError(w, "更新厅方失败", http.StatusInternalServerError)
		return
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		sendError(w, "厅方不存在", http.StatusNotFound)
		return
	}

	AddAuditLogFromRequest(r, "UPDATE_WEBSITE_HALL", hallID, operator, "", "", fmt.Sprintf("更新厅方: %s", hall.HallName))

	hall.ID = hallID
	hall.Password = ""
	sendSuccess(w, hall)
}

// HandleDeleteWebsiteHall 删除厅方
func HandleDeleteWebsiteHall(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	hallID := vars["hallId"]
	operator := r.Header.Get("X-Operator")
	if operator == "" {
		operator = "system"
	}

	var hallName string
	database.DB.QueryRow("SELECT hall_name FROM website_halls WHERE id = ?", hallID).Scan(&hallName)

	result, err := database.DB.Exec("DELETE FROM website_halls WHERE id = ?", hallID)
	if err != nil {
		log.Printf("[ERROR] 删除厅方失败: %v", err)
		sendError(w, "删除厅方失败", http.StatusInternalServerError)
		return
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		sendError(w, "厅方不存在", http.StatusNotFound)
		return
	}

	AddAuditLogFromRequest(r, "DELETE_WEBSITE_HALL", hallID, operator, "", "", fmt.Sprintf("删除厅方: %s", hallName))

	sendSuccess(w, map[string]string{"message": "删除成功"})
}

// HandleGetHallPassword 获取厅方密码
func HandleGetHallPassword(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	hallID := vars["hallId"]
	operator := r.Header.Get("X-Operator")
	if operator == "" {
		operator = "system"
	}

	var encryptedPassword string
	var hallName string
	err := database.DB.QueryRow("SELECT hall_name, COALESCE(password, '') FROM website_halls WHERE id = ?", hallID).Scan(&hallName, &encryptedPassword)
	if err != nil {
		if err == sql.ErrNoRows {
			sendError(w, "厅方不存在", http.StatusNotFound)
			return
		}
		log.Printf("[ERROR] 查询厅方密码失败: %v", err)
		sendError(w, "查询失败", http.StatusInternalServerError)
		return
	}

	if encryptedPassword == "" {
		sendError(w, "该厅方未设置密码", http.StatusBadRequest)
		return
	}

	password, err := decryptPassword(encryptedPassword)
	if err != nil {
		log.Printf("[ERROR] 解密密码失败: %v", err)
		sendError(w, "密码解密失败", http.StatusInternalServerError)
		return
	}

	AddAuditLogFromRequest(r, "VIEW_HALL_PASSWORD", hallID, operator, "", "", fmt.Sprintf("查看厅方密码: %s", hallName))

	sendSuccess(w, map[string]string{"password": password})
}
