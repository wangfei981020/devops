package handlers

import (
	"database/sql"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/internal/httpx"
)

// 巡检 = 定时任务 + 最近一次执行结果。
//
// ⚠️ 这一页最重要的一件事：**「从没跑过」和「跑成功了」必须分开**。
// 一个从未执行过的任务，`last_result` 是空的 —— 空字符串很容易被当成
// "没有错误信息" 从而渲染成正常。而真相是这个任务可能压根没被调度到，
// 采集全靠它的那批数据其实一直是空的。

type TaskListHandler struct{ DB *sql.DB }

func NewTaskListHandler(db *sql.DB) *TaskListHandler { return &TaskListHandler{DB: db} }

func (h *TaskListHandler) Register(r *gin.RouterGroup) {
	r.GET("/task-list", h.List)
}

type taskOut struct {
	TaskKey  string `json:"task_key"`
	Name     string `json:"name"`
	Enabled  bool   `json:"enabled"`
	Schedule string `json:"schedule"`

	// LastRunAt 空 = 从没跑过（不是"刚跑完"）
	LastRunAt  string `json:"last_run_at,omitempty"`
	LastResult string `json:"last_result"`
	// LastOK 上一次是否成功。用指针：null = 从没跑过，无从谈成败
	LastOK *bool `json:"last_ok"`
	// LastStatus 最近一次执行流水的状态（ok/partial/fail/skipped/...）。
	// 空 = 没有流水记录。它比 last_ok 精确，taskState 优先用它
	LastStatus string `json:"last_status"`

	// Overdue 按 schedule 早该跑了却没跑。
	//
	// ⚠️ 这是"任务静默停摆"的唯一信号。调度器挂掉时任务不会报错，
	// 它只是**不再执行** —— 界面上永远显示着上一次的成功结果，
	// 而数据在慢慢变旧。
	Overdue bool `json:"overdue"`

	NotifyEnabled bool `json:"notify_enabled"`
}

// taskStaleAfter 超过这个时间没跑就算停摆。
//
// 取 26 小时：覆盖"每天一次"的任务加上时区/夏令时的偏移，
// 又不至于让一个真停了两天的任务看起来正常。
// 更精确的做法是解析 cron 表达式算下一次执行时刻 ——
// 那需要引入一个 cron 解析库，而这里的目的只是"有没有明显停摆"。
const taskStaleAfter = 26 * time.Hour

// List GET /api/task-list
//
//	@Summary		定时任务与巡检
//	@Description	含"从没跑过"与"早该跑了却没跑"两种静默失败信号。
//	@Tags			ops
//	@Produce		json
//	@Param			q		query		string	false	"按任务名搜索"
//	@Param			state	query		string	false	"状态"	Enums(all, failed, overdue, never, ok, disabled)
//	@Success		200		{object}	httpx.ListResponse[handlers.taskOut]
//	@Router			/task-list [get]
func (h *TaskListHandler) List(c *gin.Context) {
	q := httpx.BindPage(c, "state")

	// ⚠️ last_ok 只有两态（成/败），表达不了「跳过」。
	// 所以额外从执行流水里取最近一次的**真实状态** —— 这也符合
	// 「判据从流水推导，不信反规范化列」的既定做法（见 3.1 那次修复）。
	rows, err := h.DB.Query(`SELECT t.task_key, t.name, t.enabled, COALESCE(t.schedule,''),
		t.last_run_at, COALESCE(t.last_result,''), t.last_ok, t.notify_enabled,
		COALESCE((SELECT l.status FROM task_run_logs l
		           WHERE l.task_key = t.task_key
		           ORDER BY l.id DESC LIMIT 1), '') AS last_status
		FROM scheduled_tasks t ORDER BY t.name`)
	if err != nil {
		httpx.Fail(c, httpx.CodeInternal, err, nil)
		return
	}
	defer rows.Close()

	now := time.Now()
	all := []taskOut{}
	for rows.Next() {
		var o taskOut
		var enabled, notify int
		var lastRun sql.NullTime
		var lastOK sql.NullInt64
		if err := rows.Scan(&o.TaskKey, &o.Name, &enabled, &o.Schedule,
			&lastRun, &o.LastResult, &lastOK, &notify, &o.LastStatus); err != nil {
			continue
		}
		o.Enabled = enabled == 1
		o.NotifyEnabled = notify == 1
		if lastRun.Valid {
			o.LastRunAt = lastRun.Time.Format(time.RFC3339)
			// 停摆判定只对启用的任务有意义：停用的任务本来就不该跑
			o.Overdue = o.Enabled && now.Sub(lastRun.Time) > taskStaleAfter
		}
		// ⚠️ 「跑没跑过」以 **last_run_at** 为准，不看 last_ok。
		//
		// last_ok 是个带默认值的列：任务从没执行过时它可能是 1，
		// 于是一个从没被调度到的任务在界面上显示「正常」，
		// 而"上次执行"那一列写着"从没跑过" —— 同一行自相矛盾，
		// 且矛盾的方向是往"没问题"那边偏。
		if lastRun.Valid && lastOK.Valid {
			v := lastOK.Int64 == 1
			o.LastOK = &v
		}
		// 没跑过 → LastOK 保持 nil，状态归入 never
		all = append(all, o)
	}

	items, total, facets := taskPage(all, q)
	c.JSON(http.StatusOK, httpx.NewList(items, q, total).WithFacets(facets))
}

// taskState 归档。
//
//	disabled  停用（正常状态，不是故障）
//	failed    上次失败
//	skipped   跑完了但没得出结论（依赖没配、没有可检查对象）
//	overdue   早该跑了却没跑 —— 调度器静默停摆
//	never     从没跑过
//	ok
//
// ⚠️ skipped 必须独立于 ok 和 failed：
//
//	并进 ok  → 一次都没采到数据的任务显示绿色「正常」（OPSCMDB-006）
//	并进 fail → 没配依赖不是故障，天天报红会让人对告警脱敏
func taskState(t taskOut) string {
	if !t.Enabled {
		return "disabled"
	}
	// 优先信执行流水的状态：last_ok 只有两态，装不下「跳过」
	if t.LastStatus == taskStatusSkipped {
		return "skipped"
	}
	if t.LastOK == nil {
		return "never"
	}
	if !*t.LastOK {
		return "failed"
	}
	if t.Overdue {
		return "overdue"
	}
	return "ok"
}

func taskPage(all []taskOut, q httpx.PageQuery) ([]taskOut, int64, map[string]map[string]int64) {
	match := func(t taskOut) bool {
		return q.Keyword == "" ||
			strings.Contains(strings.ToLower(t.Name), strings.ToLower(q.Keyword)) ||
			strings.Contains(strings.ToLower(t.TaskKey), strings.ToLower(q.Keyword))
	}
	state := q.Filters["state"]
	facets := map[string]map[string]int64{"state": {}}
	for _, t := range all {
		if !match(t) {
			continue
		}
		facets["state"][taskState(t)]++
		facets["state"]["all"]++
	}
	filtered := make([]taskOut, 0, len(all))
	for _, t := range all {
		if !match(t) {
			continue
		}
		if state != "" && state != "all" && taskState(t) != state {
			continue
		}
		filtered = append(filtered, t)
	}

	sev := func(t taskOut) int {
		switch taskState(t) {
		case "failed":
			return 0
		case "overdue":
			return 1 // 静默停摆：界面上一直显示着上次的成功
		case "never":
			return 2
		case "ok":
			return 3
		default:
			return 4 // disabled 排最后：停用是正常状态
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		a, b := filtered[i], filtered[j]
		if q.SortBy == "name" {
			if q.SortDesc {
				return a.Name > b.Name
			}
			return a.Name < b.Name
		}
		if s1, s2 := sev(a), sev(b); s1 != s2 {
			return s1 < s2
		}
		return a.Name < b.Name
	})

	total := int64(len(filtered))
	lo := min(q.Offset(), len(filtered))
	hi := min(lo+q.Size, len(filtered))
	return filtered[lo:hi], total, facets
}
