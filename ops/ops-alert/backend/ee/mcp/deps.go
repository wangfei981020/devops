// SPDX-License-Identifier: LicenseRef-OpsAlert-Enterprise

package mcp

import (
	"context"

	"github.com/gin-gonic/gin"

	"ops-alert-backend/internal/store"
)

// Server 是本包需要的全部外部依赖。
//
// # 为什么是一个小结构体而不是 interface
//
// 依赖只有两项（存储、审计），而且都是具体的东西。
// 定义 interface 只会多一层名字，还得在 api 侧写一个适配器 ——
// 而这个包与 api 之间没有需要解耦的多态。
//
// ⚠️ 往这里加字段前先想清楚：这个包依赖得越多，
// "企业版代码"和"社区版代码"的边界就越模糊。
type Server struct {
	St *store.Store
	// Audit 记审计。由 api 侧注入 —— 审计表是社区版的东西，
	// 不该因为 MCP 是企业版就跟着搬进 ee/
	Audit func(c *gin.Context, sc *store.Scoped, action, obj string, id int64, detail any)
	// Licensed 判断 MCP 功能是否被授权。返回 false 时整个模块对外不可见：
	// 路由 402、菜单不显示。
	//
	// ⚠️ 注入而不是在这里直接读 license.Manager：那会让 ee 包依赖授权内核，
	// 而授权内核是社区版也要用的（它决定 CE 的容量上限）。
	Licensed func() bool
	// DryRun 试运行一次查询。由 api 侧注入引擎的方法 ——
	// 检测引擎是社区版的核心，不该因为 MCP 用到它就搬进 ee/
	DryRun func(ctx context.Context, sc *store.Scoped, dsID int64, query string,
		lookbackSec, threshold, limit int, groupBy []string) (any, error)
}

// defaultStr 空值兜底。这类一行助手在包内自带一份，
// 比为它建一个共享包更清楚 —— 共享的成本是每次读代码都要跳一次。
func defaultStr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
