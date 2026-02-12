package handlers

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"opsplatform/database"

	"github.com/google/uuid"
	"golang.org/x/crypto/pbkdf2"
)

// ============ 数据结构 ============

// PasswordEntry 密码条目
type PasswordEntry struct {
	ID        string `json:"id"`
	UserID    string `json:"userId"`
	GroupName string `json:"groupName"`
	Title     string `json:"title"`
	Username  string `json:"username"`
	Password  string `json:"password"` // 解密后的密码（仅在响应中）
	URL       string `json:"url"`
	Notes     string `json:"notes"`
	Icon      string `json:"icon"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// PasswordGroup 密码分组
type PasswordGroup struct {
	ID        string `json:"id"`
	UserID    string `json:"userId"`
	Name      string `json:"name"`
	Icon      string `json:"icon"`
	SortOrder int    `json:"sortOrder"`
	Count     int    `json:"count"`
}

// MasterPasswordRequest 主密码请求
type MasterPasswordRequest struct {
	MasterPassword    string `json:"masterPassword"`
	NewMasterPassword string `json:"newMasterPassword,omitempty"`
	ResetToken        string `json:"resetToken,omitempty"`
}

// ============ 加密工具函数 ============

// deriveKey 使用 PBKDF2 从主密码派生密钥
func deriveKey(password, salt string, iterations int) []byte {
	return pbkdf2.Key([]byte(password), []byte(salt), iterations, 32, sha256.New)
}

// generateSalt 生成随机盐值
func generateSalt() string {
	salt := make([]byte, 32)
	rand.Read(salt)
	return hex.EncodeToString(salt)
}

// encrypt 使用 AES-256-GCM 加密
func encrypt(plaintext string, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
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

// decrypt 使用 AES-256-GCM 解密
func decrypt(ciphertext string, key []byte) (string, error) {
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, cipherData := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, cipherData, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// hashMasterPassword 哈希主密码
func hashMasterPassword(password, salt string) string {
	hash := sha256.Sum256([]byte(password + salt))
	return hex.EncodeToString(hash[:])
}

// ============ 主密码管理 ============

// HandleSetupMasterPassword 设置主密码
func HandleSetupMasterPassword(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		sendError(w, "未登录", http.StatusUnauthorized)
		return
	}

	var req MasterPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, "请求格式错误", http.StatusBadRequest)
		return
	}

	if len(req.MasterPassword) < 8 {
		sendError(w, "主密码长度至少8位", http.StatusBadRequest)
		return
	}

	// 检查是否已设置主密码
	var exists int
	err := database.DB.QueryRow("SELECT 1 FROM password_master WHERE user_id = ?", userID).Scan(&exists)
	if err != sql.ErrNoRows {
		sendError(w, "主密码已设置，请使用重置功能", http.StatusBadRequest)
		return
	}

	// 生成盐值和派生密钥
	salt := generateSalt()
	iterations := 100000
	masterHash := hashMasterPassword(req.MasterPassword, salt)

	// 生成随机加密密钥（用于加密密码条目）
	encryptionKey := make([]byte, 32)
	rand.Read(encryptionKey)
	encryptionKeyHex := hex.EncodeToString(encryptionKey)

	// 使用主密码派生的密钥加密加密密钥
	derivedKey := deriveKey(req.MasterPassword, salt, iterations)
	encryptedKey, err := encrypt(encryptionKeyHex, derivedKey)
	if err != nil {
		sendError(w, "加密失败", http.StatusInternalServerError)
		return
	}

	// 保存到数据库
	_, err = database.DB.Exec(`
		INSERT INTO password_master (user_id, master_hash, encryption_key_encrypted, salt, iterations, created_at)
		VALUES (?, ?, ?, ?, ?, NOW())
	`, userID, masterHash, encryptedKey, salt, iterations)

	if err != nil {
		log.Printf("保存主密码失败: %v", err)
		sendError(w, "保存失败", http.StatusInternalServerError)
		return
	}

	// 创建默认分组
	database.DB.Exec(`
		INSERT INTO password_groups (id, user_id, name, icon, sort_order, created_at)
		VALUES (?, ?, '默认分组', 'folder', 0, NOW())
	`, uuid.New().String(), userID)

	// 记录审计日志
	AddAuditLogFromRequest(r, "设置主密码", "password_master", userID, "", "", "用户设置了密码管理主密码")

	sendSuccess(w, map[string]interface{}{
		"message": "主密码设置成功",
	})
}

// HandleVerifyMasterPassword 验证主密码
func HandleVerifyMasterPassword(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		sendError(w, "未登录", http.StatusUnauthorized)
		return
	}

	var req MasterPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, "请求格式错误", http.StatusBadRequest)
		return
	}

	// 获取主密码信息
	var masterHash, salt, encryptedKey string
	var iterations int
	err := database.DB.QueryRow(`
		SELECT master_hash, salt, encryption_key_encrypted, iterations
		FROM password_master WHERE user_id = ?
	`, userID).Scan(&masterHash, &salt, &encryptedKey, &iterations)

	if err == sql.ErrNoRows {
		sendError(w, "请先设置主密码", http.StatusBadRequest)
		return
	}
	if err != nil {
		sendError(w, "查询失败", http.StatusInternalServerError)
		return
	}

	// 验证主密码
	inputHash := hashMasterPassword(req.MasterPassword, salt)
	if inputHash != masterHash {
		sendError(w, "主密码错误", http.StatusUnauthorized)
		return
	}

	// 返回会话令牌（实际可用于后续操作）
	sessionToken := uuid.New().String()

	sendSuccess(w, map[string]interface{}{
		"message":      "验证成功",
		"sessionToken": sessionToken,
	})
}

// HandleResetMasterPassword 重置主密码
func HandleResetMasterPassword(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		sendError(w, "未登录", http.StatusUnauthorized)
		return
	}

	var req MasterPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, "请求格式错误", http.StatusBadRequest)
		return
	}

	if len(req.NewMasterPassword) < 8 {
		sendError(w, "新密码长度至少8位", http.StatusBadRequest)
		return
	}

	// 获取当前主密码信息
	var masterHash, salt, encryptedKey string
	var iterations int
	err := database.DB.QueryRow(`
		SELECT master_hash, salt, encryption_key_encrypted, iterations
		FROM password_master WHERE user_id = ?
	`, userID).Scan(&masterHash, &salt, &encryptedKey, &iterations)

	if err == sql.ErrNoRows {
		sendError(w, "请先设置主密码", http.StatusBadRequest)
		return
	}

	// 验证旧主密码
	inputHash := hashMasterPassword(req.MasterPassword, salt)
	if inputHash != masterHash {
		sendError(w, "当前主密码错误", http.StatusUnauthorized)
		return
	}

	// 解密加密密钥
	oldDerivedKey := deriveKey(req.MasterPassword, salt, iterations)
	encryptionKeyHex, err := decrypt(encryptedKey, oldDerivedKey)
	if err != nil {
		sendError(w, "解密失败", http.StatusInternalServerError)
		return
	}

	// 使用新密码重新加密
	newSalt := generateSalt()
	newMasterHash := hashMasterPassword(req.NewMasterPassword, newSalt)
	newDerivedKey := deriveKey(req.NewMasterPassword, newSalt, iterations)
	newEncryptedKey, err := encrypt(encryptionKeyHex, newDerivedKey)
	if err != nil {
		sendError(w, "加密失败", http.StatusInternalServerError)
		return
	}

	// 更新数据库
	_, err = database.DB.Exec(`
		UPDATE password_master 
		SET master_hash = ?, salt = ?, encryption_key_encrypted = ?, updated_at = NOW()
		WHERE user_id = ?
	`, newMasterHash, newSalt, newEncryptedKey, userID)

	if err != nil {
		sendError(w, "更新失败", http.StatusInternalServerError)
		return
	}

	// 记录审计日志
	AddAuditLogFromRequest(r, "重置主密码", "password_master", userID, "", "", "用户重置了密码管理主密码")

	sendSuccess(w, map[string]interface{}{
		"message": "主密码重置成功",
	})
}

// HandleForceResetMasterPassword 强制重置主密码（管理员功能，会清空所有密码）
func HandleForceResetMasterPassword(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		sendError(w, "未登录", http.StatusUnauthorized)
		return
	}

	var req MasterPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, "请求格式错误", http.StatusBadRequest)
		return
	}

	if len(req.NewMasterPassword) < 8 {
		sendError(w, "新密码长度至少8位", http.StatusBadRequest)
		return
	}

	// 删除所有密码条目
	_, err := database.DB.Exec("DELETE FROM password_vault WHERE user_id = ?", userID)
	if err != nil {
		log.Printf("删除密码条目失败: %v", err)
	}

	// 删除旧的主密码记录
	database.DB.Exec("DELETE FROM password_master WHERE user_id = ?", userID)

	// 生成新的盐值和密钥
	salt := generateSalt()
	iterations := 100000
	masterHash := hashMasterPassword(req.NewMasterPassword, salt)

	encryptionKey := make([]byte, 32)
	rand.Read(encryptionKey)
	encryptionKeyHex := hex.EncodeToString(encryptionKey)

	derivedKey := deriveKey(req.NewMasterPassword, salt, iterations)
	encryptedKey, err := encrypt(encryptionKeyHex, derivedKey)
	if err != nil {
		sendError(w, "加密失败", http.StatusInternalServerError)
		return
	}

	// 保存新的主密码
	_, err = database.DB.Exec(`
		INSERT INTO password_master (user_id, master_hash, encryption_key_encrypted, salt, iterations, created_at)
		VALUES (?, ?, ?, ?, ?, NOW())
	`, userID, masterHash, encryptedKey, salt, iterations)

	if err != nil {
		sendError(w, "保存失败", http.StatusInternalServerError)
		return
	}

	// 记录审计日志
	AddAuditLogFromRequest(r, "强制重置主密码", "password_master", userID, "", "", "用户强制重置了主密码（所有密码已清空）")

	sendSuccess(w, map[string]interface{}{
		"message": "主密码已重置，所有密码已清空",
	})
}

// HandleCheckMasterPassword 检查是否已设置主密码
func HandleCheckMasterPassword(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		sendError(w, "未登录", http.StatusUnauthorized)
		return
	}

	var exists int
	err := database.DB.QueryRow("SELECT 1 FROM password_master WHERE user_id = ?", userID).Scan(&exists)

	sendSuccess(w, map[string]interface{}{
		"hasSetup": err != sql.ErrNoRows,
	})
}

// ============ 密码条目管理 ============

// getEncryptionKey 获取用户的加密密钥
func getEncryptionKey(userID, masterPassword string) ([]byte, error) {
	var masterHash, salt, encryptedKey string
	var iterations int
	err := database.DB.QueryRow(`
		SELECT master_hash, salt, encryption_key_encrypted, iterations
		FROM password_master WHERE user_id = ?
	`, userID).Scan(&masterHash, &salt, &encryptedKey, &iterations)

	if err != nil {
		return nil, fmt.Errorf("未设置主密码")
	}

	// 验证主密码
	inputHash := hashMasterPassword(masterPassword, salt)
	if inputHash != masterHash {
		return nil, fmt.Errorf("主密码错误")
	}

	// 解密加密密钥
	derivedKey := deriveKey(masterPassword, salt, iterations)
	encryptionKeyHex, err := decrypt(encryptedKey, derivedKey)
	if err != nil {
		return nil, fmt.Errorf("解密失败")
	}

	return hex.DecodeString(encryptionKeyHex)
}

// HandleGetPasswords 获取密码列表
func HandleGetPasswords(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	masterPassword := r.Header.Get("X-Master-Password")

	if userID == "" {
		sendError(w, "未登录", http.StatusUnauthorized)
		return
	}

	encryptionKey, err := getEncryptionKey(userID, masterPassword)
	if err != nil {
		sendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	// 查询密码条目
	rows, err := database.DB.Query(`
		SELECT id, user_id, group_name, title, username, password_encrypted, url, notes, icon, created_at, updated_at
		FROM password_vault WHERE user_id = ? ORDER BY group_name, title
	`, userID)
	if err != nil {
		sendError(w, "查询失败", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var passwords []PasswordEntry
	for rows.Next() {
		var p PasswordEntry
		var encryptedPassword string
		var createdAt, updatedAt sql.NullTime

		if err := rows.Scan(&p.ID, &p.UserID, &p.GroupName, &p.Title, &p.Username, &encryptedPassword, &p.URL, &p.Notes, &p.Icon, &createdAt, &updatedAt); err != nil {
			continue
		}

		// 解密密码
		decryptedPassword, err := decrypt(encryptedPassword, encryptionKey)
		if err != nil {
			p.Password = "***解密失败***"
		} else {
			p.Password = decryptedPassword
		}

		if createdAt.Valid {
			p.CreatedAt = createdAt.Time.Format("2006-01-02 15:04:05")
		}
		if updatedAt.Valid {
			p.UpdatedAt = updatedAt.Time.Format("2006-01-02 15:04:05")
		}

		passwords = append(passwords, p)
	}

	sendSuccess(w, passwords)
}

// HandleAddPassword 添加密码条目
func HandleAddPassword(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	masterPassword := r.Header.Get("X-Master-Password")

	if userID == "" {
		sendError(w, "未登录", http.StatusUnauthorized)
		return
	}

	encryptionKey, err := getEncryptionKey(userID, masterPassword)
	if err != nil {
		sendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	var entry PasswordEntry
	if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
		sendError(w, "请求格式错误", http.StatusBadRequest)
		return
	}

	if entry.Title == "" {
		sendError(w, "标题不能为空", http.StatusBadRequest)
		return
	}

	// 加密密码
	encryptedPassword, err := encrypt(entry.Password, encryptionKey)
	if err != nil {
		sendError(w, "加密失败", http.StatusInternalServerError)
		return
	}

	entry.ID = uuid.New().String()
	if entry.GroupName == "" {
		entry.GroupName = "默认分组"
	}
	if entry.Icon == "" {
		entry.Icon = "key"
	}

	_, err = database.DB.Exec(`
		INSERT INTO password_vault (id, user_id, group_name, title, username, password_encrypted, url, notes, icon, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NOW(), NOW())
	`, entry.ID, userID, entry.GroupName, entry.Title, entry.Username, encryptedPassword, entry.URL, entry.Notes, entry.Icon)

	if err != nil {
		log.Printf("添加密码失败: %v", err)
		sendError(w, "添加失败", http.StatusInternalServerError)
		return
	}

	// 记录审计日志
	AddAuditLogFromRequest(r, "添加密码", "password_vault", userID, "", entry.Title, "添加密码条目: "+entry.Title)

	sendSuccess(w, map[string]interface{}{
		"id":      entry.ID,
		"message": "添加成功",
	})
}

// HandleUpdatePassword 更新密码条目
func HandleUpdatePassword(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	masterPassword := r.Header.Get("X-Master-Password")

	if userID == "" {
		sendError(w, "未登录", http.StatusUnauthorized)
		return
	}

	encryptionKey, err := getEncryptionKey(userID, masterPassword)
	if err != nil {
		sendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	var entry PasswordEntry
	if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
		sendError(w, "请求格式错误", http.StatusBadRequest)
		return
	}

	if entry.ID == "" {
		sendError(w, "ID不能为空", http.StatusBadRequest)
		return
	}

	// 加密密码
	encryptedPassword, err := encrypt(entry.Password, encryptionKey)
	if err != nil {
		sendError(w, "加密失败", http.StatusInternalServerError)
		return
	}

	_, err = database.DB.Exec(`
		UPDATE password_vault 
		SET group_name = ?, title = ?, username = ?, password_encrypted = ?, url = ?, notes = ?, icon = ?, updated_at = NOW()
		WHERE id = ? AND user_id = ?
	`, entry.GroupName, entry.Title, entry.Username, encryptedPassword, entry.URL, entry.Notes, entry.Icon, entry.ID, userID)

	if err != nil {
		sendError(w, "更新失败", http.StatusInternalServerError)
		return
	}

	// 记录审计日志
	AddAuditLogFromRequest(r, "更新密码", "password_vault", userID, "", entry.Title, "更新密码条目: "+entry.Title)

	sendSuccess(w, map[string]interface{}{
		"message": "更新成功",
	})
}

// HandleDeletePassword 删除密码条目
func HandleDeletePassword(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	id := r.URL.Query().Get("id")

	if userID == "" {
		sendError(w, "未登录", http.StatusUnauthorized)
		return
	}

	if id == "" {
		sendError(w, "ID不能为空", http.StatusBadRequest)
		return
	}

	// 获取标题用于审计日志
	var title string
	database.DB.QueryRow("SELECT title FROM password_vault WHERE id = ? AND user_id = ?", id, userID).Scan(&title)

	_, err := database.DB.Exec("DELETE FROM password_vault WHERE id = ? AND user_id = ?", id, userID)
	if err != nil {
		sendError(w, "删除失败", http.StatusInternalServerError)
		return
	}

	// 记录审计日志
	AddAuditLogFromRequest(r, "删除密码", "password_vault", userID, "", title, "删除密码条目: "+title)

	sendSuccess(w, map[string]interface{}{
		"message": "删除成功",
	})
}

// ============ 分组管理 ============

// HandleGetPasswordGroups 获取密码分组
func HandleGetPasswordGroups(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		sendError(w, "未登录", http.StatusUnauthorized)
		return
	}

	rows, err := database.DB.Query(`
		SELECT g.id, g.name, g.icon, g.sort_order, COALESCE(COUNT(p.id), 0) as count
		FROM password_groups g
		LEFT JOIN password_vault p ON g.name = p.group_name AND g.user_id = p.user_id
		WHERE g.user_id = ?
		GROUP BY g.id, g.name, g.icon, g.sort_order
		ORDER BY g.sort_order, g.name
	`, userID)
	if err != nil {
		sendError(w, "查询失败", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var groups []PasswordGroup
	for rows.Next() {
		var g PasswordGroup
		if err := rows.Scan(&g.ID, &g.Name, &g.Icon, &g.SortOrder, &g.Count); err != nil {
			continue
		}
		g.UserID = userID
		groups = append(groups, g)
	}

	sendSuccess(w, groups)
}

// HandleAddPasswordGroup 添加密码分组
func HandleAddPasswordGroup(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		sendError(w, "未登录", http.StatusUnauthorized)
		return
	}

	var group PasswordGroup
	if err := json.NewDecoder(r.Body).Decode(&group); err != nil {
		sendError(w, "请求格式错误", http.StatusBadRequest)
		return
	}

	if group.Name == "" {
		sendError(w, "分组名称不能为空", http.StatusBadRequest)
		return
	}

	group.ID = uuid.New().String()
	if group.Icon == "" {
		group.Icon = "folder"
	}

	_, err := database.DB.Exec(`
		INSERT INTO password_groups (id, user_id, name, icon, sort_order, created_at)
		VALUES (?, ?, ?, ?, ?, NOW())
	`, group.ID, userID, group.Name, group.Icon, group.SortOrder)

	if err != nil {
		if strings.Contains(err.Error(), "Duplicate") {
			sendError(w, "分组名称已存在", http.StatusBadRequest)
			return
		}
		sendError(w, "添加失败", http.StatusInternalServerError)
		return
	}

	sendSuccess(w, map[string]interface{}{
		"id":      group.ID,
		"message": "添加成功",
	})
}

// HandleDeletePasswordGroup 删除密码分组
func HandleDeletePasswordGroup(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	id := r.URL.Query().Get("id")

	if userID == "" || id == "" {
		sendError(w, "参数错误", http.StatusBadRequest)
		return
	}

	// 不能删除默认分组
	var name string
	database.DB.QueryRow("SELECT name FROM password_groups WHERE id = ? AND user_id = ?", id, userID).Scan(&name)
	if name == "默认分组" {
		sendError(w, "不能删除默认分组", http.StatusBadRequest)
		return
	}

	// 将该分组的密码移到默认分组
	database.DB.Exec("UPDATE password_vault SET group_name = '默认分组' WHERE group_name = ? AND user_id = ?", name, userID)

	// 删除分组
	database.DB.Exec("DELETE FROM password_groups WHERE id = ? AND user_id = ?", id, userID)

	sendSuccess(w, map[string]interface{}{
		"message": "删除成功",
	})
}

// ============ 导入导出（KeePass 兼容）============

// KeePassXMLDatabase KeePass XML 格式
type KeePassXMLDatabase struct {
	XMLName xml.Name        `xml:"KeePassFile"`
	Root    KeePassXMLRoot  `xml:"Root"`
}

type KeePassXMLRoot struct {
	Group KeePassXMLGroup `xml:"Group"`
}

type KeePassXMLGroup struct {
	Name    string           `xml:"Name"`
	Entry   []KeePassXMLEntry `xml:"Entry"`
	Group   []KeePassXMLGroup `xml:"Group"`
}

type KeePassXMLEntry struct {
	String []KeePassXMLString `xml:"String"`
}

type KeePassXMLString struct {
	Key   string `xml:"Key"`
	Value string `xml:"Value"`
}

// HandleExportPasswords 导出密码（CSV格式，KeePass兼容）
func HandleExportPasswords(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	masterPassword := r.Header.Get("X-Master-Password")
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "csv"
	}

	if userID == "" {
		sendError(w, "未登录", http.StatusUnauthorized)
		return
	}

	encryptionKey, err := getEncryptionKey(userID, masterPassword)
	if err != nil {
		sendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	// 查询密码
	rows, err := database.DB.Query(`
		SELECT group_name, title, username, password_encrypted, url, notes
		FROM password_vault WHERE user_id = ? ORDER BY group_name, title
	`, userID)
	if err != nil {
		sendError(w, "查询失败", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var entries []PasswordEntry
	for rows.Next() {
		var p PasswordEntry
		var encryptedPassword string
		if err := rows.Scan(&p.GroupName, &p.Title, &p.Username, &encryptedPassword, &p.URL, &p.Notes); err != nil {
			continue
		}
		decryptedPassword, _ := decrypt(encryptedPassword, encryptionKey)
		p.Password = decryptedPassword
		entries = append(entries, p)
	}

	// 记录审计日志
	AddAuditLogFromRequest(r, "导出密码", "password_vault", userID, "", "", fmt.Sprintf("导出了 %d 条密码（%s格式）", len(entries), format))

	if format == "xml" {
		// 导出为 KeePass XML
		w.Header().Set("Content-Type", "application/xml")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=passwords_%s.xml", time.Now().Format("20060102")))

		xmlDB := KeePassXMLDatabase{}
		xmlDB.Root.Group.Name = "Root"

		// 按分组组织
		groupMap := make(map[string][]KeePassXMLEntry)
		for _, e := range entries {
			entry := KeePassXMLEntry{
				String: []KeePassXMLString{
					{Key: "Title", Value: e.Title},
					{Key: "UserName", Value: e.Username},
					{Key: "Password", Value: e.Password},
					{Key: "URL", Value: e.URL},
					{Key: "Notes", Value: e.Notes},
				},
			}
			groupMap[e.GroupName] = append(groupMap[e.GroupName], entry)
		}

		for gName, gEntries := range groupMap {
			group := KeePassXMLGroup{Name: gName, Entry: gEntries}
			xmlDB.Root.Group.Group = append(xmlDB.Root.Group.Group, group)
		}

		xml.NewEncoder(w).Encode(xmlDB)
	} else {
		// 导出为 CSV（KeePass 兼容格式）
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=passwords_%s.csv", time.Now().Format("20060102")))

		// 写 BOM 头（Excel 兼容）
		w.Write([]byte{0xEF, 0xBB, 0xBF})

		writer := csv.NewWriter(w)
		// KeePass CSV 标准格式
		writer.Write([]string{"Group", "Title", "Username", "Password", "URL", "Notes"})
		for _, e := range entries {
			writer.Write([]string{e.GroupName, e.Title, e.Username, e.Password, e.URL, e.Notes})
		}
		writer.Flush()
	}
}

// HandleImportPasswords 导入密码（CSV/XML格式，KeePass兼容）
func HandleImportPasswords(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	masterPassword := r.Header.Get("X-Master-Password")

	if userID == "" {
		sendError(w, "未登录", http.StatusUnauthorized)
		return
	}

	encryptionKey, err := getEncryptionKey(userID, masterPassword)
	if err != nil {
		sendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	// 解析 multipart form
	if err := r.ParseMultipartForm(10 << 20); err != nil { // 10MB
		sendError(w, "文件太大", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		sendError(w, "请选择文件", http.StatusBadRequest)
		return
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		sendError(w, "读取文件失败", http.StatusInternalServerError)
		return
	}

	var entries []PasswordEntry
	fileName := strings.ToLower(header.Filename)

	if strings.HasSuffix(fileName, ".xml") {
		// 解析 KeePass XML
		var xmlDB KeePassXMLDatabase
		if err := xml.Unmarshal(content, &xmlDB); err != nil {
			sendError(w, "XML 格式错误", http.StatusBadRequest)
			return
		}

		// 递归解析分组
		var parseGroup func(group KeePassXMLGroup, parentName string)
		parseGroup = func(group KeePassXMLGroup, parentName string) {
			groupName := group.Name
			if parentName != "" && parentName != "Root" {
				groupName = parentName + "/" + group.Name
			}
			if groupName == "" || groupName == "Root" {
				groupName = "默认分组"
			}

			for _, entry := range group.Entry {
				p := PasswordEntry{GroupName: groupName}
				for _, s := range entry.String {
					switch s.Key {
					case "Title":
						p.Title = s.Value
					case "UserName":
						p.Username = s.Value
					case "Password":
						p.Password = s.Value
					case "URL":
						p.URL = s.Value
					case "Notes":
						p.Notes = s.Value
					}
				}
				if p.Title != "" {
					entries = append(entries, p)
				}
			}

			for _, subGroup := range group.Group {
				parseGroup(subGroup, groupName)
			}
		}
		parseGroup(xmlDB.Root.Group, "")

	} else {
		// 解析 CSV
		reader := csv.NewReader(strings.NewReader(string(content)))
		records, err := reader.ReadAll()
		if err != nil {
			sendError(w, "CSV 格式错误", http.StatusBadRequest)
			return
		}

		if len(records) < 2 {
			sendError(w, "CSV 文件为空", http.StatusBadRequest)
			return
		}

		// 查找列索引
		header := records[0]
		colMap := make(map[string]int)
		for i, col := range header {
			colMap[strings.ToLower(strings.TrimSpace(col))] = i
		}

		for i := 1; i < len(records); i++ {
			row := records[i]
			p := PasswordEntry{GroupName: "默认分组"}

			if idx, ok := colMap["group"]; ok && idx < len(row) {
				p.GroupName = row[idx]
			}
			if idx, ok := colMap["title"]; ok && idx < len(row) {
				p.Title = row[idx]
			}
			if idx, ok := colMap["username"]; ok && idx < len(row) {
				p.Username = row[idx]
			}
			if idx, ok := colMap["password"]; ok && idx < len(row) {
				p.Password = row[idx]
			}
			if idx, ok := colMap["url"]; ok && idx < len(row) {
				p.URL = row[idx]
			}
			if idx, ok := colMap["notes"]; ok && idx < len(row) {
				p.Notes = row[idx]
			}

			if p.Title != "" {
				if p.GroupName == "" {
					p.GroupName = "默认分组"
				}
				entries = append(entries, p)
			}
		}
	}

	// 导入到数据库
	successCount := 0
	for _, entry := range entries {
		encryptedPassword, err := encrypt(entry.Password, encryptionKey)
		if err != nil {
			continue
		}

		_, err = database.DB.Exec(`
			INSERT INTO password_vault (id, user_id, group_name, title, username, password_encrypted, url, notes, icon, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'key', NOW(), NOW())
		`, uuid.New().String(), userID, entry.GroupName, entry.Title, entry.Username, encryptedPassword, entry.URL, entry.Notes)

		if err == nil {
			successCount++

			// 确保分组存在
			database.DB.Exec(`
				INSERT IGNORE INTO password_groups (id, user_id, name, icon, sort_order, created_at)
				VALUES (?, ?, ?, 'folder', 0, NOW())
			`, uuid.New().String(), userID, entry.GroupName)
		}
	}

	// 记录审计日志
	AddAuditLogFromRequest(r, "导入密码", "password_vault", userID, "", "", fmt.Sprintf("导入了 %d 条密码", successCount))

	sendSuccess(w, map[string]interface{}{
		"message":      fmt.Sprintf("成功导入 %d 条密码", successCount),
		"total":        len(entries),
		"successCount": successCount,
	})
}

// HandleGeneratePassword 生成随机密码
func HandleGeneratePassword(w http.ResponseWriter, r *http.Request) {
	length := 16
	if l := r.URL.Query().Get("length"); l != "" {
		fmt.Sscanf(l, "%d", &length)
	}
	if length < 8 {
		length = 8
	}
	if length > 64 {
		length = 64
	}

	includeNumbers := r.URL.Query().Get("numbers") != "false"
	includeSymbols := r.URL.Query().Get("symbols") != "false"
	includeUppercase := r.URL.Query().Get("uppercase") != "false"
	includeLowercase := r.URL.Query().Get("lowercase") != "false"

	charset := ""
	if includeLowercase {
		charset += "abcdefghijklmnopqrstuvwxyz"
	}
	if includeUppercase {
		charset += "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	}
	if includeNumbers {
		charset += "0123456789"
	}
	if includeSymbols {
		charset += "!@#$%^&*()_+-=[]{}|;:,.<>?"
	}

	if charset == "" {
		charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	}

	password := make([]byte, length)
	for i := range password {
		b := make([]byte, 1)
		rand.Read(b)
		password[i] = charset[int(b[0])%len(charset)]
	}

	sendSuccess(w, map[string]interface{}{
		"password": string(password),
	})
}
