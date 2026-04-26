package handlers

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"opsplatform-deploy-backend/database"
)

// =========================================================================
//   发布历史自动清理
//
// 1. 每天凌晨 2:00 自动清理 N 天前的 deployment + deployment_pod_logs
//    （保留天数在 global_config.history_retention_days，默认 180）
//    minio 那边的实际日志文件靠原 lifecycle 自动过期，匹配上即可
// 2. admin 提供「立即清理」按钮：选 env + days，预览条数 + 删除
// =========================================================================

// loadHistoryRetentionDays 读保留天数，未配置或 0 返回默认 180
func loadHistoryRetentionDays() int {
	var days int
	_ = database.DB.QueryRow(`SELECT IFNULL(history_retention_days, 180) FROM global_config WHERE id=1`).Scan(&days)
	if days <= 0 {
		days = 180
	}
	return days
}

// cleanupOldDeployments 删 N 天前的 deployment + 关联 deployment_pod_logs
// envNames 为空 / nil = 所有环境；非空 = 仅清这些 env
// 返回删除条数
func cleanupOldDeployments(ctx context.Context, days int, envNames []string) (int64, error) {
	if days <= 0 {
		return 0, nil
	}
	tx, err := database.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	// 找出待删的 deployment id 集合（按时间 + 可选 env 过滤）
	q := `SELECT d.id FROM deployment d`
	args := []interface{}{}
	conds := []string{"d.created_at < DATE_SUB(NOW(), INTERVAL ? DAY)"}
	args = append(args, days)
	if len(envNames) > 0 {
		q += ` INNER JOIN project_env pe ON pe.id = d.project_env_id`
		ph := strings.Repeat("?,", len(envNames))
		ph = ph[:len(ph)-1]
		conds = append(conds, "pe.name IN ("+ph+")")
		for _, n := range envNames {
			args = append(args, n)
		}
	}
	q += ` WHERE ` + strings.Join(conds, " AND ")
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	if len(ids) == 0 {
		return 0, nil
	}

	// 批量删 deployment_pod_logs（外键关系，先删子表）
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	delArgs := make([]interface{}, len(ids))
	for i, id := range ids {
		delArgs[i] = id
	}
	_, err = tx.ExecContext(ctx,
		`DELETE FROM deployment_pod_logs WHERE deployment_id IN (`+placeholders+`)`, delArgs...)
	if err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx,
		`DELETE FROM deployment WHERE id IN (`+placeholders+`)`, delArgs...)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()

	// 记录上次清理时间
	_, _ = database.DB.Exec(`UPDATE global_config SET last_history_cleanup_at=NOW() WHERE id=1`)
	return n, nil
}

// previewCleanupCount 预览将删除多少条
func previewCleanupCount(ctx context.Context, days int, envNames []string) (int64, error) {
	q := `SELECT COUNT(*) FROM deployment d`
	args := []interface{}{}
	conds := []string{"d.created_at < DATE_SUB(NOW(), INTERVAL ? DAY)"}
	args = append(args, days)
	if len(envNames) > 0 {
		q += ` INNER JOIN project_env pe ON pe.id = d.project_env_id`
		ph := strings.Repeat("?,", len(envNames))
		ph = ph[:len(ph)-1]
		conds = append(conds, "pe.name IN ("+ph+")")
		for _, n := range envNames {
			args = append(args, n)
		}
	}
	q += ` WHERE ` + strings.Join(conds, " AND ")
	var cnt int64
	err := database.DB.QueryRowContext(ctx, q, args...).Scan(&cnt)
	return cnt, err
}

// StartHistoryCleanupCron 启动定时清理（每天凌晨 2:00 跑一次）
// 在 main.go 启动时调一次即可
func StartHistoryCleanupCron() {
	go func() {
		for {
			next := nextCleanupTime()
			d := time.Until(next)
			log.Printf("[history-cleanup] next run at %s (in %s)", next.Format(time.RFC3339), d)
			select {
			case <-time.After(d):
			}
			runDailyCleanup()
		}
	}()
}

// nextCleanupTime 下一个凌晨 2:00 的时刻
func nextCleanupTime() time.Time {
	now := time.Now()
	next := time.Date(now.Year(), now.Month(), now.Day(), 2, 0, 0, 0, now.Location())
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return next
}

func runDailyCleanup() {
	days := loadHistoryRetentionDays()
	if days <= 0 {
		log.Printf("[history-cleanup] retention disabled, skip")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	n, err := cleanupOldDeployments(ctx, days, nil)
	if err != nil {
		log.Printf("[history-cleanup] failed: %v", err)
		return
	}
	log.Printf("[history-cleanup] deleted %d deployments older than %d days", n, days)
}

// HandlePreviewHistoryCleanup GET /api/history-cleanup/preview?days=N&envs=g32-uat,g50-uat
//
//	envs 可空 = 所有环境；多个用逗号分隔
func HandlePreviewHistoryCleanup(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days <= 0 {
		days = loadHistoryRetentionDays()
	}
	envNames := parseEnvList(r.URL.Query().Get("envs"))
	cnt, err := previewCleanupCount(r.Context(), days, envNames)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	JSONSuccess(w, map[string]interface{}{
		"days":  days,
		"envs":  envNames,
		"count": cnt,
	})
}

// HandleRunHistoryCleanup POST /api/history-cleanup
//
//	body: {"days": 90, "env_names": ["g32-uat", "g50-uat"]}  env_names 可空 = 所有环境
func HandleRunHistoryCleanup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Days     int      `json:"days"`
		EnvNames []string `json:"env_names"`
	}
	if !DecodeJSON(w, r, &req) {
		return
	}
	if req.Days <= 0 {
		req.Days = loadHistoryRetentionDays()
	}
	if req.Days < 1 || req.Days > 3650 {
		JSONError(w, 40001, "days 必须 1~3650")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	n, err := cleanupOldDeployments(ctx, req.Days, req.EnvNames)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	Audit(r, "history.cleanup", "deployment", "",
		map[string]interface{}{"days": req.Days, "envs": req.EnvNames, "deleted": n})
	JSONSuccess(w, map[string]interface{}{"deleted": n})
}

// parseEnvList "g32-uat,g50-uat" → ["g32-uat", "g50-uat"]，空返回 nil
func parseEnvList(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// 让 sql.ErrNoRows 编译可见（虽然此文件没用到但避免风格警告）
var _ = sql.ErrNoRows
