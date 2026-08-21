package license

import (
	"ops-alert-backend/logx"
	"ops-kit/licensekit"
)

// Manager 包住共享内核，对外提供类型安全的接口。
//
// 只覆写需要类型转换的方法，其余（Load / Status / ReadOnly / Payload / Limit）直接继承。
type Manager struct {
	*licensekit.Manager
}

// NewManager 创建一个未激活的管理器，已填好本产品的能力表。
func NewManager() *Manager {
	opt := Options()
	// OnWatchError 留空会静默吞掉多副本同步错误，
	// 表现是某个副本的状态永远停在旧值（LICENSING §13.2）。
	opt.OnWatchError = func(err error) {
		logx.J("license", "watch_error", map[string]any{
			"err": err.Error(),
			"note": "授权状态同步失败，本副本保持原状不降级；" +
				"持续出现要查数据库连通性，否则该副本会一直停在旧授权状态",
		})
	}
	return &Manager{Manager: licensekit.NewManager(opt)}
}

// Has 判定某个功能是否可用。
//
// ⚠️ 判定逻辑全在内核里（授权有效性、宽限期、implemented、features 展开），
// 这一层只做类型转换 —— 不要在这里加任何判断，否则两处逻辑迟早分叉。
func (m *Manager) Has(f Feature) bool { return m.Manager.Has(string(f)) }

// Limit 某项容量的上限。0 表示不限。
func (m *Manager) Limit(c Capacity) int64 { return m.Manager.Limit(string(c)) }

// CheckCapacity 核对用量，返回所有超限项。
//
// ⚠️ 返回非空**不代表要拒绝操作**（内核注释里写死了这条）：
// 它只该被渲染成提示、计入续购报价。客户临时扩容结果系统罢工，是会丢客户的设计。
// 告警平台尤其如此 —— 因为超了 20 条规则上限就不让建第 21 条，
// 而第 21 条恰好是这次事故要加的那条，这种产品没人会续费。
func (m *Manager) CheckCapacity(usage map[Capacity]int64) []licensekit.Exceeded {
	raw := make(map[string]int64, len(usage))
	for k, v := range usage {
		raw[string(k)] = v
	}
	return m.Manager.CheckCapacity(raw)
}
