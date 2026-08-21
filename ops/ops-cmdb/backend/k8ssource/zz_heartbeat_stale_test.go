package k8ssource

import (
	"testing"
	"time"
)

// 节点「失联」误报查了三个版本才收敛，根因是**两个独立的**、同一类的错误。
// 两个都钉在这里，因为它们都会随着某个常量被"顺手优化"而复活。
//
// 共同形态：**判定阈值 ≈ 信号本身的更新周期** → 每台节点各自掷硬币。
// 表现就是误报数一直在飘（生产实测 8 → 7 → 2）却永远归不了零。

// 错误一：我们自己把心跳取整到 5 分钟，判定阈值也是 5 分钟。
//
// last_heartbeat 存库前被 bucketHeartbeat 削到 5 分钟粒度（为了压写放大），
// 而且只有跨桶才重写。于是一台健康节点的**存储值**最老可以是
// 取整粒度 + 采集间隔。拿它减 NOW() 去卡 5 分钟阈值，必然误报。
//
// 这个测试不验证某段代码，它验证的是「能不能用存储值做判定」这件事本身。
func TestBucketedHeartbeatIsUnsoundForStaleJudgment(t *testing.T) {
	syncInterval := time.Duration(DefaultSyncIntervalSec) * time.Second
	worstCaseStoredAge := heartbeatBucket + syncInterval

	if worstCaseStoredAge <= HeartbeatStaleAfter {
		t.Fatalf("前提变了：健康节点存储心跳年龄上界 %v 已不超过阈值 %v，"+
			"本测试记录的误报机制可能已不成立，请重新推导后再改判定",
			worstCaseStoredAge, HeartbeatStaleAfter)
	}
	t.Logf("健康节点的存储心跳年龄上界 = 取整粒度 %v + 采集间隔 %v = %v，已超过阈值 %v"+
		" → 判定必须在采集时刻用未取整的心跳做",
		heartbeatBucket, syncInterval, worstCaseStoredAge, HeartbeatStaleAfter)
}

// 错误二（真正的主因）：conditions[Ready].lastHeartbeatTime 根本不是心跳。
//
// K8s 1.13 引入 NodeLease、1.17 GA 之后，kubelet 只在状态**变化**时更新 node.status；
// 状态没变就按 nodeStatusReportFrequency 走，默认 **5 分钟**。
// 真心跳在 kube-node-lease 的 Lease 里，~10 秒续约一次。
//
// 所以对 cond 来源必须用一个显著大于 5 分钟的阈值，否则又是掷硬币。
func TestCondHeartbeatThresholdMustExceedKubeletReportPeriod(t *testing.T) {
	// kubelet 的 nodeStatusReportFrequency 默认值。改这个常量前先确认上游没变。
	const kubeletStatusReportFrequency = 5 * time.Minute

	if condHeartbeatStaleAfter <= kubeletStatusReportFrequency {
		t.Fatalf("cond 来源阈值 %v 没有超过 kubelet 状态上报周期 %v —— "+
			"健康节点会被判成失联，这正是生产上查了三个版本的那个 bug",
			condHeartbeatStaleAfter, kubeletStatusReportFrequency)
	}
	if condHeartbeatStaleAfter < 3*kubeletStatusReportFrequency {
		t.Errorf("cond 来源阈值 %v 不足上报周期 %v 的 3 倍，余量太小："+
			"上报本身会抖动，贴着周期设阈值迟早再次误报",
			condHeartbeatStaleAfter, kubeletStatusReportFrequency)
	}

	// Lease 来源相反：续约 10 秒一次，阈值必须远大于它才不会被抖动误伤
	const leaseRenewPeriod = 10 * time.Second
	if HeartbeatStaleAfter < 10*leaseRenewPeriod {
		t.Errorf("lease 来源阈值 %v 不足续约周期 %v 的 10 倍，网络抖一下就会误报",
			HeartbeatStaleAfter, leaseRenewPeriod)
	}
	t.Logf("lease 阈值 %v（续约 %v，约 %d 次续约的余量）；cond 阈值 %v（上报 %v，约 %.1f 倍余量）",
		HeartbeatStaleAfter, leaseRenewPeriod, int(HeartbeatStaleAfter/leaseRenewPeriod),
		condHeartbeatStaleAfter, kubeletStatusReportFrequency,
		float64(condHeartbeatStaleAfter)/float64(kubeletStatusReportFrequency))
}

// nodeHeartbeat 必须优先用 Lease，并如实报出来源——
// 来源决定了这个判定有多可信，静默退化会让人以为精度还是好的。
func TestNodeHeartbeatPrefersLease(t *testing.T) {
	now := time.Now()
	lease := now.Add(-8 * time.Second) // 刚续约过
	cond := now.Add(-4 * time.Minute)  // 正常的 5 分钟上报节奏里的一个点
	leases := map[string]time.Time{"n1": lease}

	hb, src := nodeHeartbeat("n1", leases, &cond)
	if src != hbSourceLease {
		t.Fatalf("有 Lease 时来源应为 %s，实际 %s", hbSourceLease, src)
	}
	if !hb.Equal(lease) {
		t.Errorf("应取 Lease 的续约时间")
	}
	if heartbeatStale(hb, src) {
		t.Error("刚续约过的节点被判失联")
	}

	// 🔴 关键回归：同一台健康节点，如果拿 cond 去配 Lease 的阈值就会误判。
	// 这就是生产上那 2 台的成因
	if !heartbeatStale(&cond, hbSourceLease) {
		t.Log("注意：本次 cond 偏移未触发误判，但机制仍然成立")
	}
	if heartbeatStale(&cond, hbSourceCond) {
		t.Error("4 分钟前上报的 cond 在 cond 阈值下被判失联——阈值设错了")
	}

	// 没有 Lease 时退化到 cond，并且必须如实说明来源
	hb2, src2 := nodeHeartbeat("n2", leases, &cond)
	if src2 != hbSourceCond || !hb2.Equal(cond) {
		t.Errorf("无 Lease 时应退化到 cond，实际来源 %s", src2)
	}

	// 两个都没有：必须判为不可信，不能当成健康
	if _, src3 := nodeHeartbeat("n3", leases, nil); src3 != hbSourceNone {
		t.Errorf("两个来源都没有时应为 %s，实际 %s", hbSourceNone, src3)
	}
	if !heartbeatStale(nil, hbSourceNone) {
		t.Error("无心跳必须判为不可信——当成新鲜会让从没上报过的节点显示成一切正常")
	}

	old := now.Add(-30 * time.Minute)
	if !heartbeatStale(&old, hbSourceLease) || !heartbeatStale(&old, hbSourceCond) {
		t.Error("30 分钟没心跳却没判失联，阈值形同虚设")
	}
}
