package handlers

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"opsplatform/database"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/pbkdf2"
)

// VaultMasterKey 主密钥结构
type VaultMasterKey struct {
	ID                   int    `json:"id"`
	UserID               string `json:"user_id"`
	MasterPasswordHash   string `json:"-"`
	EncryptedDEK         string `json:"-"`
	EncryptedDEKRecovery string `json:"-"`
	RecoveryKeyHash      string `json:"-"`
	Salt                 string `json:"-"`
	IsInitialized        bool   `json:"is_initialized"`
}

// VaultItem 密码条目
type VaultItem struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	FolderID  string `json:"folder_id"`
	Name      string `json:"name"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	URL       string `json:"url"`
	Notes     string `json:"notes"`
	Type      string `json:"type"`
	Favorite  bool   `json:"favorite"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// VaultFolder 文件夹
type VaultFolder struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	Name      string `json:"name"`
	ParentID  string `json:"parent_id"`
	Icon      string `json:"icon"`
	SortOrder int    `json:"sort_order"`
	CreatedAt string `json:"created_at"`
}

// 加密相关常量
const (
	pbkdf2Iterations = 100000
	keyLength        = 32
	saltLength       = 16
	dekLength        = 32
	recoveryKeyLen   = 32
)

// vaultDeriveKey 使用 PBKDF2 从密码派生密钥（用于密码库）
func vaultDeriveKey(password, salt string) []byte {
	saltBytes, _ := hex.DecodeString(salt)
	return pbkdf2.Key([]byte(password), saltBytes, pbkdf2Iterations, keyLength, sha256.New)
}

// generateRandomBytes 生成随机字节
func generateRandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	return b, err
}

// vaultGenerateSalt 生成盐值（用于密码库）
func vaultGenerateSalt() (string, error) {
	salt, err := generateRandomBytes(saltLength)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(salt), nil
}

// generateDEK 生成数据加密密钥
func generateDEK() ([]byte, error) {
	return generateRandomBytes(dekLength)
}

// generateRecoveryKey 生成恢复密钥
func generateRecoveryKey() (string, error) {
	key, err := generateRandomBytes(recoveryKeyLen)
	if err != nil {
		return "", err
	}
	// 返回 base64 编码的恢复密钥，便于用户保存
	return base64.StdEncoding.EncodeToString(key), nil
}

// vaultHashPassword 计算密码哈希（用于密码库）
func vaultHashPassword(password, salt string) string {
	key := vaultDeriveKey(password, salt)
	return hex.EncodeToString(key)
}

// encryptAESGCM 使用 AES-GCM 加密
func encryptAESGCM(plaintext, key []byte) (string, error) {
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

	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decryptAESGCM 使用 AES-GCM 解密
func decryptAESGCM(ciphertext string, key []byte) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, cipherBytes := data[:nonceSize], data[nonceSize:]
	return gcm.Open(nil, nonce, cipherBytes, nil)
}

// HandleVaultStatus 检查密码库状态
func HandleVaultStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := getUserIDFromRequest(r)
	if userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var count int
	err := database.DB.QueryRow("SELECT COUNT(*) FROM vault_master_keys WHERE user_id = ?", userID).Scan(&count)
	if err != nil {
		log.Printf("查询密码库状态失败: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"initialized": count > 0,
	})
}

// HandleVaultInit 初始化密码库（设置主密码）
func HandleVaultInit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := getUserIDFromRequest(r)
	if userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		MasterPassword string `json:"master_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if len(req.MasterPassword) < 8 {
		http.Error(w, "主密码至少需要8个字符", http.StatusBadRequest)
		return
	}

	// 检查是否已初始化
	var count int
	database.DB.QueryRow("SELECT COUNT(*) FROM vault_master_keys WHERE user_id = ?", userID).Scan(&count)
	if count > 0 {
		http.Error(w, "密码库已初始化", http.StatusBadRequest)
		return
	}

	// 生成盐值
	salt, err := vaultGenerateSalt()
	if err != nil {
		log.Printf("生成盐值失败: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// 生成 DEK
	dek, err := generateDEK()
	if err != nil {
		log.Printf("生成DEK失败: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// 生成恢复密钥
	recoveryKey, err := generateRecoveryKey()
	if err != nil {
		log.Printf("生成恢复密钥失败: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// 计算主密码哈希
	masterPasswordHash := vaultHashPassword(req.MasterPassword, salt)

	// 用主密码派生密钥加密 DEK
	masterKey := vaultDeriveKey(req.MasterPassword, salt)
	encryptedDEK, err := encryptAESGCM(dek, masterKey)
	if err != nil {
		log.Printf("加密DEK失败: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// 用恢复密钥加密 DEK
	recoveryKeyBytes, _ := base64.StdEncoding.DecodeString(recoveryKey)
	// 使用恢复密钥的 SHA256 作为加密密钥
	recoveryKeyHash := sha256.Sum256(recoveryKeyBytes)
	encryptedDEKRecovery, err := encryptAESGCM(dek, recoveryKeyHash[:])
	if err != nil {
		log.Printf("用恢复密钥加密DEK失败: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// 计算恢复密钥哈希（用于验证）
	recoveryKeyHashStr := hex.EncodeToString(recoveryKeyHash[:])

	// 存储到数据库
	_, err = database.DB.Exec(`
		INSERT INTO vault_master_keys (user_id, master_password_hash, encrypted_dek, encrypted_dek_recovery, recovery_key_hash, salt)
		VALUES (?, ?, ?, ?, ?, ?)
	`, userID, masterPasswordHash, encryptedDEK, encryptedDEKRecovery, recoveryKeyHashStr, salt)
	if err != nil {
		log.Printf("存储主密钥失败: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// 记录审计日志
	AddAuditLogFromRequest(r, "vault_init", "vault:"+userID, userID, "", "", `{"action":"初始化密码库"}`)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":      true,
		"recovery_key": recoveryKey,
		"message":      "密码库初始化成功，请妥善保存恢复密钥！",
	})
}

// HandleVaultUnlock 解锁密码库
func HandleVaultUnlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := getUserIDFromRequest(r)
	if userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		MasterPassword string `json:"master_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// 获取存储的主密钥信息
	var masterPasswordHash, encryptedDEK, salt string
	err := database.DB.QueryRow(`
		SELECT master_password_hash, encrypted_dek, salt 
		FROM vault_master_keys WHERE user_id = ?
	`, userID).Scan(&masterPasswordHash, &encryptedDEK, &salt)
	if err == sql.ErrNoRows {
		http.Error(w, "密码库未初始化", http.StatusBadRequest)
		return
	}
	if err != nil {
		log.Printf("查询主密钥失败: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// 验证主密码
	inputHash := vaultHashPassword(req.MasterPassword, salt)
	if inputHash != masterPasswordHash {
		AddAuditLogFromRequest(r, "vault_unlock_failed", "vault:"+userID, userID, "", "", `{"reason":"密码错误"}`)
		http.Error(w, "主密码错误", http.StatusUnauthorized)
		return
	}

	// 解密 DEK
	masterKey := vaultDeriveKey(req.MasterPassword, salt)
	dek, err := decryptAESGCM(encryptedDEK, masterKey)
	if err != nil {
		log.Printf("解密DEK失败: %v", err)
		http.Error(w, "解密失败", http.StatusInternalServerError)
		return
	}

	// 生成会话令牌（包含 DEK，有效期 30 分钟）
	sessionToken := generateVaultSession(userID, dek)

	AddAuditLogFromRequest(r, "vault_unlock", "vault:"+userID, userID, "", "", `{"action":"解锁密码库"}`)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":       true,
		"session_token": sessionToken,
		"expires_in":    1800, // 30 分钟
	})
}

// 密码库会话存储（简化实现，生产环境应使用 Redis）
var vaultSessions = make(map[string]vaultSession)

type vaultSession struct {
	UserID    string
	DEK       []byte
	ExpiresAt time.Time
}

func generateVaultSession(userID string, dek []byte) string {
	token := uuid.New().String()
	vaultSessions[token] = vaultSession{
		UserID:    userID,
		DEK:       dek,
		ExpiresAt: time.Now().Add(8 * time.Hour), // 延长到8小时
	}
	return token
}

func getVaultSession(token string) (*vaultSession, bool) {
	session, ok := vaultSessions[token]
	if !ok {
		return nil, false
	}
	if time.Now().After(session.ExpiresAt) {
		delete(vaultSessions, token)
		return nil, false
	}
	return &session, true
}

// HandleVaultResetPassword 重置主密码
func HandleVaultResetPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := getUserIDFromRequest(r)
	if userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		RecoveryKey       string `json:"recovery_key"`
		NewMasterPassword string `json:"new_master_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if len(req.NewMasterPassword) < 8 {
		http.Error(w, "新主密码至少需要8个字符", http.StatusBadRequest)
		return
	}

	// 获取存储的信息
	var encryptedDEKRecovery, recoveryKeyHashStored, salt string
	err := database.DB.QueryRow(`
		SELECT encrypted_dek_recovery, recovery_key_hash, salt 
		FROM vault_master_keys WHERE user_id = ?
	`, userID).Scan(&encryptedDEKRecovery, &recoveryKeyHashStored, &salt)
	if err == sql.ErrNoRows {
		http.Error(w, "密码库未初始化", http.StatusBadRequest)
		return
	}
	if err != nil {
		log.Printf("查询主密钥失败: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// 验证恢复密钥
	recoveryKeyBytes, err := base64.StdEncoding.DecodeString(req.RecoveryKey)
	if err != nil {
		http.Error(w, "恢复密钥格式错误", http.StatusBadRequest)
		return
	}
	recoveryKeyHash := sha256.Sum256(recoveryKeyBytes)
	if hex.EncodeToString(recoveryKeyHash[:]) != recoveryKeyHashStored {
		AddAuditLogFromRequest(r, "vault_reset_failed", "vault:"+userID, userID, "", "", `{"reason":"恢复密钥错误"}`)
		http.Error(w, "恢复密钥错误", http.StatusUnauthorized)
		return
	}

	// 用恢复密钥解密 DEK
	dek, err := decryptAESGCM(encryptedDEKRecovery, recoveryKeyHash[:])
	if err != nil {
		log.Printf("用恢复密钥解密DEK失败: %v", err)
		http.Error(w, "解密失败", http.StatusInternalServerError)
		return
	}

	// 生成新盐值
	newSalt, err := vaultGenerateSalt()
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// 计算新主密码哈希
	newMasterPasswordHash := vaultHashPassword(req.NewMasterPassword, newSalt)

	// 用新主密码派生密钥加密 DEK
	newMasterKey := vaultDeriveKey(req.NewMasterPassword, newSalt)
	newEncryptedDEK, err := encryptAESGCM(dek, newMasterKey)
	if err != nil {
		http.Error(w, "加密失败", http.StatusInternalServerError)
		return
	}

	// 生成新恢复密钥
	newRecoveryKey, err := generateRecoveryKey()
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// 用新恢复密钥加密 DEK
	newRecoveryKeyBytes, _ := base64.StdEncoding.DecodeString(newRecoveryKey)
	newRecoveryKeyHash := sha256.Sum256(newRecoveryKeyBytes)
	newEncryptedDEKRecovery, err := encryptAESGCM(dek, newRecoveryKeyHash[:])
	if err != nil {
		http.Error(w, "加密失败", http.StatusInternalServerError)
		return
	}

	// 更新数据库
	_, err = database.DB.Exec(`
		UPDATE vault_master_keys 
		SET master_password_hash = ?, encrypted_dek = ?, encrypted_dek_recovery = ?, 
		    recovery_key_hash = ?, salt = ?, updated_at = NOW()
		WHERE user_id = ?
	`, newMasterPasswordHash, newEncryptedDEK, newEncryptedDEKRecovery,
		hex.EncodeToString(newRecoveryKeyHash[:]), newSalt, userID)
	if err != nil {
		log.Printf("更新主密钥失败: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	AddAuditLogFromRequest(r, "vault_reset_password", "vault:"+userID, userID, "", "", `{"action":"重置主密码"}`)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":          true,
		"new_recovery_key": newRecoveryKey,
		"message":          "主密码重置成功，请妥善保存新的恢复密钥！",
	})
}

// HandleVaultItems 获取/添加密码条目
func HandleVaultItems(w http.ResponseWriter, r *http.Request) {
	userID := getUserIDFromRequest(r)
	if userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// 获取会话令牌
	sessionToken := r.Header.Get("X-Vault-Session")
	session, ok := getVaultSession(sessionToken)
	if !ok || session.UserID != userID {
		http.Error(w, "密码库未解锁或会话已过期", http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case http.MethodGet:
		getVaultItems(w, r, userID, session.DEK)
	case http.MethodPost:
		addVaultItem(w, r, userID, session.DEK)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func getVaultItems(w http.ResponseWriter, r *http.Request, userID string, dek []byte) {
	rows, err := database.DB.Query(`
		SELECT id, folder_id, name, username, password, url, notes, type, favorite, created_at, updated_at
		FROM vault_items WHERE user_id = ? ORDER BY name
	`, userID)
	if err != nil {
		log.Printf("查询密码条目失败: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var items []VaultItem
	for rows.Next() {
		var item VaultItem
		var encName, encUsername, encPassword, encURL, encNotes string
		var createdAt, updatedAt time.Time
		err := rows.Scan(&item.ID, &item.FolderID, &encName, &encUsername, &encPassword, &encURL, &encNotes, &item.Type, &item.Favorite, &createdAt, &updatedAt)
		if err != nil {
			log.Printf("扫描密码条目失败: %v", err)
			continue
		}

		// 解密字段
		if name, err := decryptAESGCM(encName, dek); err == nil {
			item.Name = string(name)
		}
		if username, err := decryptAESGCM(encUsername, dek); err == nil {
			item.Username = string(username)
		}
		if password, err := decryptAESGCM(encPassword, dek); err == nil {
			item.Password = string(password)
		}
		if url, err := decryptAESGCM(encURL, dek); err == nil {
			item.URL = string(url)
		}
		if notes, err := decryptAESGCM(encNotes, dek); err == nil {
			item.Notes = string(notes)
		}

		item.UserID = userID
		item.CreatedAt = createdAt.Format("2006-01-02 15:04:05")
		item.UpdatedAt = updatedAt.Format("2006-01-02 15:04:05")
		items = append(items, item)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(items)
}

func addVaultItem(w http.ResponseWriter, r *http.Request, userID string, dek []byte) {
	var item VaultItem
	if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	item.ID = uuid.New().String()
	item.UserID = userID

	// 加密字段
	encName, _ := encryptAESGCM([]byte(item.Name), dek)
	encUsername, _ := encryptAESGCM([]byte(item.Username), dek)
	encPassword, _ := encryptAESGCM([]byte(item.Password), dek)
	encURL, _ := encryptAESGCM([]byte(item.URL), dek)
	encNotes, _ := encryptAESGCM([]byte(item.Notes), dek)

	if item.Type == "" {
		item.Type = "login"
	}

	_, err := database.DB.Exec(`
		INSERT INTO vault_items (id, user_id, folder_id, name, username, password, url, notes, type, favorite)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, userID, item.FolderID, encName, encUsername, encPassword, encURL, encNotes, item.Type, item.Favorite)
	if err != nil {
		log.Printf("添加密码条目失败: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	AddAuditLogFromRequest(r, "vault_add_item", "vault_item:"+item.ID, userID, "", "", `{"name":"`+item.Name+`"}`)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"id":      item.ID,
	})
}

// HandleVaultItem 处理单个密码条目（更新/删除）
func HandleVaultItem(w http.ResponseWriter, r *http.Request) {
	userID := getUserIDFromRequest(r)
	if userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// 获取会话令牌
	sessionToken := r.Header.Get("X-Vault-Session")
	session, ok := getVaultSession(sessionToken)
	if !ok || session.UserID != userID {
		http.Error(w, "密码库未解锁或会话已过期", http.StatusUnauthorized)
		return
	}

	// 从 URL 获取 ID
	path := r.URL.Path
	parts := strings.Split(path, "/")
	if len(parts) < 4 {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	itemID := parts[len(parts)-1]

	switch r.Method {
	case http.MethodPut:
		updateVaultItem(w, r, userID, itemID, session.DEK)
	case http.MethodDelete:
		deleteVaultItem(w, r, userID, itemID)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func updateVaultItem(w http.ResponseWriter, r *http.Request, userID, itemID string, dek []byte) {
	var item VaultItem
	if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// 验证条目属于该用户
	var count int
	database.DB.QueryRow("SELECT COUNT(*) FROM vault_items WHERE id = ? AND user_id = ?", itemID, userID).Scan(&count)
	if count == 0 {
		http.Error(w, "条目不存在", http.StatusNotFound)
		return
	}

	// 加密字段
	encName, _ := encryptAESGCM([]byte(item.Name), dek)
	encUsername, _ := encryptAESGCM([]byte(item.Username), dek)
	encPassword, _ := encryptAESGCM([]byte(item.Password), dek)
	encURL, _ := encryptAESGCM([]byte(item.URL), dek)
	encNotes, _ := encryptAESGCM([]byte(item.Notes), dek)

	_, err := database.DB.Exec(`
		UPDATE vault_items 
		SET name = ?, username = ?, password = ?, url = ?, notes = ?, folder_id = ?, type = ?, favorite = ?, updated_at = NOW()
		WHERE id = ? AND user_id = ?
	`, encName, encUsername, encPassword, encURL, encNotes, item.FolderID, item.Type, item.Favorite, itemID, userID)
	if err != nil {
		log.Printf("更新密码条目失败: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	AddAuditLogFromRequest(r, "vault_update_item", "vault_item:"+itemID, userID, "", "", `{"name":"`+item.Name+`"}`)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
	})
}

func deleteVaultItem(w http.ResponseWriter, r *http.Request, userID, itemID string) {
	// 获取条目名称用于日志
	var encName string
	database.DB.QueryRow("SELECT name FROM vault_items WHERE id = ? AND user_id = ?", itemID, userID).Scan(&encName)

	result, err := database.DB.Exec("DELETE FROM vault_items WHERE id = ? AND user_id = ?", itemID, userID)
	if err != nil {
		log.Printf("删除密码条目失败: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		http.Error(w, "条目不存在", http.StatusNotFound)
		return
	}

	AddAuditLogFromRequest(r, "vault_delete_item", "vault_item:"+itemID, userID, "", "", "")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
	})
}

// HandleVaultFolders 处理文件夹
func HandleVaultFolders(w http.ResponseWriter, r *http.Request) {
	userID := getUserIDFromRequest(r)
	if userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case http.MethodGet:
		rows, err := database.DB.Query(`
			SELECT id, name, parent_id, icon, sort_order, created_at
			FROM vault_folders WHERE user_id = ? ORDER BY sort_order, name
		`, userID)
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var folders []VaultFolder
		for rows.Next() {
			var f VaultFolder
			var createdAt time.Time
			rows.Scan(&f.ID, &f.Name, &f.ParentID, &f.Icon, &f.SortOrder, &createdAt)
			f.UserID = userID
			f.CreatedAt = createdAt.Format("2006-01-02 15:04:05")
			folders = append(folders, f)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(folders)

	case http.MethodPost:
		var folder VaultFolder
		if err := json.NewDecoder(r.Body).Decode(&folder); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		folder.ID = uuid.New().String()
		if folder.Icon == "" {
			folder.Icon = "folder"
		}

		_, err := database.DB.Exec(`
			INSERT INTO vault_folders (id, user_id, name, parent_id, icon, sort_order)
			VALUES (?, ?, ?, ?, ?, ?)
		`, folder.ID, userID, folder.Name, folder.ParentID, folder.Icon, folder.SortOrder)
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"id":      folder.ID,
		})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandleVaultFolder 处理单个文件夹的更新和删除
func HandleVaultFolder(w http.ResponseWriter, r *http.Request) {
	userID := getUserIDFromRequest(r)
	if userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// 从 URL 获取文件夹 ID
	path := r.URL.Path
	parts := strings.Split(path, "/")
	if len(parts) < 4 {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	folderID := parts[len(parts)-1]

	switch r.Method {
	case http.MethodPut:
		var folder VaultFolder
		if err := json.NewDecoder(r.Body).Decode(&folder); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		_, err := database.DB.Exec(`
			UPDATE vault_folders SET name = ?, icon = ?, sort_order = ?
			WHERE id = ? AND user_id = ?
		`, folder.Name, folder.Icon, folder.SortOrder, folderID, userID)
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}

		AddAuditLogFromRequest(r, "vault_update_folder", "vault_folder:"+folderID, userID, "", "", `{"name":"`+folder.Name+`"}`)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true})

	case http.MethodDelete:
		// 删除文件夹（不删除文件夹中的密码，只是将它们的 folder_id 设为空）
		database.DB.Exec("UPDATE vault_items SET folder_id = '' WHERE folder_id = ? AND user_id = ?", folderID, userID)

		result, err := database.DB.Exec("DELETE FROM vault_folders WHERE id = ? AND user_id = ?", folderID, userID)
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}

		affected, _ := result.RowsAffected()
		if affected == 0 {
			http.Error(w, "文件夹不存在", http.StatusNotFound)
			return
		}

		AddAuditLogFromRequest(r, "vault_delete_folder", "vault_folder:"+folderID, userID, "", "", "")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandleVaultLock 锁定密码库
func HandleVaultLock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionToken := r.Header.Get("X-Vault-Session")
	if sessionToken != "" {
		delete(vaultSessions, sessionToken)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
	})
}

// HandleVaultGeneratePassword 生成随机密码
func HandleVaultGeneratePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	length := 16
	if l := r.URL.Query().Get("length"); l != "" {
		fmt.Sscanf(l, "%d", &length)
		if length < 8 {
			length = 8
		}
		if length > 128 {
			length = 128
		}
	}

	includeUpper := r.URL.Query().Get("upper") != "false"
	includeLower := r.URL.Query().Get("lower") != "false"
	includeNumbers := r.URL.Query().Get("numbers") != "false"
	includeSymbols := r.URL.Query().Get("symbols") != "false"

	charset := ""
	if includeUpper {
		charset += "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	}
	if includeLower {
		charset += "abcdefghijklmnopqrstuvwxyz"
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
		randomIndex, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		password[i] = charset[randomIndex.Int64()]
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"password": string(password),
	})
}

// 从请求中获取用户 ID（与现有认证系统一致）
func getUserIDFromRequest(r *http.Request) string {
	// 从 X-Operator header 获取用户名
	operator := r.Header.Get("X-Operator")
	if operator != "" {
		return operator
	}
	// 如果没有 header，返回默认值
	return "admin"
}

// ========== 用户组管理 ==========

// VaultGroup 用户组结构
type VaultGroup struct {
	ID          string `json:"id"`
	OwnerID     string `json:"owner_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MemberCount int    `json:"member_count"`
	CreatedAt   string `json:"created_at"`
}

// VaultGroupMember 组成员结构
type VaultGroupMember struct {
	ID       string `json:"id"`
	GroupID  string `json:"group_id"`
	UserID   string `json:"user_id"`
	Role     string `json:"role"`
	AddedAt  string `json:"added_at"`
	AddedBy  string `json:"added_by"`
}

// VaultShare 分享结构
type VaultShare struct {
	ID          string `json:"id"`
	OwnerID     string `json:"owner_id"`
	TargetType  string `json:"target_type"`
	TargetID    string `json:"target_id"`
	SharedWith  string `json:"shared_with"`
	Permission  string `json:"permission"`
	CreatedAt   string `json:"created_at"`
	ExpiresAt   string `json:"expires_at,omitempty"`
	TargetName  string `json:"target_name,omitempty"`
}

// HandleVaultGroups 处理用户组
func HandleVaultGroups(w http.ResponseWriter, r *http.Request) {
	userID := getUserIDFromRequest(r)
	if userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case http.MethodGet:
		// 获取用户创建的和加入的所有组
		rows, err := database.DB.Query(`
			SELECT g.id, g.owner_id, g.name, g.description, g.created_at,
				   (SELECT COUNT(*) FROM vault_group_members WHERE group_id = g.id) as member_count
			FROM vault_groups g
			WHERE g.owner_id = ? OR g.id IN (SELECT group_id FROM vault_group_members WHERE user_id = ?)
			ORDER BY g.name
		`, userID, userID)
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var groups []VaultGroup
		for rows.Next() {
			var g VaultGroup
			var createdAt time.Time
			if err := rows.Scan(&g.ID, &g.OwnerID, &g.Name, &g.Description, &createdAt, &g.MemberCount); err != nil {
				continue
			}
			g.CreatedAt = createdAt.Format("2006-01-02 15:04:05")
			groups = append(groups, g)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(groups)

	case http.MethodPost:
		var g VaultGroup
		if err := json.NewDecoder(r.Body).Decode(&g); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		g.ID = uuid.New().String()
		g.OwnerID = userID

		_, err := database.DB.Exec(`
			INSERT INTO vault_groups (id, owner_id, name, description) VALUES (?, ?, ?, ?)
		`, g.ID, userID, g.Name, g.Description)
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}

		AddAuditLogFromRequest(r, "vault_create_group", "vault_group:"+g.ID, userID, "", "", `{"name":"`+g.Name+`"}`)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"id":      g.ID,
		})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandleVaultGroup 处理单个用户组
func HandleVaultGroup(w http.ResponseWriter, r *http.Request) {
	userID := getUserIDFromRequest(r)
	if userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	path := r.URL.Path
	parts := strings.Split(path, "/")
	if len(parts) < 4 {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	groupID := parts[len(parts)-1]

	switch r.Method {
	case http.MethodPut:
		// 验证权限
		var ownerID string
		err := database.DB.QueryRow("SELECT owner_id FROM vault_groups WHERE id = ?", groupID).Scan(&ownerID)
		if err != nil {
			http.Error(w, "组不存在", http.StatusNotFound)
			return
		}
		if ownerID != userID {
			http.Error(w, "无权限修改此组", http.StatusForbidden)
			return
		}

		var g VaultGroup
		if err := json.NewDecoder(r.Body).Decode(&g); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		_, err = database.DB.Exec(`UPDATE vault_groups SET name = ?, description = ? WHERE id = ?`,
			g.Name, g.Description, groupID)
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true})

	case http.MethodDelete:
		var ownerID string
		err := database.DB.QueryRow("SELECT owner_id FROM vault_groups WHERE id = ?", groupID).Scan(&ownerID)
		if err != nil {
			http.Error(w, "组不存在", http.StatusNotFound)
			return
		}
		if ownerID != userID {
			http.Error(w, "无权限删除此组", http.StatusForbidden)
			return
		}

		// 删除组成员
		database.DB.Exec("DELETE FROM vault_group_members WHERE group_id = ?", groupID)
		// 删除组
		database.DB.Exec("DELETE FROM vault_groups WHERE id = ?", groupID)

		AddAuditLogFromRequest(r, "vault_delete_group", "vault_group:"+groupID, userID, "", "", "")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandleVaultGroupMembers 处理组成员
func HandleVaultGroupMembers(w http.ResponseWriter, r *http.Request) {
	userID := getUserIDFromRequest(r)
	if userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	path := r.URL.Path
	parts := strings.Split(path, "/")
	// /api/vault/groups/{id}/members
	if len(parts) < 5 {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	groupID := parts[len(parts)-2]

	switch r.Method {
	case http.MethodGet:
		rows, err := database.DB.Query(`
			SELECT id, group_id, user_id, role, added_at, added_by
			FROM vault_group_members WHERE group_id = ?
		`, groupID)
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var members []VaultGroupMember
		for rows.Next() {
			var m VaultGroupMember
			var addedAt time.Time
			if err := rows.Scan(&m.ID, &m.GroupID, &m.UserID, &m.Role, &addedAt, &m.AddedBy); err != nil {
				continue
			}
			m.AddedAt = addedAt.Format("2006-01-02 15:04:05")
			members = append(members, m)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(members)

	case http.MethodPost:
		// 验证是否有权限添加成员
		var ownerID string
		err := database.DB.QueryRow("SELECT owner_id FROM vault_groups WHERE id = ?", groupID).Scan(&ownerID)
		if err != nil {
			http.Error(w, "组不存在", http.StatusNotFound)
			return
		}

		// 检查是否是组拥有者或管理员
		isAdmin := ownerID == userID
		if !isAdmin {
			var role string
			database.DB.QueryRow("SELECT role FROM vault_group_members WHERE group_id = ? AND user_id = ?", groupID, userID).Scan(&role)
			isAdmin = role == "admin"
		}
		if !isAdmin {
			http.Error(w, "无权限添加成员", http.StatusForbidden)
			return
		}

		var m VaultGroupMember
		if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		m.ID = uuid.New().String()
		m.GroupID = groupID
		m.AddedBy = userID

		_, err = database.DB.Exec(`
			INSERT INTO vault_group_members (id, group_id, user_id, role, added_by) VALUES (?, ?, ?, ?, ?)
		`, m.ID, groupID, m.UserID, m.Role, userID)
		if err != nil {
			http.Error(w, "添加失败，用户可能已在组中", http.StatusBadRequest)
			return
		}

		AddAuditLogFromRequest(r, "vault_add_member", "vault_group:"+groupID, userID, "", "", `{"member":"`+m.UserID+`"}`)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "id": m.ID})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandleVaultGroupMember 处理单个组成员
func HandleVaultGroupMember(w http.ResponseWriter, r *http.Request) {
	userID := getUserIDFromRequest(r)
	if userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	path := r.URL.Path
	parts := strings.Split(path, "/")
	// /api/vault/groups/{id}/members/{memberId}
	if len(parts) < 6 {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	groupID := parts[len(parts)-3]
	memberID := parts[len(parts)-1]

	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 验证权限
	var ownerID string
	err := database.DB.QueryRow("SELECT owner_id FROM vault_groups WHERE id = ?", groupID).Scan(&ownerID)
	if err != nil {
		http.Error(w, "组不存在", http.StatusNotFound)
		return
	}

	isAdmin := ownerID == userID
	if !isAdmin {
		var role string
		database.DB.QueryRow("SELECT role FROM vault_group_members WHERE group_id = ? AND user_id = ?", groupID, userID).Scan(&role)
		isAdmin = role == "admin"
	}
	if !isAdmin {
		http.Error(w, "无权限删除成员", http.StatusForbidden)
		return
	}

	database.DB.Exec("DELETE FROM vault_group_members WHERE id = ?", memberID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// ========== 分享管理 ==========

// HandleVaultShares 处理分享列表
func HandleVaultShares(w http.ResponseWriter, r *http.Request) {
	userID := getUserIDFromRequest(r)
	if userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case http.MethodGet:
		// 获取用户的分享（自己分享的和被分享的）
		rows, err := database.DB.Query(`
			SELECT s.id, s.owner_id, s.target_type, s.target_id, s.shared_with, s.permission, s.created_at, s.expires_at,
				   CASE 
					   WHEN s.target_type = 'folder' THEN (SELECT name FROM vault_folders WHERE id = s.target_id)
					   WHEN s.target_type = 'item' THEN (SELECT name FROM vault_items WHERE id = s.target_id)
				   END as target_name
			FROM vault_shares s
			WHERE s.owner_id = ? OR s.shared_with = ?
			ORDER BY s.created_at DESC
		`, userID, userID)
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var shares []VaultShare
		for rows.Next() {
			var s VaultShare
			var createdAt time.Time
			var expiresAt sql.NullTime
			var targetName sql.NullString
			if err := rows.Scan(&s.ID, &s.OwnerID, &s.TargetType, &s.TargetID, &s.SharedWith,
				&s.Permission, &createdAt, &expiresAt, &targetName); err != nil {
				continue
			}
			s.CreatedAt = createdAt.Format("2006-01-02 15:04:05")
			if expiresAt.Valid {
				s.ExpiresAt = expiresAt.Time.Format("2006-01-02 15:04:05")
			}
			if targetName.Valid {
				s.TargetName = targetName.String
			}
			shares = append(shares, s)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(shares)

	case http.MethodPost:
		var s VaultShare
		if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		// 验证用户是否拥有这个条目/文件夹
		var itemOwner string
		var query string
		if s.TargetType == "folder" {
			query = "SELECT user_id FROM vault_folders WHERE id = ?"
		} else {
			query = "SELECT user_id FROM vault_items WHERE id = ?"
		}
		err := database.DB.QueryRow(query, s.TargetID).Scan(&itemOwner)
		if err != nil {
			http.Error(w, "目标不存在", http.StatusNotFound)
			return
		}
		if itemOwner != userID {
			http.Error(w, "无权限分享此内容", http.StatusForbidden)
			return
		}

		s.ID = uuid.New().String()
		s.OwnerID = userID

		_, err = database.DB.Exec(`
			INSERT INTO vault_shares (id, owner_id, target_type, target_id, shared_with, permission, expires_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE permission = VALUES(permission), expires_at = VALUES(expires_at)
		`, s.ID, userID, s.TargetType, s.TargetID, s.SharedWith, s.Permission, nil)
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}

		AddAuditLogFromRequest(r, "vault_share", "vault_share:"+s.ID, userID, "", "",
			`{"type":"`+s.TargetType+`","shared_with":"`+s.SharedWith+`"}`)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "id": s.ID})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandleVaultShare 处理单个分享
func HandleVaultShare(w http.ResponseWriter, r *http.Request) {
	userID := getUserIDFromRequest(r)
	if userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	path := r.URL.Path
	parts := strings.Split(path, "/")
	if len(parts) < 4 {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	shareID := parts[len(parts)-1]

	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 只有分享者可以取消分享
	result, err := database.DB.Exec("DELETE FROM vault_shares WHERE id = ? AND owner_id = ?", shareID, userID)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		http.Error(w, "分享不存在或无权限", http.StatusNotFound)
		return
	}

	AddAuditLogFromRequest(r, "vault_unshare", "vault_share:"+shareID, userID, "", "", "")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// HandleVaultUsers 获取可分享的用户列表
func HandleVaultUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 获取所有活跃用户
	rows, err := database.DB.Query(`
		SELECT username, display_name FROM users WHERE status = 'active' ORDER BY username
	`)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type User struct {
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
	}

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.Username, &u.DisplayName); err != nil {
			continue
		}
		users = append(users, u)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(users)
}
