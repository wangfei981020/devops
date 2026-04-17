package handlers

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"regexp"

	"github.com/gorilla/mux"
	"opsplatform-probe-backend/database"
	"opsplatform-probe-backend/services"
)

// verifyEd25519 verifies a base64-encoded ed25519 signature against the data using the configured PEM public key.
// Returns nil on success. If signing isn't configured, returns nil only when not required.
func verifyEd25519(data []byte, sigB64 string) error {
	if cfg.AgentPublicKeyPEM == "" {
		if cfg.RequireSignedUploads {
			return fmt.Errorf("REQUIRE_SIGNED_UPLOADS=true 但 AGENT_PUBLIC_KEY_PEM 未配置")
		}
		return nil // signing disabled
	}
	if sigB64 == "" {
		if cfg.RequireSignedUploads {
			return fmt.Errorf("缺少签名 (REQUIRE_SIGNED_UPLOADS=true)")
		}
		return nil // optional and not provided
	}
	block, _ := pem.Decode([]byte(cfg.AgentPublicKeyPEM))
	if block == nil {
		return fmt.Errorf("AGENT_PUBLIC_KEY_PEM 解析失败")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("解析公钥失败: %w", err)
	}
	edPub, ok := pub.(ed25519.PublicKey)
	if !ok {
		return fmt.Errorf("公钥不是 ed25519 类型")
	}
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return fmt.Errorf("签名 base64 解码失败: %w", err)
	}
	if !ed25519.Verify(edPub, data, sig) {
		return fmt.Errorf("签名校验失败")
	}
	return nil
}

var versionRe = regexp.MustCompile(`^v?\d+\.\d+\.\d+(-[a-zA-Z0-9.\-]+)?$`)

// HandleListVersions returns all uploaded agent versions.
func HandleListVersions(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(
		`SELECT id, version, minio_key, sha256, size_bytes, os, arch, source, source_image, changelog, uploaded_by, uploaded_at
		   FROM agent_versions ORDER BY id DESC`,
	)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()
	list := []map[string]interface{}{}
	for rows.Next() {
		var id int
		var size int64
		var version, key, sha, os, arch, source, sourceImg, changelog, uploadedBy, uploadedAt string
		rows.Scan(&id, &version, &key, &sha, &size, &os, &arch, &source, &sourceImg, &changelog, &uploadedBy, &uploadedAt)
		list = append(list, map[string]interface{}{
			"id":           id,
			"version":      version,
			"minio_key":    key,
			"sha256":       sha,
			"size_bytes":   size,
			"os":           os,
			"arch":         arch,
			"source":       source,
			"source_image": sourceImg,
			"changelog":    changelog,
			"uploaded_by":  uploadedBy,
			"uploaded_at":  uploadedAt,
		})
	}
	jsonSuccess(w, list)
}

// HandleUploadVersion accepts a multipart file upload.
func HandleUploadVersion(w http.ResponseWriter, r *http.Request) {
	if !services.MinIOReady() {
		jsonError(w, http.StatusServiceUnavailable, "MinIO 未配置")
		return
	}
	if err := r.ParseMultipartForm(80 << 20); err != nil {
		jsonError(w, http.StatusBadRequest, "parse form failed")
		return
	}
	version := r.FormValue("version")
	changelog := r.FormValue("changelog")
	osField := r.FormValue("os")
	archField := r.FormValue("arch")
	signature := r.FormValue("signature") // base64 ed25519 signature, optional
	if osField == "" {
		osField = "linux"
	}
	if archField == "" {
		archField = "amd64"
	}
	if !versionRe.MatchString(version) {
		jsonError(w, http.StatusBadRequest, "version 格式无效，应为 v1.2.3")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "file required")
		return
	}
	defer file.Close()
	if header.Size > 80*1024*1024 {
		jsonError(w, http.StatusBadRequest, "文件过大 (最大 80MB)")
		return
	}
	data, err := io.ReadAll(file)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "read file failed")
		return
	}
	// Strict ELF validation (fix #7)
	if osField == "linux" {
		if err := validateLinuxELF(data, archField); err != nil {
			jsonError(w, http.StatusBadRequest, "文件校验失败: "+err.Error())
			return
		}
	}
	// Code signing verification (fix #A)
	if err := verifyEd25519(data, signature); err != nil {
		jsonError(w, http.StatusBadRequest, "签名校验失败: "+err.Error())
		return
	}
	if err := saveVersion(r, version, data, osField, archField, "upload", "", changelog, signature); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonSuccess(w, map[string]interface{}{"version": version, "size": len(data)})
}

// HandleImportVersionFromImage pulls a container image, extracts the binary, and stores it.
type ImportImageReq struct {
	Image      string `json:"image"`
	BinaryPath string `json:"binary_path"`
	Version    string `json:"version"`
	Changelog  string `json:"changelog"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`
}

func HandleImportVersionFromImage(w http.ResponseWriter, r *http.Request) {
	if !services.MinIOReady() {
		jsonError(w, http.StatusServiceUnavailable, "MinIO 未配置")
		return
	}
	var req ImportImageReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid")
		return
	}
	if req.Image == "" {
		jsonError(w, http.StatusBadRequest, "image required")
		return
	}
	if req.BinaryPath == "" {
		req.BinaryPath = cfg.ImageBinaryPath
	}
	if req.Version == "" {
		// Use tag part as version
		if idx := indexLast(req.Image, ":"); idx > 0 {
			req.Version = req.Image[idx+1:]
		}
	}
	if !versionRe.MatchString(req.Version) {
		jsonError(w, http.StatusBadRequest, "version 格式无效")
		return
	}
	if req.OS == "" {
		req.OS = "linux"
	}
	if req.Arch == "" {
		req.Arch = "amd64"
	}

	result, err := services.PullAndExtract(cfg, req.Image, req.BinaryPath)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "镜像拉取/提取失败: "+err.Error())
		return
	}
	// 镜像导入无法附带签名. 若强制签名开启则拒绝.
	if cfg.RequireSignedUploads {
		jsonError(w, http.StatusBadRequest, "REQUIRE_SIGNED_UPLOADS=true, 镜像导入不支持签名, 请改用文件上传")
		return
	}
	if err := saveVersion(r, req.Version, result.Binary, req.OS, req.Arch, "image", req.Image, req.Changelog, ""); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonSuccess(w, map[string]interface{}{
		"version": req.Version,
		"sha256":  result.SHA256,
		"size":    result.Size,
	})
}

// saveVersion uploads to MinIO twice: versioned + public/latest, and writes DB row.
func saveVersion(r *http.Request, version string, data []byte, os, arch, source, sourceImage, changelog, signature string) error {
	// Refuse duplicate version
	var exists int
	database.DB.QueryRow("SELECT COUNT(*) FROM agent_versions WHERE version=?", version).Scan(&exists)
	if exists > 0 {
		return fmt.Errorf("版本 %s 已存在", version)
	}
	hash := sha256.Sum256(data)
	shaHex := hex.EncodeToString(hash[:])
	versionKey := fmt.Sprintf("agent-versions/%s/probe-agent-%s-%s", version, os, arch)
	publicKey := "public/latest/probe-agent"

	ctx := context.Background()
	if err := services.PutObjectBytes(ctx, versionKey, data, "application/octet-stream"); err != nil {
		return fmt.Errorf("upload to minio: %w", err)
	}
	if err := services.PutObjectBytes(ctx, publicKey, data, "application/octet-stream"); err != nil {
		return fmt.Errorf("upload latest: %w", err)
	}

	username := ""
	if v := r.Context().Value(contextUsername); v != nil {
		username = v.(string)
	}
	_, err := database.DB.Exec(
		`INSERT INTO agent_versions (version, minio_key, sha256, size_bytes, os, arch, source, source_image, changelog, uploaded_by, signature)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		version, versionKey, shaHex, int64(len(data)), os, arch, source, sourceImage, changelog, username, signature,
	)
	if err != nil {
		return fmt.Errorf("save db: %w", err)
	}
	SaveAuditLog(r, "upload_version", "version", version, source)
	return nil
}

func HandleDeleteVersion(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	var version, key string
	err := database.DB.QueryRow("SELECT version, minio_key FROM agent_versions WHERE id=?", id).Scan(&version, &key)
	if err != nil {
		jsonError(w, http.StatusNotFound, "版本不存在")
		return
	}
	// Refuse deletion if any agent is running this version
	var inUse int
	database.DB.QueryRow("SELECT COUNT(*) FROM agents WHERE version=?", version).Scan(&inUse)
	if inUse > 0 {
		jsonError(w, http.StatusBadRequest, "该版本正在被 Agent 使用，不允许删除")
		return
	}
	services.DeleteObject(context.Background(), key)
	database.DB.Exec("DELETE FROM agent_versions WHERE id=?", id)
	SaveAuditLog(r, "delete_version", "version", version, "")
	jsonSuccess(w, map[string]interface{}{"ok": true})
}

// validateLinuxELF checks magic, class (64-bit), data (little-endian), and e_machine matches arch.
// ELF64 layout (little-endian):
//
//	0..3   = 0x7f,'E','L','F'
//	4      = EI_CLASS  (2 = ELF64)
//	5      = EI_DATA   (1 = little-endian)
//	18..19 = e_machine (62=x86_64, 183=aarch64)
func validateLinuxELF(data []byte, arch string) error {
	if len(data) < 64 {
		return fmt.Errorf("文件过小")
	}
	if !bytes.HasPrefix(data, []byte{0x7f, 'E', 'L', 'F'}) {
		return fmt.Errorf("非 ELF 魔数")
	}
	if data[4] != 2 {
		return fmt.Errorf("仅支持 64 位 ELF (EI_CLASS=2)")
	}
	if data[5] != 1 {
		return fmt.Errorf("仅支持小端序 ELF (EI_DATA=1)")
	}
	machine := uint16(data[18]) | uint16(data[19])<<8
	expected := map[string]uint16{
		"amd64": 62,  // EM_X86_64
		"arm64": 183, // EM_AARCH64
	}
	if want, ok := expected[arch]; ok {
		if machine != want {
			return fmt.Errorf("架构不匹配: 文件 e_machine=%d, 期望 arch=%s", machine, arch)
		}
	}
	// Statically linked Go binaries expected: no dynamic interpreter segment (optional check skipped).
	return nil
}

func indexLast(s, sub string) int {
	for i := len(s) - len(sub); i >= 0; i-- {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
