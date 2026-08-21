// Package notify 通知渠道管理（飞书群、通知人）。
//
// 迁移自 handlers/lark_groups.go，按 registrar 样板做租户隔离，
// 并修掉了 4 处 `WHERE id=?` 的跨租户越权。
//
// # 这里的凭据是 webhook URL
//
// webhook URL 本身**就是凭据**：不需要任何额外鉴权，谁拿到谁就能往那个群发消息。
// 所以它同时受两重保护：
//   - 租户隔离：别的租户根本查不到这行
//   - 权限掩码：本租户内没有通知管理权限的人，看到的是打码值
//
// 在前端打码是假的 —— 值已经在响应里了，F12 就能看见。
package notify

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/internal/store"
	"ops-cmdb-backend/notify"
)

// PermChecker 判断当前请求是否有某权限。
//
// 抽成接口是为了不让本包依赖 handlers（那会形成循环）——
// 权限判定还在旧包里，等它迁移后这个接口可以去掉。
type PermChecker func(c *gin.Context, perm string) bool

// Masker 把 webhook 打码。同样是为了解耦。
type Masker func(string) string

type LarkHandler struct {
	Store   *store.Store
	HasPerm PermChecker
	Mask    Masker
}

func NewLark(st *store.Store, hasPerm PermChecker, mask Masker) *LarkHandler {
	return &LarkHandler{Store: st, HasPerm: hasPerm, Mask: mask}
}

func (h *LarkHandler) Register(r *gin.RouterGroup) {
	r.GET("/lark-groups", h.List)
	r.POST("/lark-groups", h.Create)
	r.PUT("/lark-groups/:id", h.Update)
	r.DELETE("/lark-groups/:id", h.Delete)
	r.POST("/lark-groups/:id/test", h.Test)
}

type larkGroup struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Webhook string `json:"webhook"`
}

func (h *LarkHandler) List(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	rows, err := sc.Query(`SELECT id, name, webhook FROM lark_groups WHERE tenant_id = ? ORDER BY id`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	// 没有通知管理权限的人只看到打码值 —— 租户隔离挡的是别的租户，
	// 这一层挡的是本租户内的只读账号。两者都要有。
	full := h.HasPerm(c, "cmdb:manage_notify")
	out := []larkGroup{}
	for rows.Next() {
		var g larkGroup
		if rows.Scan(&g.ID, &g.Name, &g.Webhook) != nil {
			continue
		}
		if !full {
			g.Webhook = h.Mask(g.Webhook)
		}
		out = append(out, g)
	}
	// 🔴 全局兜底出口也要说出来。
	//
	//	它存在 settings.feishu_webhook，**不在 lark_groups 表里** ——
	//	而这一页只列 lark_groups，于是界面写着「一个群都没配」，
	//	任务执行记录里却写着「已发送到 全局兜底出口」，两句话直接打架
	//	（生产实测，OPSCMDB-080）。
	//
	//	配置项分散在两处、界面只显示一处，人就会据此得出错误结论：
	//	以为通知完全发不出去（实际有兜底），或以为配好了（实际兜底是坏的）。
	//
	// ⚠️ 只发**配没配**这个事实，不发 webhook 本身 —— 那是凭据，write-only
	//	（同一文件上方 sensitiveSettingKeys 的理由）。
	// ⚠️ settings 是**全局表**（不带 tenant_id），所以这里不能用 sc 的租户改写。
	//	Scoped.QueryRow 会强制要求租户过滤（这是对的），所以这里走 Raw()。
	var fallback string
	_ = h.Store.Raw().QueryRow(`SELECT COALESCE(v,'') FROM settings WHERE k='feishu_webhook'`).Scan(&fallback)
	c.JSON(http.StatusOK, gin.H{
		"items":                out,
		"global_fallback_set":  fallback != "",
		"global_fallback_hint": "全局兜底出口配在「基础配置 → 系统设置」，不在这一页",
	})
}

func (h *LarkHandler) Create(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var in struct {
		Name    string `json:"name"`
		Webhook string `json:"webhook"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.Name == "" || in.Webhook == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "群名和 webhook 必填"})
		return
	}
	if _, err := sc.Insert(`INSERT INTO lark_groups (tenant_id, name, webhook) VALUES (?, ?, ?)`,
		in.Name, in.Webhook); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"ok": true})
}

func (h *LarkHandler) Update(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id 不合法"})
		return
	}
	// 指针 = 三态：没传（不动它）/ 显式清空 / 改成它。
	// 用普通 string 时，只想换 webhook 就会把群名清空 —— 通知列表里
	// 那一行会变成一个没有名字的群，谁也认不出它是哪个（OPSCMDB-083）。
	var in struct {
		Name    *string `json:"name"`
		Webhook string  `json:"webhook"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name 不能为空"})
		return
	}

	// 界面上显示的是掩码，原样提交回来不能当成"用户要改成这个" ——
	// 否则一次编辑就把真 webhook 覆盖成了一串星号。留空同样按"不改"处理。
	cols := []string{}
	args := []any{}
	if in.Name != nil {
		cols = append(cols, "name=?")
		args = append(args, *in.Name)
	}
	if in.Webhook != "" && !isMaskedValue(in.Webhook) {
		cols = append(cols, "webhook=?")
		args = append(args, in.Webhook)
	}
	if len(cols) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "至少要传一个要改的字段"})
		return
	}
	var res interface{ RowsAffected() (int64, error) }
	res, err = sc.Exec(`UPDATE lark_groups SET `+strings.Join(cols, ", ")+
		` WHERE tenant_id = ? AND id=?`, append(args, id)...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "群不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *LarkHandler) Delete(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id 不合法"})
		return
	}
	res, err := sc.Exec(`DELETE FROM lark_groups WHERE tenant_id = ? AND id=?`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "群不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Test 往该群发一条测试消息。
//
// ⚠️ 这个接口会**用存着的 webhook 真的发一条消息**。没有租户过滤的话，
// A 租户就能触发向 B 租户的群发消息 —— 既是信息泄露（暴露了群的存在），
// 也是骚扰渠道。
func (h *LarkHandler) Test(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id 不合法"})
		return
	}
	var webhook string
	if err := sc.QueryRow(`SELECT webhook FROM lark_groups WHERE tenant_id = ? AND id=?`, id).
		Scan(&webhook); err != nil || webhook == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "群不存在或 webhook 为空"})
		return
	}
	if err := notify.SendFeishu(webhook, "【测试】该群通知通道正常 ✅"); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "发送失败：" + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "msg": "已发送，去群里看"})
}

// isMaskedValue 判断是不是掩码回传。
// 与旧实现保持同样的判据：含连续 3 个及以上的 * 即视为掩码。
func isMaskedValue(s string) bool {
	run := 0
	for _, r := range s {
		if r == '*' {
			run++
			if run >= 3 {
				return true
			}
		} else {
			run = 0
		}
	}
	return false
}
