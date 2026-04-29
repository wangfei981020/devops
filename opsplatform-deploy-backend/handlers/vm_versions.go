package handlers

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"opsplatform-deploy-backend/crypto"
	"opsplatform-deploy-backend/database"
)

// =========================================================================
//   VM 版本列表代理
//
// 调用流程：
//   1. 前端要求 service 的版本列表
//   2. 后端读 global_config.list_version_api + list_version_token
//   3. 拼 path = /data/vcs/<project>/tidb/<service>
//   4. GET <api>?path=<path>&token=<token>
//   5. 返回 versions 数组（最新在前；trust API order，不再排序）
// =========================================================================

// GET /api/vm-services/{id}/versions
//
//	返回该 service 的版本列表（commit hash 字符串数组），最新在前
func HandleListVmServiceVersions(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	var name string
	var envID int64
	if err := database.DB.QueryRow(
		`SELECT name, vm_project_env_id FROM vm_service WHERE id=?`, id).
		Scan(&name, &envID); err != nil {
		JSONError(w, 40400, "vm_service not found")
		return
	}
	// 数据级权限：用户对 service 所属 vm_project_env 有访问权
	if !IsVmEnvIDAllowed(r, envID) {
		JSONError(w, 40300, "无权访问该 VM 服务的版本列表")
		return
	}
	v, err := loadVmProjectEnv(envID)
	if err != nil {
		JSONError(w, 40404, "vm_project_env not found")
		return
	}

	// 读全局配置 list_version_api + token
	var apiURL, encToken string
	_ = database.DB.QueryRow(
		`SELECT IFNULL(list_version_api,''), IFNULL(list_version_token,'') FROM global_config WHERE id=1`).
		Scan(&apiURL, &encToken)
	if apiURL == "" {
		JSONError(w, 40001, "list_version_api 未在系统设置配置")
		return
	}
	token, _ := crypto.Decrypt(encToken)
	if token == "" {
		JSONError(w, 40001, "list_version_token 未配置或解密失败")
		return
	}

	// 拼调用 URL：<api>?path=/data/vcs/<project>/tidb/<service>&token=<token>
	path := fmt.Sprintf("/data/vcs/%s/tidb/%s", v.ProjectCode, name)
	u := apiURL + "?path=" + url.QueryEscape(path) + "&token=" + url.QueryEscape(token)

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // 内网 API，可能自签
		},
	}
	resp, err := client.Get(u)
	if err != nil {
		log.Printf("[vm-versions] svc=%d call list-version api failed: %v", id, err)
		JSONError(w, 50001, "调 list-version 接口失败，详情见服务端日志")
		return
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		log.Printf("[vm-versions] svc=%d list-version api status=%d body=%s", id, resp.StatusCode, string(raw))
		JSONError(w, 50001, "list-version 接口返回 "+strconv.Itoa(resp.StatusCode)+"，详情见服务端日志")
		return
	}
	var parsed struct {
		Versions []string `json:"versions"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		log.Printf("[vm-versions] svc=%d parse response failed: %v body=%s", id, err, string(raw))
		JSONError(w, 50001, "list-version 接口响应格式错误，详情见服务端日志")
		return
	}
	JSONSuccess(w, map[string]interface{}{
		"versions": parsed.Versions,
		"count":    len(parsed.Versions),
		"path":     path,
	})
}
