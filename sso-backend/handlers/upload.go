package handlers

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"sso-backend/database"
	"sso-backend/storage"
)

var allowedImageTypes = map[string]bool{
	"image/png":     true,
	"image/jpeg":    true,
	"image/gif":     true,
	"image/webp":    true,
	"image/svg+xml": true,
	"image/x-icon":  true,
}

const maxUploadSize = 2 << 20 // 2MB

func HandleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if !storage.IsReady() {
		jsonError(w, http.StatusServiceUnavailable, "存储服务不可用")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		jsonError(w, http.StatusBadRequest, "文件过大，最大 2MB")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "请选择文件")
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	if !allowedImageTypes[contentType] {
		jsonError(w, http.StatusBadRequest, "仅支持图片文件 (PNG, JPG, GIF, WebP, SVG, ICO)")
		return
	}

	ext := filepath.Ext(header.Filename)
	safeName := strings.ReplaceAll(strings.TrimSuffix(header.Filename, ext), " ", "_")
	objectName := fmt.Sprintf("logos/%d_%s%s", time.Now().UnixMilli(), safeName, ext)

	url, err := storage.GetStorage().Upload(r.Context(), objectName, file, header.Size, contentType)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "上传失败")
		return
	}

	adminID := GetUserID(r)
	adminName := GetUsername(r)
	database.SaveAuditLog(adminID, adminName, "upload_logo", "file", objectName, "上传应用图标", GetClientIP(r), r.UserAgent(), "success")

	jsonOK(w, map[string]string{"url": url})
}
