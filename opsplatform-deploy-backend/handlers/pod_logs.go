package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/models"
	"opsplatform-deploy-backend/services"
)

// =========================================================================
//   pod 排查 API（C 计划：失败时点「查看日志」拉真实 panic 堆栈）
//
// 两个端点：
//   GET /api/deployments/{id}/pods?app=<argocd_app_name>
//     → 列出该 app 当前的 pod 状态（用 argocd resource-tree）
//   GET /api/deployments/{id}/pod-logs?app=&pod=&namespace=&previous=&tail_lines=
//     → 拉具体 pod 容器日志（透传给 argocd /logs 接口）
//
// 防御措施（避免给 argocd 增加压力）：
//   1. 1 秒防抖缓存：同 (app, pod, container, previous) 1s 内重复请求复用缓存
//   2. tail_lines 上限 2000，超过自动收敛
//   3. 单次 argocd 调用 30s 超时（GetPodLogs 内部）
//   4. 不开放给非鉴权用户；env 权限检查复用 IsEnvIDAllowed
//   5. 不自动轮询（这是前端约束，后端无 streaming）
// =========================================================================

// HandleGetDeploymentPods GET /api/deployments/{id}/pods?app=<argocd_app_name>
//
//	返回该 app 当前所有 pod 的 name + namespace + 状态原因 + 重启次数。
//	供前端「查看日志」弹窗的 pod 选择器用——一个 deployment 通常 1 个失败 pod，
//	但多副本场景可能 2-3 个。
type podBrief struct {
	Name         string `json:"name"`
	Namespace    string `json:"namespace"`
	Health       string `json:"health"`
	StatusReason string `json:"status_reason"`
	RestartCount string `json:"restart_count"`
	ContainersOK string `json:"containers_ready"`
}

func HandleGetDeploymentPods(w http.ResponseWriter, r *http.Request) {
	depID := ParseID(mux.Vars(r)["id"])
	app := r.URL.Query().Get("app")
	if app == "" {
		JSONError(w, 40000, "app 参数必填")
		return
	}

	// 拿到 deployment 对应的 project_env，校验 env 权限 + 取 argocd 凭证
	p, err := loadProjectEnvByDeployment(depID)
	if err != nil {
		JSONError(w, 40400, "deployment 不存在")
		return
	}
	if !IsEnvIDAllowed(r, p.ID) {
		JSONError(w, 40300, "无权访问该环境")
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
			Name:         n.Name,
			Namespace:    n.Namespace,
			Health:       n.Health,
			StatusReason: n.StatusReason,
			RestartCount: n.RestartCount,
			ContainersOK: n.ContainersOK,
		})
	}
	JSONSuccess(w, map[string]interface{}{
		"pods": pods,
	})
}

// ---- pod-logs 接口（带 1s 防抖缓存） ----

type logCacheEntry struct {
	logs string
	at   time.Time
}

var (
	podLogsCache   sync.Map // key=app|pod|container|previous → *logCacheEntry
	podLogsCacheMu sync.Mutex
)

// HandleGetDeploymentPodLogs GET /api/deployments/{id}/pod-logs
//
//	?app=...&pod=...&namespace=...&container=...&previous=true&tail_lines=200
//
//	返回 {"logs": "...", "lines": 187, "previous": true}
func HandleGetDeploymentPodLogs(w http.ResponseWriter, r *http.Request) {
	depID := ParseID(mux.Vars(r)["id"])
	q := r.URL.Query()
	app := q.Get("app")
	pod := q.Get("pod")
	namespace := q.Get("namespace")
	container := q.Get("container") // 可空，argocd 默认主 container
	previous := q.Get("previous") == "true"
	tailLines, _ := strconv.Atoi(q.Get("tail_lines"))
	if tailLines <= 0 {
		tailLines = 200
	}
	if tailLines > 2000 {
		tailLines = 2000
	}
	if app == "" || pod == "" || namespace == "" {
		JSONError(w, 40000, "app / pod / namespace 必填")
		return
	}

	// env 权限校验
	p, err := loadProjectEnvByDeployment(depID)
	if err != nil {
		JSONError(w, 40400, "deployment 不存在")
		return
	}
	if !IsEnvIDAllowed(r, p.ID) {
		JSONError(w, 40300, "无权访问该环境")
		return
	}

	// 1s 防抖缓存：粗粒度 key（不带 tailLines，因为 200/500/2000 都从同一日志流取，
	// 缓存按"全部加载过一次"对待，前端根据 tail_lines 客户端裁剪也可，但简单起见
	// 我们仍按完整 key 区分缓存）
	cacheKey := app + "|" + pod + "|" + container + "|" + strconv.FormatBool(previous) + "|" + strconv.Itoa(tailLines)
	if v, ok := podLogsCache.Load(cacheKey); ok {
		if e, ok := v.(*logCacheEntry); ok && time.Since(e.at) < time.Second {
			respondPodLogs(w, e.logs, previous)
			return
		}
	}

	// 串行同一 key 的并发请求（防多人同时狂点刷新打 argocd）
	podLogsCacheMu.Lock()
	defer podLogsCacheMu.Unlock()
	// 双检：可能其他 goroutine 刚填好缓存
	if v, ok := podLogsCache.Load(cacheKey); ok {
		if e, ok := v.(*logCacheEntry); ok && time.Since(e.at) < time.Second {
			respondPodLogs(w, e.logs, previous)
			return
		}
	}

	argoURL, argoToken, err := ResolveArgocdForEnv(p)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	client := services.NewArgocdClient(argoURL, argoToken)
	logs, err := client.GetPodLogs(r.Context(), app, namespace, pod, container, tailLines, previous)
	if err != nil {
		// 上一次崩溃前没日志（pod 首次部署）→ argocd 通常返回 400，前端友好提示
		JSONError(w, 50000, "拉取日志失败 · "+err.Error())
		return
	}
	podLogsCache.Store(cacheKey, &logCacheEntry{logs: logs, at: time.Now()})
	respondPodLogs(w, logs, previous)
}

func respondPodLogs(w http.ResponseWriter, logs string, previous bool) {
	lines := 0
	for i := 0; i < len(logs); i++ {
		if logs[i] == '\n' {
			lines++
		}
	}
	JSONSuccess(w, map[string]interface{}{
		"logs":     logs,
		"lines":    lines,
		"previous": previous,
	})
}

// loadProjectEnvByDeployment 通过 deployment id 反查它部署的 project_env
func loadProjectEnvByDeployment(depID int64) (*models.ProjectEnv, error) {
	var peID int64
	if err := database.DB.QueryRow(`SELECT project_env_id FROM deployment WHERE id=?`, depID).
		Scan(&peID); err != nil {
		return nil, err
	}
	p := &models.ProjectEnv{}
	row := database.DB.QueryRow(`SELECT id, name, display_name, env_type, git_repo, git_branch,
		chart_base_path, namespace, IFNULL(argocd_url,''), IFNULL(argocd_token,''),
		IFNULL(lark_webhook,''), IFNULL(lark_secret,''), auto_sync, IFNULL(argocd_instance_id, 0),
		IFNULL(lark_bot_id, 0), IFNULL(gitlab_repo_id, 0)
		FROM project_env WHERE id=?`, peID)
	var argocdInstanceID, larkBotID, gitlabRepoID int64
	err := row.Scan(&p.ID, &p.Name, &p.DisplayName, &p.EnvType, &p.GitRepo, &p.GitBranch,
		&p.ChartBasePath, &p.Namespace, &p.ArgocdURL, &p.ArgocdToken,
		&p.LarkWebhook, &p.LarkSecret, &p.AutoSync, &argocdInstanceID,
		&larkBotID, &gitlabRepoID)
	if err != nil {
		return nil, err
	}
	if argocdInstanceID > 0 {
		p.ArgocdInstanceID = &argocdInstanceID
	}
	if larkBotID > 0 {
		p.LarkBotID = &larkBotID
	}
	if gitlabRepoID > 0 {
		p.GitlabRepoID = &gitlabRepoID
	}
	return p, nil
}

// json import 兜底（如果文件没有别处用 json 编译会有未引用警告，但 marshaling 在 helpers）
var _ = json.Marshal
