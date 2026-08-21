package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/logx"
)

// LoginStats 登录页左栏的三个数字。
//
// # 为什么只有这三个，且只有数字
//
// 这是**未认证**就能看到的接口，所以只给聚合计数，不给任何资源明细：
// 名字、IP、云账号、集群名一个都不出现。知道"这套系统管着 5 个集群"
// 对攻击者的价值接近于零，而对采购方的说服力是实打实的。
//
// # ⚠️ 三态：取不到就是取不到，绝不返回 0
//
// 数字取不到时返回 null，前端**整块不渲染**。
//
// 返回 0 是最坏的做法：登录页会显示"0 个集群、0 个资源"，
// 看起来像这套系统什么都没管，而实际只是查询失败了 ——
// 一个用来建立信任的区块，反而在故障时主动摧毁信任。
type LoginStats struct {
	Clusters  *int64 `json:"clusters"`
	Resources *int64 `json:"resources"`
	// FreshSeconds 距最近一次成功采集过了多少秒。
	//
	// ⚠️ 第三项刻意选"新鲜度"而不是"采集间隔"：
	// 间隔是配置（我们说多久采一次），新鲜度是事实（实际多久前采到的）。
	// 这套产品的主张就是"数据敢被信任"，登录页放一个事实比放一个承诺更有说服力。
	//
	// 顺带避开一个坑：间隔存在 scheduled_tasks.schedule 里，是 cron 表达式，
	// 为一个装饰数字去解析 cron 不值当（我第一版猜了个不存在的 interval_sec 列）。
	FreshSeconds *int64 `json:"fresh_seconds"`
}

// PublicStatsHandler 登录页统计。**不需要登录**，注册在鉴权中间件之前。
type PublicStatsHandler struct{ DB *sql.DB }

func NewPublicStatsHandler(db *sql.DB) *PublicStatsHandler { return &PublicStatsHandler{DB: db} }

func (h *PublicStatsHandler) RegisterPublic(r *gin.RouterGroup) {
	r.GET("/public/login-stats", h.LoginStats)
}

// count 查一个计数。失败返回 nil —— 让"查不到"和"确实是 0"在类型上就分开。
func (h *PublicStatsHandler) count(q string) *int64 {
	var n int64
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := h.DB.QueryRowContext(ctx, q).Scan(&n); err != nil {
		// 登录页不该因为这个查询失败而报错，但必须留下日志 ——
		// 否则那三个数字悄悄消失了没有任何人知道
		logx.J("public_stats", "count_failed", map[string]any{
			"level": "WARN", "query": q, "error": err.Error(),
		})
		return nil
	}
	return &n
}

func (h *PublicStatsHandler) LoginStats(c *gin.Context) {
	out := LoginStats{
		// ⚠️ 跨租户合计。登录页是未认证状态，还不知道来的人属于哪个租户，
		// 所以这里给的是整套部署的规模，不是某个租户的
		Clusters: h.count(`SELECT COUNT(*) FROM k8s_clusters`),
		// "在管资源"= CI 总数。用一个笼统的口径而不是分类明细，
		// 既是隐私考虑，也因为登录页放不下也没人看分类
		Resources: h.count(`SELECT COUNT(*) FROM cis`),
	}
	// 距最近一次成功采集多久。没采过（全新安装）时 last_sync 全为 NULL，
	// 这里也就取不到 —— 那正确的表现就是整块不显示，而不是显示"0 秒前"
	var sec sql.NullInt64
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := h.DB.QueryRowContext(ctx,
		`SELECT TIMESTAMPDIFF(SECOND, MAX(last_sync), NOW()) FROM k8s_sync_state`).
		Scan(&sec); err == nil && sec.Valid && sec.Int64 >= 0 {
		v := sec.Int64
		out.FreshSeconds = &v
	}
	c.JSON(http.StatusOK, out)
}
