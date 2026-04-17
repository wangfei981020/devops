package handlers

import (
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/models"
)

func HandleListSecrets(w http.ResponseWriter, r *http.Request) {
	peID := r.URL.Query().Get("project_env_id")
	if peID == "" {
		jsonError(w, 40001, "project_env_id 必填")
		return
	}
	rows, err := database.DB.Query(`SELECT id, project_env_id, name, type, IFNULL(data,''), description,
		synced_at, sync_status, IFNULL(sync_error,''), created_at, updated_at
		FROM secrets WHERE project_env_id=? ORDER BY name`, peID)
	if err != nil {
		jsonError(w, 50000, err.Error())
		return
	}
	defer rows.Close()
	list := []models.Secret{}
	for rows.Next() {
		var s models.Secret
		rows.Scan(&s.ID, &s.ProjectEnvID, &s.Name, &s.Type, &s.Data, &s.Description,
			&s.SyncedAt, &s.SyncStatus, &s.SyncError, &s.CreatedAt, &s.UpdatedAt)
		// 列表不返回 data 内容, 避免泄露
		s.Data = ""
		list = append(list, s)
	}
	jsonSuccess(w, list)
}

func HandleGetSecret(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	reveal := r.URL.Query().Get("reveal") == "1"
	var s models.Secret
	err := database.DB.QueryRow(`SELECT id, project_env_id, name, type, IFNULL(data,''), description,
		synced_at, sync_status, IFNULL(sync_error,''), created_at, updated_at
		FROM secrets WHERE id=?`, id).
		Scan(&s.ID, &s.ProjectEnvID, &s.Name, &s.Type, &s.Data, &s.Description,
			&s.SyncedAt, &s.SyncStatus, &s.SyncError, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		jsonError(w, 40400, "Secret 不存在")
		return
	}
	if !reveal {
		// TODO: 解密 data 各 value 后再脱敏返回; 阶段 3 完善
		s.Data = ""
	}
	jsonSuccess(w, s)
}

func HandleCreateSecret(w http.ResponseWriter, r *http.Request) {
	jsonSuccess(w, map[string]interface{}{"status": "stub", "msg": "新建Secret: 待实现 AES加密+渲染git (阶段3)"})
}

func HandleUpdateSecret(w http.ResponseWriter, r *http.Request) {
	jsonSuccess(w, map[string]interface{}{"status": "stub", "msg": "更新Secret: 待实现 (阶段3)"})
}

func HandleDeleteSecret(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	_, err := database.DB.Exec(`DELETE FROM secrets WHERE id=?`, id)
	if err != nil {
		jsonError(w, 50000, err.Error())
		return
	}
	jsonSuccess(w, nil)
}

func HandleBatchUpdateSecret(w http.ResponseWriter, r *http.Request) {
	jsonSuccess(w, map[string]interface{}{"status": "stub", "msg": "批量修改Secret字段: 待实现 (阶段3)"})
}

func HandleSecretReferencedBy(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	// 查 secret 的 name + project_env_id, 然后扫所有同 project_env 的模块, 看 extra_env_vars 是否包含
	var name string
	var peID int64
	err := database.DB.QueryRow(`SELECT name, project_env_id FROM secrets WHERE id=?`, id).Scan(&name, &peID)
	if err != nil {
		jsonError(w, 40400, "Secret 不存在")
		return
	}
	rows, err := database.DB.Query(`SELECT id, name, IFNULL(extra_env_vars,'') FROM modules WHERE project_env_id=? AND status!='deleted'`, peID)
	if err != nil {
		jsonError(w, 50000, err.Error())
		return
	}
	defer rows.Close()
	type ref struct {
		ModuleID   int64  `json:"module_id"`
		ModuleName string `json:"module_name"`
	}
	refs := []ref{}
	for rows.Next() {
		var mid int64
		var mname, eev string
		rows.Scan(&mid, &mname, &eev)
		// 简单字符串包含判断 (后续可改 JSON parse)
		if eev != "" && containsName(eev, name) {
			refs = append(refs, ref{mid, mname})
		}
	}
	jsonSuccess(w, refs)
}

func containsName(jsonStr, name string) bool {
	// 在 JSON 数组里查找 name 字面量, 简化实现
	target := `"` + name + `"`
	for i := 0; i+len(target) <= len(jsonStr); i++ {
		if jsonStr[i:i+len(target)] == target {
			return true
		}
	}
	return false
}
