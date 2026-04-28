package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"opsplatform-deploy-backend/crypto"
	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/models"
	"opsplatform-deploy-backend/services"
)

// =========================================================================
//   失败 pod 日志归档器
//
// 在 deploy / restart / rollback 调用结束后，异步对每个失败模块拉一次
// 「上一次崩溃前」日志（最多 2000 行）→ 上传 MinIO → 写表 deployment_pod_logs。
//
// 优雅降级：MinIO 未配置时整个流程跳过，不影响发布主流程。
//
// 时机考虑：deploy 完成 → poll timeout/Degraded → 此时 pod 通常正在 crash-loop
// 或卡在 Pending，「kubectl logs --previous」能拿到最后一次失败容器的输出。
// =========================================================================

// loadMinIOClientFromDB 从 global_config 加载 MinIO 配置，返回 client。
// 凭证缺失或解密失败时返回 nil（调用方按"未配置"处理）。
func loadMinIOClientFromDB() (*services.MinIOClient, error) {
	var endpoint, bucket, accessKey, encSecret, region string
	var retentionDays int
	err := database.DB.QueryRow(`SELECT
		IFNULL(minio_endpoint,''), IFNULL(minio_bucket,''),
		IFNULL(minio_access_key,''), IFNULL(minio_secret_key,''),
		IFNULL(minio_region,'us-east-1'), IFNULL(minio_retention_days, 90)
		FROM global_config WHERE id=1`).
		Scan(&endpoint, &bucket, &accessKey, &encSecret, &region, &retentionDays)
	if err != nil {
		return nil, err
	}
	if endpoint == "" || bucket == "" || accessKey == "" || encSecret == "" {
		return nil, nil // 未配置
	}
	secret, err := crypto.Decrypt(encSecret)
	if err != nil {
		return nil, err
	}
	return services.NewMinIOClient(services.MinIOConfig{
		Endpoint:      endpoint,
		Bucket:        bucket,
		AccessKey:     accessKey,
		SecretKey:     secret,
		Region:        region,
		RetentionDays: retentionDays,
	})
}

// ArchiveFailedPodLogs 同步归档：在 deploy goroutine 返回前完成。
//
//	关键改动（v93）：
//	1. 同步执行 —— 不再 goroutine。保证用户下一次 deploy 一定排在 archive 之后，
//	   ReplicaSet 不会抢在 archive 前把失败 pod 滚走。
//	2. 用调用方传入的 FailingPods 快照（fail 决策瞬间抓的）—— 不再调 resource-tree
//	   二次过滤，即使 pod 已被删 k8s logs API 仍能拉到 terminated pod 的日志（~1 小时）。
//	3. 调用方从 res.ArgocdResults 收集每个失败 app 的 FailingPods 列表，按 app 分组传入。
//
//	由于同步执行，整体耗时控制在 ~10s 内（每 pod 3 次 API 拉取 + MinIO 上传）。
//	失败的 deploy 多 10s 完成对用户感知 0 影响（反正失败不在赶时间）。
func ArchiveFailedPodLogs(
	deploymentID int64,
	p *models.ProjectEnv,
	appPods map[string][]models.FailingPodSnapshot,
) {
	if len(appPods) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	mc, err := loadMinIOClientFromDB()
	if err != nil {
		log.Printf("[log-archive] load minio config: %v (skip)", err)
		return
	}
	if mc == nil {
		return
	}
	if err := mc.EnsureBucket(ctx); err != nil {
		log.Printf("[log-archive] ensure bucket: %v (skip)", err)
		return
	}

	argoURL, argoToken, err := ResolveArgocdForEnv(p)
	if err != nil {
		log.Printf("[log-archive] resolve argocd: %v (skip)", err)
		return
	}
	argoClient := services.NewArgocdClient(argoURL, argoToken)

	for app, pods := range appPods {
		for _, snap := range pods {
			archivePodThreeKinds(ctx, deploymentID, app, snap, argoClient, mc)
		}
	}
}

// archivePodThreeKinds 对单个 pod 拉三种归档并写表（用 FailingPods 快照里的 name/uid/ns）
func archivePodThreeKinds(
	ctx context.Context,
	deploymentID int64,
	app string,
	pod models.FailingPodSnapshot,
	argoClient *services.ArgocdClient,
	mc *services.MinIOClient,
) {
	type kindResult struct {
		key  string
		size int64
	}
	out := map[services.LogKind]kindResult{}

	// previous.log（最关键 —— 崩溃前最后输出）
	if logs, _ := argoClient.GetPodLogs(ctx, app, pod.Namespace, pod.Name, "", 2000, true); logs != "" {
		key := services.LogObjectKey(deploymentID, app, pod.Name, services.LogKindPrevious)
		if size, err := mc.PutLog(ctx, key, logs); err == nil {
			out[services.LogKindPrevious] = kindResult{key, size}
		} else {
			log.Printf("[log-archive] put previous %s: %v", key, err)
		}
	}

	// current.log（200 行兜底）
	if logs, _ := argoClient.GetPodLogs(ctx, app, pod.Namespace, pod.Name, "", 200, false); logs != "" {
		key := services.LogObjectKey(deploymentID, app, pod.Name, services.LogKindCurrent)
		if size, err := mc.PutLog(ctx, key, logs); err == nil {
			out[services.LogKindCurrent] = kindResult{key, size}
		} else {
			log.Printf("[log-archive] put current %s: %v", key, err)
		}
	}

	// events.json（FailedScheduling / ImagePullBackOff / FailedMount 等）
	// 注意：UID 来自 fail 决策瞬间的快照，即便 pod 已被 ReplicaSet 删，UID 仍能让 ArgoCD/k8s
	// 按 involvedObject.uid 索引到 events（events 自身有 ~1 小时保留）
	if events, err := argoClient.GetPodEvents(ctx, app, pod.UID, pod.Namespace, pod.Name); err == nil && len(events) > 0 {
		if data, jerr := json.Marshal(events); jerr == nil {
			key := services.LogObjectKey(deploymentID, app, pod.Name, services.LogKindEvents)
			if size, perr := mc.PutLog(ctx, key, string(data)); perr == nil {
				out[services.LogKindEvents] = kindResult{key, size}
			} else {
				log.Printf("[log-archive] put events %s: %v", key, perr)
			}
		}
	}

	if len(out) == 0 {
		return // 三种全空，不留 DB 行（避免前端打开看到空 tab 集）
	}

	// ON DUPLICATE KEY UPDATE：同 pod 多次归档时让最新内容覆盖旧的
	// COALESCE(VALUES(x), x)：本次没拉到 (NULL) 就保留上次的值
	// （unique key = (deployment_id, argocd_app, pod_name)）
	prev := out[services.LogKindPrevious]
	curr := out[services.LogKindCurrent]
	evts := out[services.LogKindEvents]
	_, err := database.DB.Exec(`
		INSERT INTO deployment_pod_logs
		  (deployment_id, argocd_app, pod_name,
		   previous_key, previous_size, current_key, current_size, events_key, events_size)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
		  previous_key  = COALESCE(VALUES(previous_key),  previous_key),
		  previous_size = IF(VALUES(previous_key) IS NULL, previous_size, VALUES(previous_size)),
		  current_key   = COALESCE(VALUES(current_key),   current_key),
		  current_size  = IF(VALUES(current_key)  IS NULL, current_size,  VALUES(current_size)),
		  events_key    = COALESCE(VALUES(events_key),    events_key),
		  events_size   = IF(VALUES(events_key)   IS NULL, events_size,   VALUES(events_size)),
		  captured_at   = CURRENT_TIMESTAMP`,
		deploymentID, app, pod.Name,
		nullableStr(prev.key), prev.size,
		nullableStr(curr.key), curr.size,
		nullableStr(evts.key), evts.size,
	)
	if err != nil {
		log.Printf("[log-archive] upsert dep=%d app=%s pod=%s: %v", deploymentID, app, pod.Name, err)
		return
	}
	log.Printf("[log-archive] archived dep=%d app=%s pod=%s prev=%dB curr=%dB events=%dB",
		deploymentID, app, pod.Name, prev.size, curr.size, evts.size)
}

// nullableStr 空字符串转 NULL（让 COALESCE 把"本次没拉到"的字段保留旧值）
func nullableStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// queryArchivedLog 按 deployment + pod + app + kind 找归档日志的 object_key
//
//	kind ∈ previous / current / events
//	返回 (objectKey, found, error)；该 kind 对应列 NULL 时返回 found=false
func queryArchivedLog(deploymentID int64, app, pod string, kind services.LogKind) (string, bool, error) {
	column := ""
	switch kind {
	case services.LogKindPrevious:
		column = "previous_key"
	case services.LogKindCurrent:
		column = "current_key"
	case services.LogKindEvents:
		column = "events_key"
	default:
		return "", false, nil
	}
	var objectKey sql.NullString
	q := `SELECT ` + column + ` FROM deployment_pod_logs
		WHERE deployment_id=? AND argocd_app=? AND pod_name=?
		ORDER BY captured_at DESC LIMIT 1`
	err := database.DB.QueryRow(q, deploymentID, app, pod).Scan(&objectKey)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if !objectKey.Valid || objectKey.String == "" {
		return "", false, nil
	}
	return objectKey.String, true, nil
}

// archivedLog 列表项；HasXxx 三个 flag 让前端打开弹窗时知道哪些 tab 有内容
type archivedLog struct {
	App         string `json:"app"`
	Pod         string `json:"pod"`
	HasPrevious bool   `json:"has_previous"`
	HasCurrent  bool   `json:"has_current"`
	HasEvents   bool   `json:"has_events"`
	At          string `json:"captured_at"`
}

func queryArchivedLogsByDeployment(deploymentID int64, app string) ([]archivedLog, error) {
	q := `SELECT argocd_app, pod_name, previous_key, current_key, events_key, captured_at
		FROM deployment_pod_logs WHERE deployment_id=?`
	args := []interface{}{deploymentID}
	if app != "" {
		q += ` AND argocd_app=?`
		args = append(args, app)
	}
	q += ` ORDER BY captured_at DESC`
	rows, err := database.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []archivedLog{}
	for rows.Next() {
		var r archivedLog
		var prev, curr, evts sql.NullString
		var t time.Time
		_ = rows.Scan(&r.App, &r.Pod, &prev, &curr, &evts, &t)
		r.HasPrevious = prev.Valid && prev.String != ""
		r.HasCurrent = curr.Valid && curr.String != ""
		r.HasEvents = evts.Valid && evts.String != ""
		r.At = t.Format("2006-01-02 15:04:05")
		out = append(out, r)
	}
	return out, nil
}

// ---- 测试 MinIO 连接（系统设置「测试连接」按钮用） ----

func HandleTestMinIO(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Endpoint  string `json:"minio_endpoint"`
		Bucket    string `json:"minio_bucket"`
		AccessKey string `json:"minio_access_key"`
		SecretKey string `json:"minio_secret_key"`
		Region    string `json:"minio_region"`
	}
	if !DecodeJSON(w, r, &req) {
		return
	}
	// 任一字段空就 fallback DB（admin 改时密钥可能不传）
	if req.Endpoint == "" || req.Bucket == "" || req.AccessKey == "" || req.SecretKey == "" {
		var encSecret string
		_ = database.DB.QueryRow(`SELECT
			IFNULL(minio_endpoint,''), IFNULL(minio_bucket,''),
			IFNULL(minio_access_key,''), IFNULL(minio_secret_key,''),
			IFNULL(minio_region,'us-east-1')
			FROM global_config WHERE id=1`).
			Scan(&req.Endpoint, &req.Bucket, &req.AccessKey, &encSecret, &req.Region)
		if req.SecretKey == "" {
			dec, _ := crypto.Decrypt(encSecret)
			req.SecretKey = dec
		}
	}
	mc, err := services.NewMinIOClient(services.MinIOConfig{
		Endpoint: req.Endpoint, Bucket: req.Bucket,
		AccessKey: req.AccessKey, SecretKey: req.SecretKey,
		Region: req.Region, RetentionDays: 90,
	})
	if err != nil {
		JSONError(w, 40001, "配置无效："+err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	if err := mc.TestConnection(ctx); err != nil {
		JSONError(w, 50001, "连接失败："+err.Error())
		return
	}
	if err := mc.EnsureBucket(ctx); err != nil {
		JSONError(w, 50001, "创建/检查 bucket 失败："+err.Error())
		return
	}
	JSONSuccess(w, map[string]interface{}{"ok": true})
}

// ApplyMinIOLifecycleNow 系统设置改保留天数后调一次，让新规则立即生效
// （admin 改了 90→30 → 立刻把 bucket lifecycle 改成 30 天）
func ApplyMinIOLifecycleNow() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		mc, err := loadMinIOClientFromDB()
		if err != nil || mc == nil {
			return
		}
		if err := mc.EnsureBucket(ctx); err != nil {
			log.Printf("[minio-lifecycle] update failed: %v", err)
		}
	}()
}
