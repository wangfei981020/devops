package services

import (
	"context"
	"sync"
)

// =========================================================================
//   ⚠ 并发池 — 维护者请读完注释再改 ⚠
//
// 这是发布中心里唯一的并发原语封装。历史上"50 个模块串行重启要等 25 分钟"
// 就是因为这层缺失。改动时严格遵循：
//
//   1. 【每个 worker 只写 results[i]】— 按 index 写入不共享 slot，slice 头
//      在调用前预分配，**不要** append。违反此约定会立刻 race。
//   2. 【onEach 回调在锁内调用】— 调用方可在 onEach 里读完整 results 做快照
//      (如渐进式写 DB)。snapshot 前请 copy；不要把 results 的引用传出锁外。
//   3. 【ctx 传到 workFn 里】— 用 egCtx，不是原始 ctx；保证上游取消能立刻
//      中断所有 worker。
//   4. 【改之前先跑 `go test -race ./services/...`】— concurrent_test.go
//      有压力 & 取消传播测试。任何改动 race 测试失败视为回归。
//
// 代码量故意很小（<60 行），就是为了让 review 时一眼看完。别往里面堆业务逻辑。
// =========================================================================

// RunBoundedConcurrent 用 <= limit 个 worker 并发执行 jobs，results[i] ↔ jobs[i]。
//
//   - jobs:    输入切片（按序分发）
//   - limit:   最大并发 worker 数（≤0 自动降到 1；>len(jobs) 等价于全并发）
//   - workFn:  每个 job 的处理函数，返回 R
//   - onEach:  可为 nil；每个 job 完成时在锁内调用一次，接收 index 和当前 results 快照。
//     用途：渐进式写 DB。实现里可以安全地读 results（已被锁保护），但读完
//     立刻应复制数据离开锁，不要把引用传出。
//
// 返回：results，长度 == len(jobs)，每个元素按输入 index 对应。即使 ctx 超时/取消，
// 也返回部分结果（未完成的 slot 是 R 的零值）。
func RunBoundedConcurrent[T any, R any](
	ctx context.Context,
	jobs []T,
	limit int,
	workFn func(ctx context.Context, job T) R,
	onEach func(idx int, results []R),
) []R {
	results := make([]R, len(jobs))
	if len(jobs) == 0 {
		return results
	}
	if limit <= 0 {
		limit = 1
	}
	if limit > len(jobs) {
		limit = len(jobs)
	}

	sem := make(chan struct{}, limit)
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(len(jobs))

	for i, job := range jobs {
		i, job := i, job
		go func() {
			defer wg.Done()
			// acquire slot（支持 ctx 取消）
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			r := workFn(ctx, job)

			mu.Lock()
			results[i] = r
			if onEach != nil {
				onEach(i, results)
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	return results
}
