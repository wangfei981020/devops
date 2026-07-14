package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/models"
)

// 新增模块任务：提交后台异步 → git(clone+helm+commit+push) → 真部署则轮询 ArgoCD 新 app 到 Synced+Healthy。
// 「新增历史」页展示状态/commit/同步结果(argocd_results)/失败报错，支持重试(手动触发 ArgoCD 同步)。

func insertOrchTask(peID int64, envName, moduleName, kind, operator string, disable bool) (int64, error) {
	d := 0
	if disable {
		d = 1
	}
	res, err := database.DB.Exec(`INSERT INTO orchestration_task
		(project_env_id, env_name, module_name, kind, operator, status, disable) VALUES (?,?,?,?,?,'pending',?)`,
		peID, envName, moduleName, kind, operator, d)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// git 失败：直接标失败
func updateOrchTaskFailed(id int64, msg string) {
	_, _ = database.DB.Exec(`UPDATE orchestration_task SET status='failed', error_msg=? WHERE id=?`, msg, id)
}

// git 成功后先记 commit（状态仍 pending，等部署结果）
func setOrchTaskCommit(id int64, sha, url string) {
	_, _ = database.DB.Exec(`UPDATE orchestration_task SET commit_sha=?, commit_url=? WHERE id=?`, sha, url, id)
}

// 记录解析出的 ArgoCD app 名 + namespace（供重试/看 pod 日志）
func setOrchTaskApp(id int64, appName, namespace string) {
	_, _ = database.DB.Exec(`UPDATE orchestration_task SET app_name=?, namespace=? WHERE id=?`, appName, namespace, id)
}

// 轮询中渐进式写同步结果
func setOrchTaskArgocd(id int64, resultsJSON []byte) {
	_, _ = database.DB.Exec(`UPDATE orchestration_task SET argocd_results=? WHERE id=?`, resultsJSON, id)
}

// 终态：状态 + 说明(error_msg 复用) + 同步结果 + 耗时
func finishOrchTask(id int64, status, note string, resultsJSON []byte, durationSec int) {
	_, _ = database.DB.Exec(
		`UPDATE orchestration_task SET status=?, error_msg=?, argocd_results=?, duration_sec=? WHERE id=?`,
		status, note, resultsJSON, durationSec, id)
}

// 重试前重置成干净 pending
func resetOrchTaskForRetry(id int64) {
	_, _ = database.DB.Exec(
		`UPDATE orchestration_task SET status='pending', error_msg='', argocd_results=NULL, duration_sec=0 WHERE id=?`, id)
}

type orchTaskRow struct {
	ID            int64                    `json:"id"`
	ProjectEnvID  int64                    `json:"project_env_id"`
	EnvName       string                   `json:"env_name"`
	ModuleName    string                   `json:"module_name"`
	Kind          string                   `json:"kind"`
	Operator      string                   `json:"operator"`
	Status        string                   `json:"status"`
	CommitSHA     string                   `json:"commit_sha"`
	CommitURL     string                   `json:"commit_url"`
	ErrorMsg      string                   `json:"error_msg"`
	ArgocdResults []models.ArgocdAppResult `json:"argocd_results"`
	DurationSec   int                      `json:"duration_sec"`
	Disable       bool                     `json:"disable"`
	AppName       string                   `json:"app_name"`
	Namespace     string                   `json:"namespace"`
	CreatedAt     time.Time                `json:"created_at"`
}

// HandleListOrchTasks GET /api/orchestration/tasks —— 新增历史列表（筛选 + 分页 + 环境权限）。
//
//	参数：env(env_name 精确) / kind(single|batch) / status(pending|success|failed) /
//	  operator(模糊) / module(module_name 模糊) / time_from / time_to / page / page_size。
//	权限：非 admin 按 AllowedEnvIDs 过滤（project_env_id IN，挪进 SQL——替代旧的"取 200 条内存逐条过滤"，
//	  否则分页 total 会不准）。返回 {list, total}。
func HandleListOrchTasks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	where := " WHERE 1=1"
	var args []interface{}
	if v := strings.TrimSpace(q.Get("env")); v != "" {
		where += " AND env_name = ?"
		args = append(args, v)
	}
	if v := strings.TrimSpace(q.Get("kind")); v != "" {
		where += " AND kind = ?"
		args = append(args, v)
	}
	if v := strings.TrimSpace(q.Get("status")); v != "" {
		where += " AND status = ?"
		args = append(args, v)
	}
	if v := strings.TrimSpace(q.Get("operator")); v != "" {
		where += " AND operator LIKE ?"
		args = append(args, "%"+v+"%")
	}
	if v := strings.TrimSpace(q.Get("module")); v != "" {
		where += " AND module_name LIKE ?"
		args = append(args, "%"+v+"%")
	}
	if v := strings.TrimSpace(q.Get("time_from")); v != "" {
		where += " AND created_at >= ?"
		args = append(args, v)
	}
	if v := strings.TrimSpace(q.Get("time_to")); v != "" {
		where += " AND created_at <= ?"
		args = append(args, v)
	}
	// 环境权限：非 admin 只看 AllowedEnvIDs（enforce=false 时是 admin，不加过滤）
	if ids, enforce := AllowedEnvIDs(r); enforce {
		if len(ids) == 0 {
			JSONSuccess(w, map[string]interface{}{"list": []orchTaskRow{}, "total": 0})
			return
		}
		ph := strings.Repeat("?,", len(ids))
		where += " AND project_env_id IN (" + ph[:len(ph)-1] + ")"
		for _, id := range ids {
			args = append(args, id)
		}
	}

	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(q.Get("page_size"))
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}

	var total int64
	_ = database.DB.QueryRow("SELECT COUNT(*) FROM orchestration_task"+where, args...).Scan(&total)

	args2 := append(append([]interface{}{}, args...), pageSize, (page-1)*pageSize)
	rows, err := database.DB.Query(`SELECT id, project_env_id, env_name, module_name, kind, operator, status,
		IFNULL(commit_sha,''), IFNULL(commit_url,''), IFNULL(error_msg,''), IFNULL(argocd_results,''),
		duration_sec, disable, IFNULL(app_name,''), IFNULL(namespace,''), created_at
		FROM orchestration_task`+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, args2...)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	defer rows.Close()
	list := []orchTaskRow{}
	for rows.Next() {
		var t orchTaskRow
		var argoRaw string
		var disable int
		if err := rows.Scan(&t.ID, &t.ProjectEnvID, &t.EnvName, &t.ModuleName, &t.Kind, &t.Operator,
			&t.Status, &t.CommitSHA, &t.CommitURL, &t.ErrorMsg, &argoRaw,
			&t.DurationSec, &disable, &t.AppName, &t.Namespace, &t.CreatedAt); err != nil {
			continue
		}
		t.Disable = disable == 1
		_ = jsonUnmarshalImpl([]byte(argoRaw), &t.ArgocdResults)
		if t.ArgocdResults == nil {
			t.ArgocdResults = []models.ArgocdAppResult{}
		}
		list = append(list, t)
	}
	JSONSuccess(w, map[string]interface{}{"list": list, "total": total})
}
