package handlers

import (
	"context"
	"database/sql"
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

// archiveFailedPodLogsAsync 在后台 goroutine 里拉日志 + 上传 + 写表。
//
// 调用方应在 deploy/restart 完成后（拿到 ArgocdResults）对每个失败 app 调一次。
// 内部完全自包含：自己开 ctx、自己处理错误（只 log），不影响调用方。
func archiveFailedPodLogsAsync(
	deploymentID int64,
	p *models.ProjectEnv,
	failedApps []string,
) {
	if len(failedApps) == 0 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		mc, err := loadMinIOClientFromDB()
		if err != nil {
			log.Printf("[log-archive] load minio config: %v (skip)", err)
			return
		}
		if mc == nil {
			return // 未配置 MinIO，静默跳过
		}
		// 确保 bucket + lifecycle（每次都 idempotent，开销可忽略）
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

		for _, app := range failedApps {
			if err := archiveOneApp(ctx, deploymentID, p, app, argoClient, mc); err != nil {
				log.Printf("[log-archive] dep=%d app=%s failed: %v", deploymentID, app, err)
			}
		}
	}()
}

// archiveOneApp 处理单个失败 app：找出 unhealthy pod → 拉日志 → 上传 → 写表
func archiveOneApp(
	ctx context.Context,
	deploymentID int64,
	p *models.ProjectEnv,
	app string,
	argoClient *services.ArgocdClient,
	mc *services.MinIOClient,
) error {
	// 1. 找出该 app 的 unhealthy pods
	nodes, err := argoClient.GetAppResourceTree(ctx, app)
	if err != nil {
		return err
	}
	failingPods := []services.ResourceNode{}
	for _, n := range nodes {
		if n.Kind == "Pod" && n.Health != "" && n.Health != "Healthy" {
			failingPods = append(failingPods, n)
		}
	}
	if len(failingPods) == 0 {
		return nil // 没有 unhealthy pod 不归档
	}

	// 2. 每个失败 pod 拉一次日志
	for _, pod := range failingPods {
		// 优先 previous（上一次崩溃前）；previous 拿不到（首次部署）退到 current
		logs, err := argoClient.GetPodLogs(ctx, app, pod.Namespace, pod.Name, "", 2000, true)
		if err != nil || logs == "" {
			logs, _ = argoClient.GetPodLogs(ctx, app, pod.Namespace, pod.Name, "", 2000, false)
		}
		if logs == "" {
			continue
		}

		// 3. 上传 MinIO
		objectKey := services.LogObjectKey(deploymentID, app, pod.Name)
		size, err := mc.PutLog(ctx, objectKey, logs)
		if err != nil {
			log.Printf("[log-archive] put %s: %v", objectKey, err)
			continue
		}

		// 4. 写入 DB（INSERT IGNORE 防重复——同一 pod 多次失败时保留最新）
		_, _ = database.DB.Exec(`
			INSERT INTO deployment_pod_logs (deployment_id, argocd_app, pod_name, object_key, size_bytes)
			VALUES (?, ?, ?, ?, ?)`,
			deploymentID, app, pod.Name, objectKey, size)
		log.Printf("[log-archive] archived dep=%d app=%s pod=%s size=%d", deploymentID, app, pod.Name, size)
	}
	return nil
}

// queryArchivedLog 按 deployment + pod + app 找归档日志的 object_key
// 返回 (objectKey, found, error)
func queryArchivedLog(deploymentID int64, app, pod string) (string, bool, error) {
	var objectKey string
	q := `SELECT object_key FROM deployment_pod_logs
		WHERE deployment_id=? AND argocd_app=? AND pod_name=?
		ORDER BY captured_at DESC LIMIT 1`
	err := database.DB.QueryRow(q, deploymentID, app, pod).Scan(&objectKey)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return objectKey, true, nil
}

// queryArchivedLogsByDeployment 列出某 deployment 已归档的所有 pod
// 用于前端弹窗在 argocd 实时拿不到 pod 时回退到归档列表
type archivedLog struct {
	App       string `json:"app"`
	Pod       string `json:"pod"`
	ObjectKey string `json:"-"`
	Size      int64  `json:"size_bytes"`
	At        string `json:"captured_at"`
}

func queryArchivedLogsByDeployment(deploymentID int64, app string) ([]archivedLog, error) {
	q := `SELECT argocd_app, pod_name, object_key, size_bytes, captured_at
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
		var t time.Time
		_ = rows.Scan(&r.App, &r.Pod, &r.ObjectKey, &r.Size, &t)
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
