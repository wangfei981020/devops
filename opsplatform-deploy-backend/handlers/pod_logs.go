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
	UID          string `json:"uid"` // 拉 events 必填，前端透传
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
			UID:          n.UID,
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
	podLogsCache   sync.Map // key=envID|namespace|app|pod|container|previous|tailLines → *logCacheEntry
	podLogsCacheMu sync.Mutex
)

// HandleGetDeploymentPodLogs GET /api/deployments/{id}/pod-logs
//
//	?app=...&pod=...&namespace=...&container=...&previous=true&tail_lines=200
//
//	返回 {"logs": "...", "lines": 187, "source": "archive"|"live"}
//
// 优先级（按 kind 不同决定）：
//   - previous=true（kind=previous）：archive 优先 —— 归档是 deploy 当时拉的快照，最可靠；失败回退 live --previous
//   - previous=false（kind=current）：live 优先 —— 拿最新输出；失败（pod 已被收走）回退 archive
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
	if app == "" || pod == "" {
		JSONError(w, 40000, "app / pod 必填")
		return
	}

	p, err := loadProjectEnvByDeployment(depID)
	if err != nil {
		JSONError(w, 40400, "deployment 不存在")
		return
	}
	if !IsEnvIDAllowed(r, p.ID) {
		JSONError(w, 40300, "无权访问该环境")
		return
	}

	kind := services.LogKindCurrent
	if previous {
		kind = services.LogKindPrevious
	}

	// previous 路径：archive 优先
	if previous {
		if logs, ok := tryArchive(r.Context(), depID, app, pod, kind); ok {
			respondPodLogs(w, logs, "archive")
			return
		}
	}

	// live 路径（current 优先 / previous 回退）
	if namespace != "" {
		// 🔒 缓存 key 必须含 envID + namespace 防跨租户串台：
		// 多 ArgoCD 实例 / 不同 env 出现同名 app 时（Helm 模板很容易撞），1 秒缓存窗口里
		// 一个用户的查询可能命中另一个 env 的数据。
		cacheKey := strconv.FormatInt(p.ID, 10) + "|" + namespace + "|" + app + "|" + pod + "|" + container + "|" + strconv.FormatBool(previous) + "|" + strconv.Itoa(tailLines)
		if v, ok := podLogsCache.Load(cacheKey); ok {
			if e, ok := v.(*logCacheEntry); ok && time.Since(e.at) < time.Second {
				respondPodLogs(w, e.logs, "live")
				return
			}
		}
		podLogsCacheMu.Lock()
		argoURL, argoToken, perr := ResolveArgocdForEnv(p)
		if perr == nil {
			client := services.NewArgocdClient(argoURL, argoToken)
			logs, lerr := client.GetPodLogs(r.Context(), app, namespace, pod, container, tailLines, previous)
			if lerr == nil && logs != "" {
				podLogsCache.Store(cacheKey, &logCacheEntry{logs: logs, at: time.Now()})
				podLogsCacheMu.Unlock()
				respondPodLogs(w, logs, "live")
				return
			}
		}
		podLogsCacheMu.Unlock()
	}

	// current 路径回退到 archive（pod 已被收走、namespace 缺失等场景）
	if !previous {
		if logs, ok := tryArchive(r.Context(), depID, app, pod, kind); ok {
			respondPodLogs(w, logs, "archive")
			return
		}
	}

	// 全空：返回友好错误
	JSONError(w, 40400, "该 pod 暂无可用日志（容器从未输出 / 未归档 / 实时拉失败）")
}

// tryArchive 尝试从 MinIO 拿归档日志；找不到 / MinIO 没配 / 读取失败都返回 ok=false
func tryArchive(ctx context.Context, depID int64, app, pod string, kind services.LogKind) (string, bool) {
	objKey, ok, err := queryArchivedLog(depID, app, pod, kind)
	if err != nil || !ok {
		return "", false
	}
	mc, _ := loadMinIOClientFromDB()
	if mc == nil {
		return "", false
	}
	tctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	logs, err := mc.GetLog(tctx, objKey)
	if err != nil || logs == "" {
		return "", false
	}
	return logs, true
}

// HandleGetDeploymentPodEvents GET /api/deployments/{id}/pod-events
//
//	?app=...&pod=...&namespace=...&pod_uid=...
//	返回 {"events": [{type,reason,message,count,first_at,last_at,field_path}], "source": "archive"|"live"}
//
//	归档优先（events.json 是 deploy 时刻的快照），归档没有 / 缺 pod_uid 退到 live ArgoCD events 接口。
func HandleGetDeploymentPodEvents(w http.ResponseWriter, r *http.Request) {
	depID := ParseID(mux.Vars(r)["id"])
	q := r.URL.Query()
	app := q.Get("app")
	pod := q.Get("pod")
	namespace := q.Get("namespace")
	podUID := q.Get("pod_uid")
	if app == "" || pod == "" {
		JSONError(w, 40000, "app / pod 必填")
		return
	}
	p, err := loadProjectEnvByDeployment(depID)
	if err != nil {
		JSONError(w, 40400, "deployment 不存在")
		return
	}
	if !IsEnvIDAllowed(r, p.ID) {
		JSONError(w, 40300, "无权访问该环境")
		return
	}

	// 1. 归档优先
	if raw, ok := tryArchive(r.Context(), depID, app, pod, services.LogKindEvents); ok {
		var events []services.PodEvent
		if jerr := jsonUnmarshalImpl([]byte(raw), &events); jerr == nil {
			respondEvents(w, events, "archive")
			return
		}
	}

	// 2. live ArgoCD（需要 pod_uid 才能精确过滤；缺则返回空）
	if podUID == "" {
		respondEvents(w, []services.PodEvent{}, "live")
		return
	}
	argoURL, argoToken, err := ResolveArgocdForEnv(p)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	client := services.NewArgocdClient(argoURL, argoToken)
	events, err := client.GetPodEvents(r.Context(), app, podUID, namespace, pod)
	if err != nil {
		JSONError(w, 50000, "拉取事件失败 · "+err.Error())
		return
	}
	respondEvents(w, events, "live")
}

func respondEvents(w http.ResponseWriter, events []services.PodEvent, source string) {
	if events == nil {
		events = []services.PodEvent{}
	}
	JSONSuccess(w, map[string]interface{}{
		"events": events,
		"count":  len(events),
		"source": source,
	})
}

// HandleGetDeploymentArchivedPods GET /api/deployments/{id}/archived-pods?app=
//
//	列出某次 deployment 已归档的 pod。前端「查看日志」打开弹窗时，
//	先调这个，归档列表不为空就直接显示归档（pod 可能已被 k8s 收走，
//	实时模式拿不到了）。
func HandleGetDeploymentArchivedPods(w http.ResponseWriter, r *http.Request) {
	depID := ParseID(mux.Vars(r)["id"])
	app := r.URL.Query().Get("app")
	p, err := loadProjectEnvByDeployment(depID)
	if err != nil {
		JSONError(w, 40400, "deployment 不存在")
		return
	}
	if !IsEnvIDAllowed(r, p.ID) {
		JSONError(w, 40300, "无权访问该环境")
		return
	}
	list, err := queryArchivedLogsByDeployment(depID, app)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	JSONSuccess(w, map[string]interface{}{"pods": list})
}

func respondPodLogs(w http.ResponseWriter, logs, source string) {
	lines := 0
	for i := 0; i < len(logs); i++ {
		if logs[i] == '\n' {
			lines++
		}
	}
	JSONSuccess(w, map[string]interface{}{
		"logs":   logs,
		"lines":  lines,
		"source": source, // "archive" | "live"
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
