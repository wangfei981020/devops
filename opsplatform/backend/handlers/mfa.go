package handlers

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/png"
	"net/http"

	"opsplatform/database"

	"github.com/pquerna/otp/totp"
)

// MFASetupResponse MFA 设置响应
type MFASetupResponse struct {
	Secret    string `json:"secret"`
	QRCode    string `json:"qr_code"` // Base64 编码的二维码图片
	Issuer    string `json:"issuer"`
	AccountName string `json:"account_name"`
}

// HandleMFASetup 生成 MFA 密钥和二维码（用于绑定）
func HandleMFASetup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID string `json:"user_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "无效的请求数据", http.StatusBadRequest)
		return
	}

	user, err := GetUserByID(req.UserID)
	if err != nil || user == nil {
		http.Error(w, "用户不存在", http.StatusNotFound)
		return
	}

	// 生成 TOTP 密钥
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "OpsPlat",
		AccountName: user.Username,
	})
	if err != nil {
		http.Error(w, "生成 MFA 密钥失败", http.StatusInternalServerError)
		return
	}

	// 生成二维码图片
	var buf bytes.Buffer
	img, err := key.Image(200, 200)
	if err != nil {
		http.Error(w, "生成二维码失败", http.StatusInternalServerError)
		return
	}
	png.Encode(&buf, img)
	qrCodeBase64 := base64.StdEncoding.EncodeToString(buf.Bytes())

	// 临时保存密钥到数据库（未启用状态）
	_, err = database.DB.Exec(`UPDATE users SET mfa_secret = ? WHERE id = ?`, key.Secret(), req.UserID)
	if err != nil {
		http.Error(w, "保存 MFA 密钥失败", http.StatusInternalServerError)
		return
	}

	response := MFASetupResponse{
		Secret:      key.Secret(),
		QRCode:      "data:image/png;base64," + qrCodeBase64,
		Issuer:      "OpsPlat",
		AccountName: user.Username,
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(response)
}

// HandleMFABind 验证并绑定 MFA（启用 MFA）
func HandleMFABind(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID string `json:"user_id"`
		Code   string `json:"code"` // 用户输入的 6 位验证码
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "无效的请求数据", http.StatusBadRequest)
		return
	}

	// 获取用户的 MFA 密钥
	var mfaSecret string
	err := database.DB.QueryRow(`SELECT COALESCE(mfa_secret, '') FROM users WHERE id = ?`, req.UserID).Scan(&mfaSecret)
	if err != nil || mfaSecret == "" {
		http.Error(w, "未找到 MFA 密钥，请先设置 MFA", http.StatusBadRequest)
		return
	}

	// 验证 TOTP 代码
	valid := totp.Validate(req.Code, mfaSecret)
	if !valid {
		http.Error(w, "验证码错误，请重试", http.StatusUnauthorized)
		return
	}

	// 验证成功，启用 MFA
	_, err = database.DB.Exec(`UPDATE users SET mfa_enabled = 1 WHERE id = ?`, req.UserID)
	if err != nil {
		http.Error(w, "启用 MFA 失败", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "MFA 绑定成功",
	})
}

// HandleMFAVerify 验证 MFA 代码（用于登录）
func HandleMFAVerify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID string `json:"user_id"`
		Code   string `json:"code"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "无效的请求数据", http.StatusBadRequest)
		return
	}

	// 获取用户信息
	var mfaSecret string
	var mfaEnabled bool
	err := database.DB.QueryRow(`SELECT COALESCE(mfa_secret, ''), mfa_enabled FROM users WHERE id = ?`, req.UserID).Scan(&mfaSecret, &mfaEnabled)
	if err != nil {
		http.Error(w, "用户不存在", http.StatusNotFound)
		return
	}

	if !mfaEnabled {
		http.Error(w, "用户未启用 MFA", http.StatusBadRequest)
		return
	}

	// 验证 TOTP 代码
	valid := totp.Validate(req.Code, mfaSecret)
	if !valid {
		http.Error(w, "验证码错误", http.StatusUnauthorized)
		return
	}

	// 获取完整用户信息返回
	user, _ := GetUserByID(req.UserID)
	
	// 生成 JWT token
	token, err := GenerateToken(user.ID, user.Username, user.Role)
	if err != nil {
		http.Error(w, "生成认证令牌失败", http.StatusInternalServerError)
		return
	}

	// 记录 MFA 登录成功日志
	ip := GetClientIP(r)
	AddAuditLog("login", "user:"+user.ID, user.Username, "", "", fmt.Sprintf("用户 MFA 验证登录成功: %s (%s)", user.Username, user.DisplayName), ip)

	user.MFABound = user.MFASecret != ""
	user.Password = ""
	user.MFASecret = ""

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"user":  user,
		"token": token,
	})
}

// HandleMFADisable 禁用 MFA
func HandleMFADisable(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID   string `json:"user_id"`
		Password string `json:"password"` // 需要验证密码才能禁用
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "无效的请求数据", http.StatusBadRequest)
		return
	}

	user, err := GetUserByID(req.UserID)
	if err != nil || user == nil {
		http.Error(w, "用户不存在", http.StatusNotFound)
		return
	}

	// 验证密码
	if !checkPassword(user.Password, req.Password) && user.Password != req.Password {
		http.Error(w, "密码错误", http.StatusUnauthorized)
		return
	}

	// 禁用 MFA
	_, err = database.DB.Exec(`UPDATE users SET mfa_enabled = 0, mfa_secret = NULL WHERE id = ?`, req.UserID)
	if err != nil {
		http.Error(w, "禁用 MFA 失败", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "MFA 已禁用",
	})
}

// HandleMFAReset 管理员重置用户 MFA（清除密钥，保持启用状态让用户重新绑定）
func HandleMFAReset(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID string `json:"user_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "无效的请求数据", http.StatusBadRequest)
		return
	}

	// 重置 MFA（只清除密钥，保持 mfa_enabled 状态）
	_, err := database.DB.Exec(`UPDATE users SET mfa_secret = NULL WHERE id = ?`, req.UserID)
	if err != nil {
		http.Error(w, "重置 MFA 失败", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "MFA 已重置，用户需重新绑定",
	})
}

