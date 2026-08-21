// Package httpx 是接口层共用的错误响应。
//
// 抽出来是因为 ee/ 下的企业版模块也要用它 —— 而**错误响应本身不是企业版功能**。
// 让 ee 反向依赖 internal/api 会形成循环，各写一份则会让两边的失败语义分叉，
// 而分叉的方向通常是"其中一边把失败渲染成了空"。
package httpx

import (
	"errors"
	"net/http"

	"github.com/go-sql-driver/mysql"
	"github.com/gin-gonic/gin"

	"ops-alert-backend/logx"
)


func AbortTenant(c *gin.Context, err error) {
	logx.J("api", "no_tenant", map[string]any{"error": err.Error()})
	c.JSON(http.StatusForbidden, gin.H{"error": "no_tenant_context"})
}

// abortQuery 统一的查询失败响应。
//
// 三态纪律：失败必须长得像失败。返回空列表 + 200 会让前端渲染成
// 「当前没有数据」，而真相是查询挂了——这正是本产品要根治的问题，
// 自己的接口先不能犯。
//
// ⚠️ 两条边界，都是实测踩出来的：
//
//  1. 唯一键冲突是**用户输入问题**，不是服务端故障。返回 500 会让人以为
//     系统坏了并重试，而重试多少次都一样。要 409 + 能看懂的话。
//  2. 原始错误**不回传给客户端**。MySQL 的 1062 长这样：
//     Duplicate entry '1-probe-1' for key 'rules.uk_rule_name'
//     —— 表名、索引名、租户 ID、字段值全在里面。日志里留全文，
//     响应里只给结论。
func AbortQuery(c *gin.Context, err error) {
	logx.J("api", "query_error", map[string]any{"path": c.FullPath(), "error": err.Error()})
	var me *mysql.MySQLError
	if errors.As(err, &me) && me.Number == 1062 {
		c.JSON(http.StatusConflict, gin.H{"error": "duplicate_name"})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": "query_failed"})
}
