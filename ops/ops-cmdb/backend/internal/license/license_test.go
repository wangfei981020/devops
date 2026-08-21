package license

import (
	"testing"
	"time"

	"ops-kit/licensekit"
)

func grant(features []string, cap map[string]int64) map[string]licensekit.ProductGrant {
	return map[string]licensekit.ProductGrant{
		ProductID: {Features: features, Capacity: cap},
	}
}

func payload(products map[string]licensekit.ProductGrant, expires time.Time, installID string) *licensekit.Payload {
	return &licensekit.Payload{
		LicenseID: "LIC-TEST",
		Licensee:  licensekit.Licensee{Org: "测试组织"},
		Products:  products,
		InstallID: installID,
		ExpiresAt: expires,
	}
}

// implemented 里的每一项都必须在目录里 —— 防手滑写错字符串。
// 写错了会表现为「买了却不给用」，且没有任何报错。
func TestImplementedIsSubsetOfCatalog(t *testing.T) {
	for f := range implemented {
		if !allFeatures[Feature(f)] {
			t.Errorf("implemented 里的 %q 不在 allFeatures 目录中", f)
		}
	}
}

// ceLimits 的 key 必须都是有效的容量项 —— 写错了会静默回落成 0（不限）。
func TestCELimitKeysAreValid(t *testing.T) {
	valid := map[string]bool{
		CapNodes: true, CapClusters: true, CapCloudAccounts: true,
		CapSeats: true, CapTenants: true, CapRetentionDays: true,
	}
	for k := range ceLimits {
		if !valid[k] {
			t.Errorf("ceLimits 里的 %q 不是有效容量项", k)
		}
	}
}

// ★ 逐项判定，不是「有效 license = 全给」。
//
// 本产品当前 implemented 是空的（重构期，能力还在旧系统里），
// 所以这里临时注入一个已实现表来验判定逻辑本身。
func TestHasIsPerFeature(t *testing.T) {
	now := time.Now()
	m := &Manager{Manager: licensekit.NewManager(licensekit.Options{
		Product:     ProductID,
		Implemented: map[string]bool{string(FeatureSSO): true, string(FeatureCost): true},
		CELimits:    ceLimits,
		Now:         func() time.Time { return now },
	})}
	m.Load(payload(grant([]string{string(FeatureSSO)}, nil), now.AddDate(1, 0, 0), ""), "")

	if !m.Has(FeatureSSO) {
		t.Error("授权了 sso 却判为无")
	}
	if m.Has(FeatureCost) {
		t.Error("没授权 cost 却判为有 —— 退化成了「全给」")
	}
}

// ★ 重构期的诚实性：没点亮的 feature 即使签进 license 也不该放行。
// 这防的是「卖了但没做」—— 界面上出现点不动的功能比没有更糟。
//
// 已点亮的三个（cost / version_upgrade / audit_rollback）在
// guard_test.go 的 TestFeatureRoutesAreImplemented 里另有约束：
// 点亮就必须同时挂上门控，否则等于白送。
func TestUnimplementedStaysOff(t *testing.T) {
	now := time.Now()
	m := newManagerAt(func() time.Time { return now })
	// 全档次都签给它，唯一还能挡住的就只有 implemented
	m.Load(payload(grant([]string{"plan:enterprise"}, nil), now.AddDate(1, 0, 0), ""), "")

	for _, f := range []Feature{FeatureBranding, FeatureMultiTenant, FeatureMultiCloud} {
		if m.Has(f) {
			t.Errorf("%s 尚未实现，必须返回 false（重构到哪步才点亮哪个）", f)
		}
	}
	// 反过来，已实现且已授权的必须放行 —— 否则就成了「买了却不给用」
	// ⚠️ FeatureExposure 从「未点亮」挪到了这里：它是第一个纯只读的付费功能，
	//	门控挂在 guard.go 的 featureReadPrefixes（OPSCMDB-072）。
	for _, f := range []Feature{FeatureCost, FeatureVersionUpgrade, FeatureAuditRollback,
		FeatureMCPFull, FeatureSSO, FeatureExposure} {
		if !m.Has(f) {
			t.Errorf("%s 已实现且在 enterprise 档里，必须放行", f)
		}
	}
}

// ★ 多产品：只买了发布平面，装数据平面时应报「不含本产品」而不是「未激活」——
// 客户要能区分「没买这个产品」和「激活码不对」。
func TestOtherProductDoesNotLeak(t *testing.T) {
	now := time.Now()
	m := newManagerAt(func() time.Time { return now })
	m.Load(payload(map[string]licensekit.ProductGrant{
		"ops-release-plane": {Features: []string{"approval"}},
	}, now.AddDate(1, 0, 0), ""), "")

	if m.Status() != StatusNotLicensed {
		t.Errorf("status = %v, want not_licensed", m.Status())
	}
	if m.Has(FeatureSSO) {
		t.Error("拿到了别的产品的授权 —— 串了")
	}
}

// 未激活时回落到 CE 容量。
func TestNotActivatedUsesCELimits(t *testing.T) {
	m := NewManager()
	c := m.Capacity()
	if c.Nodes != 100 || c.Clusters != 3 || c.Tenants != 1 {
		t.Errorf("CE 容量 = %+v, want nodes=100 clusters=3 tenants=1", c)
	}
}

// ★ 过期不停服：宽限期内容量与状态照常。
func TestGraceKeepsCapacity(t *testing.T) {
	now := time.Now()
	m := newManagerAt(func() time.Time { return now })
	m.Load(payload(grant(nil, map[string]int64{CapNodes: 1000}),
		now.AddDate(0, 0, -10), ""), "") // 过期 10 天，仍在 30 天宽限内

	if m.Status() != StatusGrace {
		t.Fatalf("status = %v, want grace", m.Status())
	}
	if m.ReadOnly() {
		t.Error("宽限期内不该只读 —— 过期不停服")
	}
	if got := m.Capacity().Nodes; got != 1000 {
		t.Errorf("宽限期内容量 = %d, want 1000（不该降级到 CE）", got)
	}
}

// ★ 指纹不匹配（客户把库恢复到新实例 / 主从切换）：进宽限，不是立刻停服。
// 那通常发生在故障处理期间，此时降级等于在故障上再加一层故障。
func TestFingerprintMismatchEntersGrace(t *testing.T) {
	now := time.Now()
	m := newManagerAt(func() time.Time { return now })
	m.Load(payload(grant(nil, nil), now.AddDate(1, 0, 0), "old-fp"), "new-fp")

	if m.Status() != StatusFingerprint {
		t.Fatalf("status = %v, want finger_mismat", m.Status())
	}
	if m.ReadOnly() {
		t.Error("指纹宽限期内不该只读")
	}
}

// ★ 容量超限只提示不阻断。
func TestCapacityWarnsButNeverBlocks(t *testing.T) {
	now := time.Now()
	m := newManagerAt(func() time.Time { return now })
	m.Load(payload(grant(nil, map[string]int64{CapNodes: 1000}),
		now.AddDate(1, 0, 0), ""), "")

	ex := m.CheckCapacity(Usage{Nodes: 1500})
	if len(ex) != 1 || ex[0].Item != CapNodes {
		t.Fatalf("超限项 = %+v, want nodes", ex)
	}
	if m.ReadOnly() {
		t.Error("超限绝不能导致只读 —— 客户临时扩容不该让系统罢工")
	}
}

// 全链路拓扑刻意不在 Feature 目录里 —— 它属于 CE，是获客钩子。
// 这条测试锁住这个产品决策，防止后来的人"顺手"把它挪进 EE。
func TestTopologyStaysInCE(t *testing.T) {
	for f := range allFeatures {
		if f == "topology" || f == "full_link_topology" {
			t.Errorf("全链路拓扑不该是付费 feature —— 它是 CE 的获客钩子")
		}
	}
}
