package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"ops-cmdb-backend/internal/httpx"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"

	"ops-cmdb-backend/logx"
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
		httpx.Invalid(c, "id", "number")
		return
	}
	if sched.CancelTask(id) {
		c.JSON(200, gin.H{"ok": true})
	} else {
		// 中文那句留给 MCP / 直接调 API 的人；界面读 msg_key（OPSCMDB-054）
		c.JSON(200, gin.H{"ok": false,
			"msg_key": "tasks.notCancellable",
			"msg":     "该记录不是运行中或已结束"})
	}
}

// RetryFailures 只重试某次执行的失败项（读该记录 failures 的 target，生成一条 trigger=retry 的新记录）。
func (h *SchedHandler) RetryFailures(c *gin.Context) {
	id := c.Param("id")
	var taskKey string
	var failJSON sql.NullString
	if h.DB.QueryRow(`SELECT task_key, failures FROM task_run_logs WHERE id=?`, id).Scan(&taskKey, &failJSON) != nil {
		httpx.NotFound(c, "task_run")
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
		httpx.FailKey(c, httpx.CodeBadRequest, "error.nothingToRetry", nil, nil)
		return
	}
	if !RunTaskRetry(taskKey, targets) {
		httpx.FailKey(c, httpx.CodeBadRequest, "error.taskNotRetryable", nil, nil)
		return
	}
	SetAuditTarget(c, taskKey)
	c.JSON(200, gin.H{"ok": true,
		"msg_key":    "tasks.retryTriggered",
		"msg_params": map[string]any{"count": len(targets)},
		"msg":        fmt.Sprintf("已触发重试 %d 项，稍后刷新看新记录", len(targets))})
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

	rows, err := h.DB.Query(`SELECT id, task_key, name, status, summary, failures, COALESCE(findings,''), trigger_by, duration_ms,
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
		Findings    []TaskFinding `json:"findings"`
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
		var findJSON string
		var started, fin sql.NullTime
		if rows.Scan(&o.ID, &o.TaskKey, &o.Name, &o.Status, &o.Summary, &failJSON, &findJSON, &o.TriggerBy, &o.DurationMs,
			&o.NotifyState, &o.NotifyGroup, &o.NotifyAt, &o.Progress, &started, &fin) != nil {
			continue
		}
		o.Failures = []TaskFailure{}
		if failJSON.Valid && failJSON.String != "" {
			_ = json.Unmarshal([]byte(failJSON.String), &o.Failures) // 老格式(字符串数组)会解析失败→空，可接受
		}
		o.Findings = []TaskFinding{}
		if findJSON != "" {
			_ = json.Unmarshal([]byte(findJSON), &o.Findings)
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
	// "上次跑成什么样"一律从 **task_run_logs** 推导，不读 scheduled_tasks 上那几个反规范化列。
	//
	// ⚠️ 那几列会漏写：实测 disk_watch 在 task_run_logs 里有失败记录，
	// 而 scheduled_tasks.last_run_at 仍然是 NULL —— 于是
	//   · last_ok 默认 1 → 界面显示"正常"（旧版就是这么显示的）
	//   · 或者按 last_run_at 判 → 界面显示"从没跑过"
	// 两个都是错的，而且都偏向"没问题"。一个正在失败的巡检任务
	// 被显示成从没跑过或正常，等于把故障藏起来。
	//
	// 执行流水是**事实**，那几列是缓存。判断一律以事实为准。
	rows, err := h.DB.Query(`SELECT s.task_key, s.name, s.enabled, s.schedule,
		r.started_at,
		COALESCE(r.summary, ''),
		CASE WHEN r.status IS NULL THEN 1 WHEN r.status IN (?, ?) THEN 1 ELSE 0 END,
		s.notify_enabled, s.lark_group_id, s.notify_when
		FROM scheduled_tasks s
		LEFT JOIN (
			SELECT t1.task_key, t1.started_at, t1.summary, t1.status
			FROM task_run_logs t1
			JOIN (SELECT task_key, MAX(id) AS mid FROM task_run_logs GROUP BY task_key) t2
			  ON t1.id = t2.mid
		) r ON r.task_key = s.task_key
		ORDER BY s.task_key`,
		// ⚠️ 状态值一律用常量，别手拼。两套值并存栽过一次（CMDB-20260806-002）：
		// 手动触发的记录状态写歪，「执行记录」筛「成功」时整批看不到。
		// partial 也算成功：有失败明细但整体可用，标红会让人以为任务挂了
		taskStatusOK, taskStatusPartial)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	type taskOut struct {
		Key        string `json:"task_key"`
		Name       string `json:"name"`
		Enabled    int    `json:"enabled"`
		Schedule   string `json:"schedule"`
		LastRunAt  string `json:"last_run_at"`
		LastResult string `json:"last_result"`
		LastOk     int    `json:"last_ok"`
		NextRunAt  string `json:"next_run_at"`
		// ScheduleErr 非空 = 这个任务**根本没被注册**（cron 表达式无效），
		// 一次都不会跑。必须和"还没到时间"分开：两者的 next_run_at 都是空，
		// 前端一律显示 `—`，registrar_expiry_sync 就这么静默躺了好几天。
		ScheduleErr   string `json:"schedule_err,omitempty"`
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
		// ⚠️ 扫描失败**必须留下日志**。原来这里是静默 continue，
		// 于是我改坏查询（返回字符串而扫描期望 time）之后，接口回的是
		// HTTP 200 + 空数组 —— 界面显示"没有任何定时任务"，
		// 完全看不出是查询坏了。空列表是这套系统里最会骗人的一种返回值。
		if err := rows.Scan(&t.Key, &t.Name, &t.Enabled, &t.Schedule, &lastRun, &t.LastResult, &t.LastOk,
			&t.NotifyEnabled, &groupID, &t.NotifyWhen); err != nil {
			logx.J("sched", "scan_failed", map[string]any{
				"err": err.Error(), "note": "该行被跳过，列表会少一条且不报错",
			})
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
			// 解析失败不能静默：这里用的解析器和调度器注册时是同一个，
			// 这里解析不了 = 调度器那边也注册不了 = 任务一次都不会跑。
			sc, err := cron.ParseStandard(t.Schedule)
			if err != nil {
				t.ScheduleErr = fmt.Sprintf("cron 表达式无效：%v", err)
			} else {
				t.NextRunAt = sc.Next(now).Format("2006-01-02 15:04")
			}
		}
		// 调度器侧的注册结果优先（它才是真正决定跑不跑的那一方）
		if e := ScheduleErrOf(t.Key); e != "" {
			t.ScheduleErr = "未被调度器注册：" + e
			t.NextRunAt = ""
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
			httpx.Invalid(c, "cron", "min hour day month weekday")
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
	// 🔴 下面这几条原来把错误**全丢掉**（`_, _ = h.DB.Exec(...)`），
	//	最后照样 `c.JSON(200, {"ok": true})`。
	//	于是"保存成功"了但没存上 —— 界面显示新值（因为它显示的是你刚填的），
	//	刷新之后又变回去，而没有任何一处说过失败。
	//	schedule 和 enabled 两条一直是检查错误的，后加的这四条漏了。
	//	这是本仓最不能接受的形态：出错时必须看起来像出错。
	set := func(sql string, args ...any) bool {
		if _, err := h.DB.Exec(sql, args...); err != nil {
			logx.J("sched", "task_update_failed", map[string]any{
				"task": key, "sql": sql, "err": err.Error(),
			})
			c.JSON(500, gin.H{"error": err.Error()})
			return false
		}
		return true
	}
	if in.NotifyEnabled != nil {
		if !set(`UPDATE scheduled_tasks SET notify_enabled=? WHERE task_key=?`, *in.NotifyEnabled, key) {
			return
		}
	}
	if in.LarkGroupID != nil {
		if !set(`UPDATE scheduled_tasks SET lark_group_id=? WHERE task_key=?`, nullableInt(in.LarkGroupID), key) {
			return
		}
	}
	if in.NotifyWhen != nil {
		if !set(`UPDATE scheduled_tasks SET notify_when=? WHERE task_key=?`, *in.NotifyWhen, key) {
			return
		}
	}
	if in.AtUserIDs != nil {
		// @人是「先删后插」，两步必须在**同一个事务**里。
		// ⚠️ 原来两步都不查错：删成功、插失败的话，@人列表会被清空而界面报成功 ——
		//	下次任务失败时不会 @ 任何人，而没有人知道这件事发生过。
		tx, err := h.DB.Begin()
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		if _, err := tx.Exec(`DELETE FROM task_notify_users WHERE task_key=?`, key); err != nil {
			_ = tx.Rollback()
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		for _, uid := range *in.AtUserIDs {
			if _, err := tx.Exec(`INSERT IGNORE INTO task_notify_users (task_key, user_id) VALUES (?, ?)`, key, uid); err != nil {
				_ = tx.Rollback()
				c.JSON(500, gin.H{"error": err.Error()})
				return
			}
		}
		if err := tx.Commit(); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
	}
	ReloadScheduler()
	SetAuditTarget(c, key)
	c.JSON(200, gin.H{"ok": true})
}

func (h *SchedHandler) Run(c *gin.Context) {
	key := c.Param("key")
	if !RunTaskNow(key) {
		httpx.NotFound(c, "task")
		return
	}
	SetAuditTarget(c, key)
	c.JSON(200, gin.H{"ok": true,
		"msg_key": "tasks.runTriggered",
		"msg":     "已触发，稍后刷新看结果"})
}
