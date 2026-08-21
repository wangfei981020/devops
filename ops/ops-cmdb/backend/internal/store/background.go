package store

import (
	"context"

	"ops-cmdb-backend/logx"
)

// ForJob 给后台任务构造一个带租户的上下文。
//
// # 为什么不直接用 WithTenant
//
// WithTenant 的约定是「只该由认证中间件调用」—— 因为那里的租户来自
// 会话，且经过了 user_tenants 成员校验。后台任务没有会话，租户是
// 任务**自己从数据里读出来的**，这是完全不同的信任来源。
//
// 混用同一个入口会让「这个租户是谁给的」不可追溯：某天出现跨租户写入，
// 你没法从代码上区分是中间件放行错了，还是某个定时任务自作主张。
// 所以后台路径走这个独立入口，且**每次都记一条日志**。
//
// # 绝不能有「超级上下文」
//
// 后台任务要处理多个租户时，用 ForEachTenant（见 foreach.go）：
//
//	failed, err := store.ForEachTenant(ctx, st, "host_sync", func(sc *store.Scoped, t store.TenantID) error {
//	    rows, err := sc.Query(`SELECT id FROM cloud_account_projects`) // 租户条件由 Scoped 自动加
//	    ...
//	})
//
// 而**不是**造一个「能看所有租户」的上下文然后一把梭 —— 那样任务里
// 任何一处漏掉 tenant 条件都会静默地跨租户写。逐租户的写法让越权
// 在 Scoped 层就被拦住，而不是靠人记得加条件。
//
// ⚠️ 这段注释原本给的示例是：
//
//	st.Platform("host_sync").Query(`SELECT tenant_id, id FROM cloud_account_projects`)
//
// **那是错的**：Platform() 只放行平台级白名单表，而 cloud_account_projects
// 是租户表，这条查询从来没成功过一次。照着它抄的四个定时任务
// （证书续期 / 磁盘巡检 / DNS 同步 / 主机同步）全部静默失败了很久。
// 教训：文档里的示例代码和生产代码一样会错，也一样需要被验证。
func ForJob(ctx context.Context, tenant TenantID, job string) context.Context {
	logx.J("store", "job_tenant_ctx", map[string]any{
		"job": job, "tenant_id": int64(tenant),
	})
	return WithTenant(ctx, tenant)
}
