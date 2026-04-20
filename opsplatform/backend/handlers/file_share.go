package handlers

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"opsplatform/database"
	"opsplatform/storage"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

type FileShare struct {
	ID        string  `json:"id"`
	Code      string  `json:"code"`
	FilePath  string  `json:"file_path"`
	FileName  string  `json:"file_name"`
	ExpiresAt *string `json:"expires_at"`
	ViewCount int     `json:"view_count"`
	MaxViews  int     `json:"max_views"`
	Password  string  `json:"password,omitempty"`
	CreatedBy string  `json:"created_by"`
	CreatedAt string  `json:"created_at"`
}

type CreateShareRequest struct {
	FilePath  string `json:"file_path"`
	FileName  string `json:"file_name"`
	ExpiresIn string `json:"expires_in"` // 1d, 7d, 30d, permanent
	Password  string `json:"password"`
	MaxViews  int    `json:"max_views"`
}

type CreateShareResponse struct {
	Code      string  `json:"code"`
	ShareURL  string  `json:"share_url"`
	ExpiresAt *string `json:"expires_at"`
}

type PresignRequest struct {
	Paths []string `json:"paths"`
}

type PresignResponse struct {
	URLs map[string]string `json:"urls"`
}

func generateShareCode() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func HandleGetPresignedURL(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		sendError(w, "path参数必填", http.StatusBadRequest)
		return
	}

	objectName := storage.GetStorage().GetObjectName(path)
	
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	presignedURL, err := storage.GetStorage().GetPresignedURL(ctx, objectName, time.Hour)
	if err != nil {
		log.Printf("[Storage] 生成预签名URL失败: %v", err)
		sendError(w, "生成预签名URL失败", http.StatusInternalServerError)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"url": presignedURL,
	})
}

func HandleBatchPresignedURL(w http.ResponseWriter, r *http.Request) {
	var req PresignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, "请求参数无效", http.StatusBadRequest)
		return
	}

	if len(req.Paths) == 0 {
		sendError(w, "paths不能为空", http.StatusBadRequest)
		return
	}

	if len(req.Paths) > 200 {
		sendError(w, "一次最多处理200个文件", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	urls := make(map[string]string)
	for _, path := range req.Paths {
		objectName := storage.GetStorage().GetObjectName(path)
		presignedURL, err := storage.GetStorage().GetPresignedURL(ctx, objectName, time.Hour)
		if err != nil {
			log.Printf("[Storage] 生成预签名URL失败 %s: %v", path, err)
			urls[path] = ""
			continue
		}
		urls[path] = presignedURL
	}

	respondJSON(w, http.StatusOK, PresignResponse{URLs: urls})
}

func HandleCreateFileShare(w http.ResponseWriter, r *http.Request) {
	userID, _, _ := GetUserFromContext(r)
	if userID == "" {
		sendError(w, "未登录", http.StatusUnauthorized)
		return
	}

	var req CreateShareRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, "请求参数无效", http.StatusBadRequest)
		return
	}

	if req.FilePath == "" {
		sendError(w, "file_path必填", http.StatusBadRequest)
		return
	}

	objectName := storage.GetStorage().GetObjectName(req.FilePath)

	var expiresAt *time.Time
	switch req.ExpiresIn {
	case "1d":
		t := time.Now().Add(24 * time.Hour)
		expiresAt = &t
	case "7d":
		t := time.Now().Add(7 * 24 * time.Hour)
		expiresAt = &t
	case "30d":
		t := time.Now().Add(30 * 24 * time.Hour)
		expiresAt = &t
	case "permanent", "":
		expiresAt = nil
	default:
		sendError(w, "expires_in 只支持: 1d, 7d, 30d, permanent", http.StatusBadRequest)
		return
	}

	code := generateShareCode()
	id := uuid.New().String()

	var expiresAtStr sql.NullString
	if expiresAt != nil {
		expiresAtStr = sql.NullString{String: expiresAt.Format("2006-01-02 15:04:05"), Valid: true}
	}

	_, err := database.DB.Exec(`
		INSERT INTO file_shares (id, code, file_path, file_name, expires_at, max_views, password, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, id, code, objectName, req.FileName, expiresAtStr, req.MaxViews, req.Password, userID)

	if err != nil {
		log.Printf("[FileShare] 创建分享失败: %v", err)
		sendError(w, "创建分享失败", http.StatusInternalServerError)
		return
	}

	var expiresAtResp *string
	if expiresAt != nil {
		s := expiresAt.Format("2006-01-02 15:04:05")
		expiresAtResp = &s
	}

	respondJSON(w, http.StatusOK, CreateShareResponse{
		Code:      code,
		ShareURL:  "/share/" + code,
		ExpiresAt: expiresAtResp,
	})
}

func HandleAccessFileShare(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	code := vars["code"]

	if code == "" {
		sendError(w, "分享码无效", http.StatusBadRequest)
		return
	}

	var share FileShare
	var expiresAt sql.NullString
	err := database.DB.QueryRow(`
		SELECT id, code, file_path, file_name, expires_at, view_count, max_views, password, created_by, created_at
		FROM file_shares WHERE code = ?
	`, code).Scan(&share.ID, &share.Code, &share.FilePath, &share.FileName, &expiresAt, &share.ViewCount, &share.MaxViews, &share.Password, &share.CreatedBy, &share.CreatedAt)

	if err == sql.ErrNoRows {
		sendError(w, "分享链接不存在或已失效", http.StatusNotFound)
		return
	}
	if err != nil {
		log.Printf("[FileShare] 查询分享失败: %v", err)
		sendError(w, "查询失败", http.StatusInternalServerError)
		return
	}

	if expiresAt.Valid {
		share.ExpiresAt = &expiresAt.String
		expTime, _ := time.Parse("2006-01-02 15:04:05", expiresAt.String)
		if time.Now().After(expTime) {
			sendError(w, "分享链接已过期", http.StatusGone)
			return
		}
	}

	if share.MaxViews > 0 && share.ViewCount >= share.MaxViews {
		sendError(w, "分享链接已达最大访问次数", http.StatusGone)
		return
	}

	if share.Password != "" {
		inputPwd := r.URL.Query().Get("password")
		if inputPwd == "" {
			respondJSON(w, http.StatusOK, map[string]interface{}{
				"requires_password": true,
				"file_name":         share.FileName,
			})
			return
		}
		if inputPwd != share.Password {
			sendError(w, "密码错误", http.StatusForbidden)
			return
		}
	}

	database.DB.Exec(`UPDATE file_shares SET view_count = view_count + 1 WHERE id = ?`, share.ID)

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	presignedURL, err := storage.GetStorage().GetPresignedURL(ctx, share.FilePath, 24*time.Hour)
	if err != nil {
		log.Printf("[FileShare] 生成预签名URL失败: %v", err)
		sendError(w, "获取文件失败", http.StatusInternalServerError)
		return
	}

	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "text/html") {
		http.Redirect(w, r, presignedURL, http.StatusFound)
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"url":        presignedURL,
		"file_name":  share.FileName,
		"expires_at": share.ExpiresAt,
		"view_count": share.ViewCount + 1,
	})
}

func HandleListFileShares(w http.ResponseWriter, r *http.Request) {
	userID, _, _ := GetUserFromContext(r)
	if userID == "" {
		sendError(w, "未登录", http.StatusUnauthorized)
		return
	}

	rows, err := database.DB.Query(`
		SELECT id, code, file_path, file_name, expires_at, view_count, max_views, created_by, created_at
		FROM file_shares
		WHERE created_by = ?
		ORDER BY created_at DESC
		LIMIT 100
	`, userID)
	if err != nil {
		log.Printf("[FileShare] 查询分享列表失败: %v", err)
		sendError(w, "查询失败", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	shares := make([]FileShare, 0)
	for rows.Next() {
		var share FileShare
		var expiresAt sql.NullString
		err := rows.Scan(&share.ID, &share.Code, &share.FilePath, &share.FileName, &expiresAt, &share.ViewCount, &share.MaxViews, &share.CreatedBy, &share.CreatedAt)
		if err != nil {
			continue
		}
		if expiresAt.Valid {
			share.ExpiresAt = &expiresAt.String
		}
		shares = append(shares, share)
	}

	respondJSON(w, http.StatusOK, shares)
}

func HandleDeleteFileShare(w http.ResponseWriter, r *http.Request) {
	userID, _, _ := GetUserFromContext(r)
	if userID == "" {
		sendError(w, "未登录", http.StatusUnauthorized)
		return
	}

	vars := mux.Vars(r)
	id := vars["id"]

	result, err := database.DB.Exec(`DELETE FROM file_shares WHERE id = ? AND created_by = ?`, id, userID)
	if err != nil {
		log.Printf("[FileShare] 删除分享失败: %v", err)
		sendError(w, "删除失败", http.StatusInternalServerError)
		return
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		sendError(w, "分享不存在或无权删除", http.StatusNotFound)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "删除成功"})
}

// HandleStorageUpload 通用文件上传接口
func HandleStorageUpload(w http.ResponseWriter, r *http.Request) {
	userID, _, _ := GetUserFromContext(r)
	if userID == "" {
		sendError(w, "未登录", http.StatusUnauthorized)
		return
	}

	err := r.ParseMultipartForm(32 << 20) // 32MB
	if err != nil {
		sendError(w, "解析表单失败", http.StatusBadRequest)
		return
	}

	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		sendError(w, "未选择文件", http.StatusBadRequest)
		return
	}
	defer file.Close()

	allowedExts := map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true, ".bmp": true}
	contentTypes := map[string]string{
		".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png",
		".gif": "image/gif", ".webp": "image/webp", ".bmp": "image/bmp",
	}

	ext := strings.ToLower(filepath.Ext(fileHeader.Filename))
	if !allowedExts[ext] {
		sendError(w, fmt.Sprintf("不支持的文件类型: %s，仅支持图片格式", ext), http.StatusBadRequest)
		return
	}

	if fileHeader.Size == 0 {
		sendError(w, "文件为空，无法上传", http.StatusBadRequest)
		return
	}

	if fileHeader.Size > 10*1024*1024 {
		sendError(w, "文件超过10MB限制", http.StatusBadRequest)
		return
	}

	objectName := fmt.Sprintf("uploads/%s_%s%s", time.Now().Format("20060102150405"), uuid.New().String()[:8], ext)
	contentType := contentTypes[ext]
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	store := storage.GetStorage()
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	fileURL, err := store.Upload(ctx, objectName, file, fileHeader.Size, contentType)
	if err != nil {
		log.Printf("[Storage] 上传文件失败: %v", err)
		sendError(w, "上传失败", http.StatusInternalServerError)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"path": fileURL,
		"name": fileHeader.Filename,
	})
}
