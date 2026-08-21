package license

import (
	"time"

	"ops-kit/licensekit"
	"ops-sso-backend/logx"
)

// Manager 包住共享内核，对外提供类型安全的接口。
//
// 只加本产品需要的形状（Has 的类型安全版、Report、CanWrite）；
// 状态机、宽限期、指纹判定一律继承，不要在这一层加任何判断 ——
// 两处逻辑迟早分叉，而分叉的方向通常是多给。
type Manager struct {
	*licensekit.Manager
}

// NewManager 创建一个未激活的管理器，已填好本产品的标识与能力表。
func NewManager() *Manager { return newManager(nil) }

func newManager(now func() time.Time) *Manager {
	return &Manager{
		Manager: licensekit.NewManager(licensekit.Options{
			Product:     ProductID,
			Implemented: implemented,
			// Plans 缺了的话，签 "plan:enterprise" 的 license 一个功能都展不开 ——
			// 客户付了钱、状态显示 active、Has() 全是 false，且不报任何错。
			Plans:    plans,
			CELimits: ceLimits,
			// CEWritable：本产品的社区版**必须可写**。
			//
			// 这里踩过一次：一开始未授权也不让写，结果新装的客户什么都配不了 ——
			// 连第一个应用都接不进来，装上去就是个空壳。
			// 访问判定那条路径本来就不查 license，配置面再锁死就等于产品不可用。
			CEWritable: true,
			// OnWatchError 留空会**静默吞掉**多副本同步错误，
			// 表现是某个副本的状态永远停在旧值 —— 而"授权同步坏了但没人知道"
			// 正是最难查的那类问题（LICENSING §13.2）。接 Watch 的产品都要填。
			OnWatchError: func(err error) {
				logx.J("license", "watch_error", map[string]any{
					"err": err.Error(),
					"note": "授权状态同步失败，本副本保持原状不降级；" +
						"持续出现要查数据库连通性，否则该副本会一直停在旧授权状态",
				})
			},
			Now: now,
		}),
	}
}

// Has 判定某个功能是否可用。类型安全版本，遮蔽内核的 string 版本。
//
// 三个条件由内核统一判：**代码里实现了** + license 授予了 + 授权状态允许。
// 少判任何一个都会出问题：
//   - 少判 implemented → 卖了没做的功能
//   - 少判 granted     → 分档形同虚设
//   - 少判 status      → 过期后还能用付费功能
func (m *Manager) Has(f Feature) bool { return m.Manager.Has(string(f)) }

// CanWrite 当前状态是否允许修改配置。
//
// # 未授权 ≠ 只读
//
// 未激活（社区版）是**可写**的，见 Options.CEWritable 的注释。
// 只有过期、超期、装了没买的产品才转只读。
func (m *Manager) CanWrite() bool { return !m.ReadOnly() }

// FeatureReport 一个功能对客户的呈现。
//
// 三态而不是布尔：「没做」和「没买」对客户是完全不同的两件事，
// 把没做的说成"未购买"，客户付完钱当天就会发现。
type FeatureReport struct {
	Feature     Feature `json:"feature"`
	Implemented bool    `json:"implemented"`
	Granted     bool    `json:"granted"`
	Usable      bool    `json:"usable"`
	// Reason 不可用的原因：not_implemented / not_granted / license_inactive
	Reason string `json:"reason,omitempty"`
}

// Report 如实上报每个功能的状态，供 /system/info 使用。
func (m *Manager) Report() []FeatureReport {
	feats := AllFeatures()
	out := make([]FeatureReport, 0, len(feats))
	for _, f := range feats {
		r := FeatureReport{
			Feature:     f,
			Implemented: implemented[string(f)],
			Granted:     m.Granted(string(f)),
		}
		r.Usable = m.Has(f)
		switch {
		case !r.Implemented:
			r.Reason = "not_implemented"
		case !r.Granted:
			r.Reason = "not_granted"
		case !r.Usable:
			// 实现了也授权了却不可用，只剩一种可能：授权状态不允许
			// （过期超出宽限、超期、或这份 license 不含本产品）
			r.Reason = "license_inactive"
		}
		out = append(out, r)
	}
	return out
}
