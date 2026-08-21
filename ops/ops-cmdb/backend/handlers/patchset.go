package handlers

import (
	"strings"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/internal/httpx"
)

// 部分更新（PATCH 语义）的 SET 子句拼装。
//
// # 为什么需要它
//
// 写接口原来一律是「一次 UPDATE 写死所有列」，值取自绑定后的结构体。
// Go 的零值让**「没传这个字段」和「把它设成空」完全无法区分**，
// 于是缺省的字段被空串覆盖：
//
//	PUT /api/domains/171  {"expiry_at": "2026-09-02"}
//	→ 200 {"ok": true}，而这个域名的 **name 变成了空**（OPSCMDB-083）
//
// 🔴 界面上一直看不出来，因为表单**总是全量提交**（打开弹窗时填好现有值）。
//
//	而同一个接口对外开着（MCP / 脚本 / AI），那一侧按 REST 直觉发部分字段，
//	就会静默清空其余的 —— 200 OK，没有任何提示。
//
// ⚠️ 不要用"缺省时保留原值"来绕过：那会让调用方再也没法把一个字段真的改成空
//
//	（比如清掉备注），语义变得不可预测。正解是**用指针区分三态**：
//	nil=没传（不动它） / 非 nil 指向零值=显式清空 / 非 nil 有值=改成它。
type patchSet struct {
	cols []string
	args []any
}

// Add 字段非 nil 时才加进 SET。expr 缺省是 `col=?`，需要包一层（如
// `expiry_at=NULLIF(?, ”)`）时用 AddExpr。
func (p *patchSet) Add(col string, v any) {
	switch t := v.(type) {
	case *string:
		if t == nil {
			return
		}
		p.push(col+"=?", *t)
	case *int:
		if t == nil {
			return
		}
		p.push(col+"=?", *t)
	case *bool:
		if t == nil {
			return
		}
		p.push(col+"=?", *t)
	case *float64:
		if t == nil {
			return
		}
		p.push(col+"=?", *t)
	default:
		// 其它类型显式传 AddExpr，别在这里猜
		panic("patchSet.Add: 不支持的类型，请用 AddExpr")
	}
}

// AddExpr 自定义 SET 表达式（如 `expiry_at=NULLIF(?, ”)`）。cond 为 false 时跳过。
func (p *patchSet) AddExpr(cond bool, expr string, args ...any) {
	if !cond {
		return
	}
	p.push(expr, args...)
}

func (p *patchSet) push(expr string, args ...any) {
	p.cols = append(p.cols, expr)
	p.args = append(p.args, args...)
}

// Empty 一个字段都没传。
//
// ⚠️ 调用方必须处理这种情况：拼出 `UPDATE t SET  WHERE ...` 是语法错误，
//
//	而"什么都没传"本身也该是 400 —— 一次什么都不改的写请求多半是调用方写错了，
//	静默返回 200 会让它以为改成功了。
func (p *patchSet) Empty() bool { return len(p.cols) == 0 }

// SQL SET 子句（不含 "SET" 关键字）。
func (p *patchSet) SQL() string { return strings.Join(p.cols, ", ") }

// Args 与 SQL() 顺序对应的参数。
func (p *patchSet) Args() []any { return p.args }

// requireNonBlank 传了就不能是空白。
//
// 🔴 三态里"显式清空"对**身份字段**是非法的：一个名字为空的字典项
//
//	在界面上只会显示成一行空白，谁都不知道它是什么。想删走 DELETE。
//	返回 true 表示已经写过错误响应，调用方直接 return。
func requireNonBlank(c *gin.Context, field string, v *string) bool {
	if v != nil && strings.TrimSpace(*v) == "" {
		httpx.Invalid(c, field, "非空")
		return true
	}
	return false
}

// derefInt *int 取值，nil 当 0。只在已确认语义的分支里用。
func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}
