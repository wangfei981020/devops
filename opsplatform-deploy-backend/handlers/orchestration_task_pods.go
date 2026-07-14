package handlers

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/models"
	"opsplatform-deploy-backend/services"
)

// 新增历史「查看日志」：直接查 ArgoCD live（新增的 app 还在，pod 一般还活着），不走归档。

// loadEnvForTask 由任务 id 拿到所属 project_env（解密，含 argocd 凭证），并校验权限。
func loadEnvForTask(r *http.Request, taskID int64) (*models.ProjectEnv, bool) {
	var peID int64
	if err := database.DB.QueryRow(`SELECT project_env_id FROM orchestration_task WHERE id=?`, taskID).Scan(&peID); err != nil {
		return nil, false
	}
	if !IsEnvIDAllowed(r, peID) {
		return nil, false
	}
	p, err := LoadProjectEnvDecrypted(peID)
	if err != nil {
		return nil, false
	}
	return p, true
}

// HandleGetOrchTaskPods GET /api/orchestration/tasks/{id}/pods?app=<argocd_app_name>
func HandleGetOrchTaskPods(w http.ResponseWriter, r *http.Request) {
	taskID := ParseID(mux.Vars(r)["id"])
	app := r.URL.Query().Get("app")
	if app == "" {
		JSONError(w, 40000, "app 参数必填")
		return
	}
	p, ok := loadEnvForTask(r, taskID)
	if !ok {
		JSONError(w, 40400, "任务不存在或无权访问")
		return
	}
	argoURL, argoToken, err := ResolveArgocdForEnv(p)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	client := services.NewArgocdClient(argoURL, argoToken)
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	nodes, err := client.GetAppResourceTree(ctx, app)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	pods := []podBrief{}
	for _, n := range nodes {
		if n.Kind != "Pod" {
			continue
		}
		pods = append(pods, podBrief{
			Name: n.Name, Namespace: n.Namespace, UID: n.UID, Health: n.Health,
			StatusReason: n.StatusReason, RestartCount: n.RestartCount, ContainersOK: n.ContainersOK,
		})
	}
	JSONSuccess(w, map[string]interface{}{"pods": pods})
}

// HandleGetOrchTaskPodLogs GET /api/orchestration/tasks/{id}/pod-logs?app=&pod=&namespace=&container=&previous=&tail_lines=
func HandleGetOrchTaskPodLogs(w http.ResponseWriter, r *http.Request) {
	taskID := ParseID(mux.Vars(r)["id"])
	q := r.URL.Query()
	app := q.Get("app")
	pod := q.Get("pod")
	namespace := q.Get("namespace")
	container := q.Get("container")
	previous := q.Get("previous") == "true"
	tailLines, _ := strconv.Atoi(q.Get("tail_lines"))
	if tailLines <= 0 {
		tailLines = 200
	}
	if app == "" || pod == "" {
		JSONError(w, 40000, "app / pod 必填")
		return
	}
	p, ok := loadEnvForTask(r, taskID)
	if !ok {
		JSONError(w, 40400, "任务不存在或无权访问")
		return
	}
	argoURL, argoToken, err := ResolveArgocdForEnv(p)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	client := services.NewArgocdClient(argoURL, argoToken)
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	logs, err := client.GetPodLogs(ctx, app, namespace, pod, container, tailLines, previous)
	if err != nil {
		JSONError(w, 40001, "拉取日志失败: "+err.Error())
		return
	}
	JSONSuccess(w, map[string]interface{}{"logs": logs, "source": "live"})
}
