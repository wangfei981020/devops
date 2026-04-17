package handlers

import (
	"context"
	"io"
	"net/http"
	"strconv"

	"opsplatform-probe-backend/database"
	"opsplatform-probe-backend/services"
)

// HandleAgentUpgradeDownload streams the binary from MinIO to the Agent.
func HandleAgentUpgradeDownload(w http.ResponseWriter, r *http.Request) {
	version := r.URL.Query().Get("version")
	if version == "" {
		jsonError(w, http.StatusBadRequest, "version required")
		return
	}
	var minioKey, sha, signature string
	var size int64
	err := database.DB.QueryRow(
		"SELECT minio_key, sha256, size_bytes, COALESCE(signature,'') FROM agent_versions WHERE version=?", version,
	).Scan(&minioKey, &sha, &size, &signature)
	if err != nil {
		jsonError(w, http.StatusNotFound, "version not found")
		return
	}
	if !services.MinIOReady() {
		jsonError(w, http.StatusServiceUnavailable, "minio not configured")
		return
	}
	rc, _, err := services.GetObjectStream(context.Background(), minioKey)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "fetch from minio failed")
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.Header().Set("X-SHA256", sha)
	if signature != "" {
		w.Header().Set("X-Signature", signature)
	}
	io.Copy(w, rc)
}
