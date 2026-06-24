package handlers

import (
	"database/sql"
	"net/http"
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
}

func (h *SchedHandler) List(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT task_key, name, enabled, schedule, last_run_at, last_result FROM scheduled_tasks ORDER BY task_key`)
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
		NextRunAt  string `json:"next_run_at"`
	}
	out := []taskOut{}
	now := time.Now()
	for rows.Next() {
		var t taskOut
		var lastRun sql.NullTime
		if rows.Scan(&t.Key, &t.Name, &t.Enabled, &t.Schedule, &lastRun, &t.LastResult) != nil {
			continue
		}
		if lastRun.Valid {
			t.LastRunAt = lastRun.Time.Format("2006-01-02 15:04")
		}
		if t.Enabled == 1 {
			if sc, err := cron.ParseStandard(t.Schedule); err == nil {
				t.NextRunAt = sc.Next(now).Format("2006-01-02 15:04")
			}
		}
		out = append(out, t)
	}
	c.JSON(http.StatusOK, out)
}

func (h *SchedHandler) Update(c *gin.Context) {
	key := c.Param("key")
	var in struct {
		Enabled  *int    `json:"enabled"`
		Schedule *string `json:"schedule"`
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
