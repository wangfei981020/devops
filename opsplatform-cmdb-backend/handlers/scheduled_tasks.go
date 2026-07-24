package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"
)

// SchedHandler 定时任务配置 API（前端定时任务页用）。
type SchedHandler struct{ DB *sql.DB }

func NewSchedHandler(db *sql.DB) *SchedHandler { return &SchedHandler{DB: db} }

func (h *SchedHandler) Register(r *gin.RouterGroup) {
	r.GET("/scheduled-tasks", h.List)
	r.PUT("/scheduled-tasks/:key", h.Update)
	r.POST("/scheduled-tasks/:key/run", h.Run)
	r.GET("/task-runs", h.RunLogs)
	r.POST("/task-runs/:id/retry-failures", h.RetryFailures)
	r.POST("/task-runs/:id/cancel", h.Cancel) // 取消运行中的任务（中止 + 收尾）
}

// Cancel 取消一条运行中的执行记录：中止其 ctx（正常收尾为已取消）或强制收尾僵尸记录。
func (h *SchedHandler) Cancel(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if sched == nil || id <= 0 {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	if sched.CancelTask(id) {
		c.JSON(200, gin.H{"ok": true})
	} else {
		c.JSON(200, gin.H{"ok": false, "msg": "该记录不是运行中或已结束"})
	}
}

// RetryFailures 只重试某次执行的失败项（读该记录 failures 的 target，生成一条 trigger=retry 的新记录）。
func (h *SchedHandler) RetryFailures(c *gin.Context) {
	id := c.Param("id")
	var taskKey string
	var failJSON sql.NullString
	if h.DB.QueryRow(`SELECT task_key, failures FROM task_run_logs WHERE id=?`, id).Scan(&taskKey, &failJSON) != nil {
		c.JSON(404, gin.H{"error": "执行记录不存在"})
		return
	}
	var fails []TaskFailure
	if failJSON.Valid && failJSON.String != "" {
		_ = json.Unmarshal([]byte(failJSON.String), &fails)
	}
	var targets []string
	for _, f := range fails {
		if f.Target != "" {
			targets = append(targets, f.Target)
		}
	}
	if len(targets) == 0 {
		c.JSON(400, gin.H{"error": "该记录没有可重试的失败项"})
		return
	}
	if !RunTaskRetry(taskKey, targets) {
		c.JSON(400, gin.H{"error": "该任务不支持重试"})
		return
	}
	WriteAudit(h.DB, c, "retry_task_failures", taskKey)
	c.JSON(200, gin.H{"ok": true, "msg": fmt.Sprintf("已触发重试 %d 项，稍后刷新看新记录", len(targets))})
}

// RunLogs 定时任务执行历史（执行记录页）。支持 task_key / status / days 过滤 + 分页。
func (h *SchedHandler) RunLogs(c *gin.Context) {
	where := []string{"1=1"}
	args := []any{}
	if k := c.Query("task_key"); k != "" {
		where = append(where, "task_key=?")
		args = append(args, k)
	}
	if s := c.Query("status"); s != "" {
		where = append(where, "status=?")
		args = append(args, s)
	}
	if d := c.Query("days"); d != "" {
		if n, err := strconv.Atoi(d); err == nil && n > 0 {
			where = append(where, "finished_at >= DATE_SUB(NOW(), INTERVAL ? DAY)")
			args = append(args, n)
		}
	}
	cond := strings.Join(where, " AND ")

	var total int
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM task_run_logs WHERE `+cond, args...).Scan(&total)

	limit := 20
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 200 {
		limit = l
	}
	offset := 0
	if o, err := strconv.Atoi(c.Query("offset")); err == nil && o > 0 {
		offset = o
	}

	rows, err := h.DB.Query(`SELECT id, task_key, name, status, summary, failures, trigger_by, duration_ms,
		notify_state, notify_group, notify_at, progress, started_at, finished_at
		FROM task_run_logs WHERE `+cond+` ORDER BY id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	type runOut struct {
		ID          int64         `json:"id"`
		TaskKey     string        `json:"task_key"`
		Name        string        `json:"name"`
		Status      string        `json:"status"`
		Summary     string        `json:"summary"`
		Failures    []TaskFailure `json:"failures"`
		TriggerBy   string        `json:"trigger_by"`
		DurationMs  int           `json:"duration_ms"`
		NotifyState string        `json:"notify_state"`
		NotifyGroup string        `json:"notify_group"`
		NotifyAt    string        `json:"notify_at"`
		Progress    string        `json:"progress"`
		StartedAt   string        `json:"started_at"`
		FinishedAt  string        `json:"finished_at"`
	}
	list := []runOut{}
	for rows.Next() {
		var o runOut
		var failJSON sql.NullString
		var started, fin sql.NullTime
		if rows.Scan(&o.ID, &o.TaskKey, &o.Name, &o.Status, &o.Summary, &failJSON, &o.TriggerBy, &o.DurationMs,
			&o.NotifyState, &o.NotifyGroup, &o.NotifyAt, &o.Progress, &started, &fin) != nil {
			continue
		}
		o.Failures = []TaskFailure{}
		if failJSON.Valid && failJSON.String != "" {
			_ = json.Unmarshal([]byte(failJSON.String), &o.Failures) // 老格式(字符串数组)会解析失败→空，可接受
		}
		if started.Valid {
			o.StartedAt = started.Time.Format("2006-01-02 15:04:05")
		}
		if fin.Valid && o.Status != "running" {
			o.FinishedAt = fin.Time.Format("2006-01-02 15:04:05")
		}
		list = append(list, o)
	}
	c.JSON(http.StatusOK, gin.H{"total": total, "items": list})
}

func (h *SchedHandler) List(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT task_key, name, enabled, schedule, last_run_at, last_result, last_ok,
		notify_enabled, lark_group_id, notify_when FROM scheduled_tasks ORDER BY task_key`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	type taskOut struct {
		Key           string `json:"task_key"`
		Name          string `json:"name"`
		Enabled       int    `json:"enabled"`
		Schedule      string `json:"schedule"`
		LastRunAt     string `json:"last_run_at"`
		LastResult    string `json:"last_result"`
		LastOk        int    `json:"last_ok"`
		NextRunAt     string `json:"next_run_at"`
		NotifyEnabled int    `json:"notify_enabled"`
		LarkGroupID   *int   `json:"lark_group_id"`
		NotifyWhen    string `json:"notify_when"`
		AtUserIDs     []int  `json:"at_user_ids"`
	}
	out := []taskOut{}
	now := time.Now()
	for rows.Next() {
		var t taskOut
		var lastRun sql.NullTime
		var groupID sql.NullInt64
		if rows.Scan(&t.Key, &t.Name, &t.Enabled, &t.Schedule, &lastRun, &t.LastResult, &t.LastOk,
			&t.NotifyEnabled, &groupID, &t.NotifyWhen) != nil {
			continue
		}
		if lastRun.Valid {
			t.LastRunAt = lastRun.Time.Format("2006-01-02 15:04")
		}
		if groupID.Valid {
			v := int(groupID.Int64)
			t.LarkGroupID = &v
		}
		if t.Enabled == 1 {
			if sc, err := cron.ParseStandard(t.Schedule); err == nil {
				t.NextRunAt = sc.Next(now).Format("2006-01-02 15:04")
			}
		}
		t.AtUserIDs = []int{}
		urows, _ := h.DB.Query(`SELECT user_id FROM task_notify_users WHERE task_key=?`, t.Key)
		if urows != nil {
			for urows.Next() {
				var uid int
				if urows.Scan(&uid) == nil {
					t.AtUserIDs = append(t.AtUserIDs, uid)
				}
			}
			urows.Close()
		}
		out = append(out, t)
	}
	c.JSON(http.StatusOK, out)
}

func (h *SchedHandler) Update(c *gin.Context) {
	key := c.Param("key")
	var in struct {
		Enabled       *int    `json:"enabled"`
		Schedule      *string `json:"schedule"`
		NotifyEnabled *int    `json:"notify_enabled"`
		LarkGroupID   *int    `json:"lark_group_id"`
		NotifyWhen    *string `json:"notify_when"`
		AtUserIDs     *[]int  `json:"at_user_ids"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if in.Schedule != nil {
		if _, err := cron.ParseStandard(*in.Schedule); err != nil {
			c.JSON(400, gin.H{"error": "cron 表达式格式错误（5 字段：分 时 日 月 周）"})
			return
		}
		if _, err := h.DB.Exec(`UPDATE scheduled_tasks SET schedule=? WHERE task_key=?`, *in.Schedule, key); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
	}
	if in.Enabled != nil {
		if _, err := h.DB.Exec(`UPDATE scheduled_tasks SET enabled=? WHERE task_key=?`, *in.Enabled, key); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
	}
	if in.NotifyEnabled != nil {
		_, _ = h.DB.Exec(`UPDATE scheduled_tasks SET notify_enabled=? WHERE task_key=?`, *in.NotifyEnabled, key)
	}
	if in.LarkGroupID != nil {
		_, _ = h.DB.Exec(`UPDATE scheduled_tasks SET lark_group_id=? WHERE task_key=?`, nullableInt(in.LarkGroupID), key)
	}
	if in.NotifyWhen != nil {
		_, _ = h.DB.Exec(`UPDATE scheduled_tasks SET notify_when=? WHERE task_key=?`, *in.NotifyWhen, key)
	}
	if in.AtUserIDs != nil {
		_, _ = h.DB.Exec(`DELETE FROM task_notify_users WHERE task_key=?`, key)
		for _, uid := range *in.AtUserIDs {
			_, _ = h.DB.Exec(`INSERT IGNORE INTO task_notify_users (task_key, user_id) VALUES (?, ?)`, key, uid)
		}
	}
	ReloadScheduler()
	WriteAudit(h.DB, c, "update_scheduled_task", key)
	c.JSON(200, gin.H{"ok": true})
}

func (h *SchedHandler) Run(c *gin.Context) {
	key := c.Param("key")
	if !RunTaskNow(key) {
		c.JSON(404, gin.H{"error": "未知任务"})
		return
	}
	WriteAudit(h.DB, c, "run_scheduled_task", key)
	c.JSON(200, gin.H{"ok": true, "msg": "已触发，稍后刷新看结果"})
}
