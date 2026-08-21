package license

import (
	"time"

	"ops-cmdb-backend/logx"
	"ops-kit/licensekit"
)

// Manager 包住共享内核，对外提供类型安全的接口。
//
// 只覆写需要类型转换的方法（Has / Capacity / CheckCapacity），
// 其余（Load / Status / ReadOnly / Payload / Limit）直接继承。
type Manager struct {
	*licensekit.Manager
}

// NewManager 创建一个未激活的管理器，已填好本产品的标识与能力表。
func NewManager() *Manager { return newManager(nil) }

// newManagerAt 注入时间源，仅测试用（宽限期逻辑依赖"现在几点"）。
func newManagerAt(now func() time.Time) *Manager { return newManager(now) }

func newManager(now func() time.Time) *Manager {
	return &Manager{
		Manager: licensekit.NewManager(licensekit.Options{
			Product:     ProductID,
			Implemented: implemented,
			// Plans 缺了的话，签 "plan:enterprise" 的 license 一个功能都展不开 ——
			// 客户付了钱、状态显示 active、Has() 全是 false，且不报任何错。
			Plans:    plans,
			CELimits: ceLimits,
			// CEWritable：CMDB 的社区版**必须可写**。
			//
			// 这不是慷慨，是产品能不能用的问题：CMDB 的第一步是配数据源、纳管集群，
			// 这些都是写操作。只读的 CE 连一台机器都采不到，装上去是个空壳，
			// 试用者第一分钟就走了。
			//
			// 与「资产台账只读也有用」不矛盾 —— 那说的是**已经有数据之后**，
			// 而 CE 用户是从零开始的。
			CEWritable: true,
			// OnWatchError 留空会静默吞掉多副本同步错误，
			// 表现是某个副本的状态永远停在旧值（LICENSING §13.2）。
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
// 判定逻辑全在内核里（授权有效性、宽限期、implemented 表、features 列表），
// 这一层只做类型转换 —— 不要在这里加任何判断，否则两处逻辑迟早分叉。
func (m *Manager) Has(f Feature) bool { return m.Manager.Has(string(f)) }

// Capacity 本产品的容量上限。
//
// 保留结构体是为了编译期检查：写错字段名编译不过，
// 而写错 map 的 key 只会静默返回 0（"不限"）—— 那是危险的失效方向。
type Capacity struct {
	Nodes         int64 `json:"nodes"`
	Clusters      int64 `json:"clusters"`
	CloudAccounts int64 `json:"cloud_accounts"`
	Seats         int64 `json:"seats"`
	Tenants       int64 `json:"tenants"`
	RetentionDays int64 `json:"retention_days"`
}

// Capacity 取当前生效的容量上限（未激活时是 CE 默认值）。
func (m *Manager) Capacity() Capacity {
	return Capacity{
		Nodes:         m.Limit(CapNodes),
		Clusters:      m.Limit(CapClusters),
		CloudAccounts: m.Limit(CapCloudAccounts),
		Seats:         m.Limit(CapSeats),
		Tenants:       m.Limit(CapTenants),
		RetentionDays: m.Limit(CapRetentionDays),
	}
}

// Usage 当前用量，用于核对容量。
type Usage struct {
	Nodes         int64
	Clusters      int64
	CloudAccounts int64
	Seats         int64
	Tenants       int64
	// RetentionDays 当前配置的数据保留天数。**只在 RetentionKnown 为真时有意义。**
	//
	//	保留天数的授权上限（CE 30 天 / 企业档 3650 天）原来**从来没有被校验过** ——
	//	Usage 里压根没有这一项，于是 CheckCapacity 拿不到它。
	//	而实测生产配的是 0（永不清理），已经超出任何有限上限，
	//	但授权页的 `exceeded` 是空的、这一项还渲染成「—」（OPSCMDB-031 P1-73）。
	RetentionDays int64
	// RetentionUnlimited 保留策略是不是"永不清理"。
	//
	//	⚠️ **必须显式声明，不能从 `RetentionDays == 0` 推断。**
	//
	//	第一版我就是从 0 推断的，结果任何**没填这一项**的 Usage
	//	（比如只关心节点数的调用方、以及已有的单测）都被判成"永不清理超限" ——
	//	一条已有测试当场红了，那正是这个设计的真实副作用：
	//	**0 同时表示"没填"和"无限"两件事，代码分不出来。**
	//
	//	与「永久授权零值被判过期」是同一族问题：
	//	0 在这个产品里表示"不限"，但各处对它的处理不一致。
	//	解法不是"记住 0 的含义"，而是让语义**有自己的字段**。
	RetentionUnlimited bool
	// RetentionKnown 这次有没有取到保留配置。false = 不参与容量校验。
	//
	//	三态：不知道 / 有限且是 N 天 / 无限。
	//	把"不知道"和"0 天"压在一起，就是上面那个 bug。
	RetentionKnown bool
}

// retentionForCompare 保留天数参与容量比较时用的值。
//
//	无限用一个足够大的数表示，让它超过任何有限上限。
//	⚠️ 不用 math.MaxInt64：那个数会被原样渲染到界面上
//	（`exceeded` 里的 current 是要显示的），一串 9223372036854775807
//	既难读又像是出了 bug。取一个明显是哨兵、又一眼能认出"很大"的值。
const retentionUnlimitedSentinel int64 = 999999

// CheckCapacity 核对用量，返回所有超限项。
//
// **返回非空不代表要拒绝操作** —— 它只该被渲染成提示，并计入续购报价。
// 客户临时扩容 20 台节点结果系统罢工，是会丢客户的设计。
func (m *Manager) CheckCapacity(u Usage) []Exceeded {
	items := map[string]int64{
		CapNodes:         u.Nodes,
		CapClusters:      u.Clusters,
		CapCloudAccounts: u.CloudAccounts,
		CapSeats:         u.Seats,
		CapTenants:       u.Tenants,
	}
	// ⚠️ 只在**确实取到**保留配置时才校验这一项。
	//
	//	取不到就不放进 items —— 不放进去等于不校验，
	//	而放一个 0 进去会被当成"零天"（合规），放哨兵值会误报超限。
	//	"不知道"必须是第三种状态，不能挤进另外两种里。
	unlimited := map[string]bool{}
	if u.RetentionKnown {
		if u.RetentionUnlimited {
			// ⚠️ 哨兵值只用于比较，**不能**当成事实值发给调用方 ——
			//	`{"current": 999999}` 与同一响应里的 `retention_unlimited: true`
			//	是同一件事的两种编码，读的人只会判断"其中一处是 bug"（OPSCMDB-073）
			unlimited[CapRetentionDays] = true
			// 永不清理：语义上无限，超过任何有限上限。
			// 这恰恰是最该被标出来的那种 —— 数据只增不减迟早撑满盘，
			// 而授权卖的就是保留期
			items[CapRetentionDays] = retentionUnlimitedSentinel
		} else {
			items[CapRetentionDays] = u.RetentionDays
		}
	}
	return m.Manager.CheckCapacityEx(items, unlimited)
}
