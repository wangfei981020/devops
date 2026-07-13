package services

import (
	"context"
	"sync/atomic"
	"time"
)

// 全局并发闸：限制"同时进行的 git/helm 重活"数量——防止 100 人同时发布/新增时
// 一下起 100 个 git clone + helm 子进程把内存打爆 OOM。超过名额的排队等待。
//
// 这是防 OOM 的根本手段（不是靠堆内存）。名额数 = InitHeavyGate 传入（来自配置/环境变量）。
var (
	heavyGate    chan struct{}
	gateInFlight atomic.Int32 // 正在跑的重活数
	gateWaiting  atomic.Int32 // 正在排队等名额的数
	gateCap      int32        // 名额上限（0=未初始化=不限流）
)

// InitHeavyGate 初始化并发闸。limit<=0 时用默认 12。启动时调一次。
func InitHeavyGate(limit int) {
	if limit <= 0 {
		limit = 12
	}
	atomic.StoreInt32(&gateCap, int32(limit))
	heavyGate = make(chan struct{}, limit)
}

// AcquireHeavy 抢一个重活名额。返回排队等待时长 + 释放函数。
// ctx 取消时立即返回 err（不占名额）。未初始化时不限流直接放行。
func AcquireHeavy(ctx context.Context) (waited time.Duration, release func(), err error) {
	if heavyGate == nil {
		return 0, func() {}, nil
	}
	start := time.Now()
	gateWaiting.Add(1)
	select {
	case heavyGate <- struct{}{}:
		gateWaiting.Add(-1)
		gateInFlight.Add(1)
		var once int32
		return time.Since(start), func() {
			if atomic.CompareAndSwapInt32(&once, 0, 1) {
				<-heavyGate
				gateInFlight.Add(-1)
			}
		}, nil
	case <-ctx.Done():
		gateWaiting.Add(-1)
		return time.Since(start), func() {}, ctx.Err()
	}
}

// GateStats 返回 (在飞, 排队, 上限)，供日志和 /internal/inflight 观测。
func GateStats() (inFlight, waiting, capacity int) {
	return int(gateInFlight.Load()), int(gateWaiting.Load()), int(atomic.LoadInt32(&gateCap))
}
