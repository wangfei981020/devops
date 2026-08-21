package store

import (
	"context"
	"fmt"

	"ops-alert-backend/logx"
)

// ForEachTenant 逐租户执行 fn，每一轮只给**一个租户**的作用域。
//
// # 这个函数为什么必须存在
//
// 后台任务要处理所有租户的数据。曾经的写法是：
//
//	st.Platform("host_sync").Query(`SELECT tenant_id, id FROM cloud_account_projects`)
//
// 也就是先跨租户捞出 (tenant_id, id) 对，再逐个构造上下文。意图是对的，
// **机制是错的**：Platform() 只放行平台级白名单表，而 cloud_account_projects
// 是租户表 —— 这条查询从来没成功过一次。
//
// 实际后果：证书自动续期、磁盘水位巡检、DNS 同步、主机同步**四个任务
// 全部静默失败**，而且因为 `scheduled_tasks.last_ok` 默认值是 1，
// 界面上它们一直显示「正常」。挂了多久没人知道。
//
// ⚠️ 更糟的是，这个错误写法当时被写进了 ForJob 的文档注释里当作**正确示例**，
// 于是后来每个照着抄的任务都是坏的。文档里的示例代码和生产代码一样需要被验证。
//
// # 为什么不给一个「能看所有租户」的作用域
//
// 那是最省事的解法，也是最危险的：一旦存在这种作用域，任务里任何一处
// 漏掉租户条件都会静默跨租户读写，而且不会有任何报错。
// 逐租户构造的代价只是一次租户列表查询，换来的是**越权在 Scoped 层被挡住**，
// 而不是靠人记得加条件。
//
// # 单个租户失败不中断其余租户
//
// fn 返回错误时记日志并继续下一个租户。一个租户的凭据过期不该让
// 其他所有租户的同步都停摆 —— 那会把一个局部故障放大成全局故障。
// 返回值是「出错的租户数」，调用方**必须**据此决定任务整体算不算成功。
//
// ⚠️ 丢掉这个返回值会造成比原来更坏的结果：原本任务是响亮地失败
// （"查集群列表失败: ..."），丢掉之后变成"检查 0 个集群：无超阈值磁盘"——
// 报的是成功，读起来还像好消息。**把一个响亮的失败改成静默的成功，
// 是比不修更糟的修法**。所以下面提供 AllFailed 让调用方一句话表态。
func ForEachTenant(ctx context.Context, st *Store, job string, fn func(sc *Scoped, tenant TenantID) error) (failed int, err error) {
	rows, err := st.Platform(job).Query(ctx, ActiveTenantsQuery)
	if err != nil {
		// ⚠️ 这里失败意味着**一个租户都不会被处理**，不是"少处理一个"。
		// 必须让调用方能把它报成任务失败，而不是报成"处理了 0 个，成功"
		return 0, fmt.Errorf("取租户列表失败: %w", err)
	}
	var tenants []TenantID
	for rows.Next() {
		var t TenantID
		if rows.Scan(&t) == nil {
			tenants = append(tenants, t)
		}
	}
	rows.Close()

	if len(tenants) == 0 {
		// 一个启用的租户都没有，也要说出来。静默返回成功的话，
		// 「任务跑了但什么都没做」和「任务正常且确实没数据」分不开
		logx.J("store", "foreach_no_tenant", map[string]any{
			"job": job, "warn": "没有任何启用中的租户，本轮任务实际上什么都没做",
		})
		return 0, nil
	}

	for _, t := range tenants {
		sc, e := st.Tenant(ForJob(ctx, t, job))
		if e != nil {
			failed++
			logx.J("store", "foreach_scope_fail", map[string]any{
				"job": job, "tenant_id": int64(t), "err": e.Error(),
			})
			continue
		}
		if e := fn(sc, t); e != nil {
			failed++
			logx.J("store", "foreach_tenant_fail", map[string]any{
				"job": job, "tenant_id": int64(t), "err": e.Error(),
				"note": "该租户本轮失败，其余租户继续",
			})
		}
	}
	return failed, nil
}

// AllFailed 判断「所有租户都失败了」。
//
// 给调用方一个不用自己算的表态方式：处理了 N 个租户、N 个都失败，
// 那么任务整体就是失败的，哪怕它"跑完了"。
//
// ⚠️ 部分失败（有的租户成功、有的失败）不算整体失败，但**必须体现在摘要里**——
// 调用方要把失败的租户写进 TaskFailure，否则界面上是一句干净的"成功"，
// 而某个租户的数据其实一直没更新。
func AllFailed(failed, total int) bool { return total > 0 && failed >= total }

// CountActiveTenants 启用中的租户数，给调用方判断"是不是全挂了"用。
func CountActiveTenants(ctx context.Context, st *Store, job string) int {
	rows, err := st.Platform(job).Query(ctx, ActiveTenantsQuery)
	if err != nil {
		return 0
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		n++
	}
	return n
}
