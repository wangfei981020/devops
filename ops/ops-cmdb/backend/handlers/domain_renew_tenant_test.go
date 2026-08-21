package handlers

import (
	"testing"
	"time"

	"ops-cmdb-backend/internal/store"
)

// 批量续费进度必须按租户隔离。
//
// 进度里有域名、订单号、到期日和金额。原来内存和库两条路径都没有租户判定，
// 库那条还固定写在 store.PlatformTenant 下——等于所有租户的续费进度
// 混在同一个地方，谁拿到 job_id 谁就能看。
//
// job_id 是时间戳生成的（br-<UnixNano>），不是随机串：
// 猜中的难度远低于"随机 id 所以没关系"这种想当然的判断。
func TestBatchJobIsolatedByTenant(t *testing.T) {
	batchJobsMu.Lock()
	batchJobs = map[string]*batchRenewJob{}
	batchJobsMu.Unlock()

	const tenantA, tenantB = store.TenantID(1), store.TenantID(2)
	job := &batchRenewJob{
		ID: "br-shared-id", Tenant: tenantA, Total: 1, StartedAt: time.Now(),
		Items: []batchRenewItem{{Domain: "tenant-a-secret.com", OrderID: "ORD-A-1"}},
	}
	putJob(job)

	if got := getJob("br-shared-id", tenantA); got == nil {
		t.Error("发起方自己查不到自己的任务")
	}
	if got := getJob("br-shared-id", tenantB); got != nil {
		t.Errorf("租户 B 读到了租户 A 的任务：域名 %s 订单 %s",
			got.Items[0].Domain, got.Items[0].OrderID)
	}
}

// 平台租户（0）也不能当万能钥匙读别人的任务。
//
// 这条单列是因为 PlatformTenant 恰好是零值：
// 任何忘了赋 Tenant 的代码路径都会默认落到 0 上，
// 如果 0 能读所有人的，那个疏忽就变成了越权。
func TestPlatformTenantIsNotAMasterKey(t *testing.T) {
	batchJobsMu.Lock()
	batchJobs = map[string]*batchRenewJob{}
	batchJobsMu.Unlock()

	putJob(&batchRenewJob{ID: "br-x", Tenant: store.TenantID(7), StartedAt: time.Now()})
	if getJob("br-x", store.PlatformTenant) != nil {
		t.Error("平台租户读到了租户 7 的任务")
	}
}
