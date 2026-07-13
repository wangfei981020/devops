package handlers

import (
	"net/http"
	"time"

	"opsplatform-deploy-backend/database"
)

// 新增模块任务（服务编排提交改后台异步后，状态/commit 落这张表，「新增历史」页展示）。

func insertOrchTask(peID int64, envName, moduleName, kind, operator string) (int64, error) {
	res, err := database.DB.Exec(`INSERT INTO orchestration_task
		(project_env_id, env_name, module_name, kind, operator, status) VALUES (?,?,?,?,?,'pending')`,
		peID, envName, moduleName, kind, operator)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func updateOrchTaskSuccess(id int64, sha, url string) {
	_, _ = database.DB.Exec(
		`UPDATE orchestration_task SET status='success', commit_sha=?, commit_url=?, error_msg='' WHERE id=?`,
		sha, url, id)
}

func updateOrchTaskFailed(id int64, msg string) {
	_, _ = database.DB.Exec(`UPDATE orchestration_task SET status='failed', error_msg=? WHERE id=?`, msg, id)
}

type orchTaskRow struct {
	ID           int64     `json:"id"`
	ProjectEnvID int64     `json:"project_env_id"`
	EnvName      string    `json:"env_name"`
	ModuleName   string    `json:"module_name"`
	Kind         string    `json:"kind"`
	Operator     string    `json:"operator"`
	Status       string    `json:"status"`
	CommitSHA    string    `json:"commit_sha"`
	CommitURL    string    `json:"commit_url"`
	ErrorMsg     string    `json:"error_msg"`
	CreatedAt    time.Time `json:"created_at"`
}

// HandleListOrchTasks GET /api/orchestration/tasks —— 新增历史列表（最近 200 条，按环境权限过滤）。
func HandleListOrchTasks(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`SELECT id, project_env_id, env_name, module_name, kind, operator, status,
		IFNULL(commit_sha,''), IFNULL(commit_url,''), IFNULL(error_msg,''), created_at
		FROM orchestration_task ORDER BY id DESC LIMIT 200`)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	defer rows.Close()
	list := []orchTaskRow{}
	for rows.Next() {
		var t orchTaskRow
		if err := rows.Scan(&t.ID, &t.ProjectEnvID, &t.EnvName, &t.ModuleName, &t.Kind, &t.Operator,
			&t.Status, &t.CommitSHA, &t.CommitURL, &t.ErrorMsg, &t.CreatedAt); err != nil {
			continue
		}
		// 只看有权限的环境；error_msg 是 helm/git（已 scrub token），保留给开发排错，不脱敏
		if !IsEnvIDAllowed(r, t.ProjectEnvID) {
			continue
		}
		list = append(list, t)
	}
	JSONSuccess(w, map[string]interface{}{"list": list})
}
