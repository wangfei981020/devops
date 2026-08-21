package licensekit

import (
	"context"
	"time"
)

// Source 授权的持久化来源，由产品实现（通常就是它自己的数据库）。
//
// 拆成两个方法是为了让轮询足够便宜：Revision 每个周期都调，Fetch 只在变了才调。
type Source interface {
	// Revision 返回一个**随授权变化而变化**的标识，用于探测"要不要重新装载"。
	//
	// 实现建议：`SELECT updated_at FROM license LIMIT 1`，或 token 的哈希。
	// 它会被每个副本每隔几十秒调一次，必须是索引命中的廉价查询。
	//
	// 授权被清空时返回空串——Watch 据此调 Clear()。
	Revision(ctx context.Context) (string, error)

	// Fetch 取出完整授权。只在 Revision 变化时调用。
	//
	// 返回值对应 LoadAt 的三个入参。payload 为 nil 表示"当前没有授权"，
	// Watch 会调 Clear() 退回未激活。
	Fetch(ctx context.Context) (payload *Payload, fingerprint string, mismatchSince time.Time, err error)
}

// Watch 周期性地把本副本的授权状态收敛到数据库里的最新值。**阻塞运行，通常起一个 goroutine。**
//
// # 为什么需要它
//
// 管理员把激活码粘进 A 副本，A 验签、写库、更新自己的内存状态——
// 而 B、C 副本还停在旧状态，直到它们各自重启。
// 现象是「激活了但一半请求仍报未授权」，且刷新几次就好几次不好，
// 极难排查。副本数大于 1 就必须解决这件事。
//
// # 为什么用轮询而不是 Redis 广播
//
// 参考实现用 Redis pub/sub，因为它本来就依赖 Redis。我们不行：
// licensekit 是**跨产品共享库**，不是每个产品都有 Redis，
// 为了这一件事引入一个硬依赖不划算。而数据库是每个产品都有的——
// 授权本来就存在那里。
//
// 代价是收敛有延迟。验收标准是「激活后 60s 内全部 Pod 生效」，
// 取 15~30s 的间隔有充足余量。授权变更是个低频操作，
// 为它做实时推送是过度设计。
//
// # 出错时保持原状，绝不降级
//
// 数据库抖一下就把全公司的授权吊销，是比"晚 30 秒生效"严重得多的事故。
// 所以 Revision / Fetch 报错时只回调 OnWatchError，**当前状态一个字节都不动**。
//
// 反过来，Fetch 明确返回 nil payload（授权被删了）才会 Clear ——
// "查不出来"和"确认没有"是两件事，不能混。
func (m *Manager) Watch(ctx context.Context, src Source, interval time.Duration) {
	if src == nil || interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// 记住上一次看到的 revision。初值是空串，所以首次循环一定会拉一次全量——
	// 这正好覆盖了"进程刚起来，内存里还没有授权"的情况。
	var seen string
	first := true

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		rev, err := src.Revision(ctx)
		if err != nil {
			m.watchErr(err)
			continue // 保持原状
		}
		if rev == seen && !first {
			continue
		}

		payload, fingerprint, mismatchSince, err := src.Fetch(ctx)
		if err != nil {
			m.watchErr(err)
			continue // 保持原状。注意此处**不更新 seen**，下个周期还会重试
		}
		if payload == nil {
			m.Clear()
		} else {
			m.LoadAt(payload, fingerprint, mismatchSince)
		}
		seen = rev
		first = false
	}
}

func (m *Manager) watchErr(err error) {
	if m.onWatchError != nil {
		m.onWatchError(err)
	}
}
