package license

import (
	"testing"
	"time"
)

// 保留天数的容量校验。
//
// 守的是 OPSCMDB-031 P1-73：授权页「数据保留天数」渲染成「—」，
// 而实际配置是 0（永不清理），已经超出授权上限 3650 天，
// 但 `usage` 里根本没有这一项、`exceeded` 是空的 ——
// **这一项的上限从来没有被校验过**。
//
// ⚠️ 核心是三态，不是两态：
//
//	不知道（没取到配置）  → 不参与校验
//	有限，N 天           → 和上限比
//	无限（永不清理）      → 超过任何有限上限
//
// 把"不知道"和"0 天"压在一起，就会出现两种相反的错：
// 从 0 推断无限 → 任何不填这一项的调用方都被误报超限（我第一版就这样，
// 已有的 TestCapacityWarnsButNeverBlocks 当场红了）；
// 把 0 当零天 → "永不清理"被判成合规，也就是 P1-73 的原状。
func TestRetentionCapacityThreeStates(t *testing.T) {
	now := time.Now()
	mk := func() *Manager {
		m := newManagerAt(func() time.Time { return now })
		m.Load(payload(grant(nil, map[string]int64{CapRetentionDays: 3650}),
			now.AddDate(1, 0, 0), ""), "")
		return m
	}

	t.Run("不知道 → 不参与校验", func(t *testing.T) {
		// RetentionKnown 为 false（零值）—— 调用方没关心这一项
		ex := mk().CheckCapacity(Usage{Nodes: 1})
		for _, e := range ex {
			if e.Item == CapRetentionDays {
				t.Fatalf("没取到保留配置却校验了它：%+v", e)
			}
		}
	})

	t.Run("有限且在上限内 → 不超限", func(t *testing.T) {
		ex := mk().CheckCapacity(Usage{RetentionDays: 365, RetentionKnown: true})
		for _, e := range ex {
			if e.Item == CapRetentionDays {
				t.Fatalf("365 天在 3650 上限内，不该超限：%+v", e)
			}
		}
	})

	t.Run("有限但超上限 → 超限", func(t *testing.T) {
		ex := mk().CheckCapacity(Usage{RetentionDays: 5000, RetentionKnown: true})
		found := false
		for _, e := range ex {
			if e.Item == CapRetentionDays {
				found = true
				if e.Current != 5000 || e.Limit != 3650 {
					t.Errorf("超限项数值不对：%+v", e)
				}
			}
		}
		if !found {
			t.Error("5000 天超过 3650，应报超限")
		}
	})

	t.Run("永不清理 → 超过任何有限上限", func(t *testing.T) {
		// 🔴 这一条是 P1-73 的原型：生产实测配的就是 0
		ex := mk().CheckCapacity(Usage{RetentionDays: 0, RetentionUnlimited: true, RetentionKnown: true})
		found := false
		for _, e := range ex {
			if e.Item == CapRetentionDays {
				found = true
				if e.Current <= e.Limit {
					t.Errorf("「永不清理」的 current 应当大于上限，实际 %+v", e)
				}
			}
		}
		if !found {
			t.Error("「永不清理」语义上无限，超过 3650 天上限，必须报超限 —— 这正是 P1-73")
		}
	})
}

// 哨兵值不能大到没法读。
//
// exceeded 里的 current 是要**渲染到界面上**的，
// 用 math.MaxInt64 会显示成 9223372036854775807 —— 既难读又像出了 bug。
func TestRetentionSentinelIsReadable(t *testing.T) {
	if retentionUnlimitedSentinel <= 3650 {
		t.Error("哨兵值必须大于任何合理的保留上限，否则「永不清理」判不出超限")
	}
	if retentionUnlimitedSentinel > 9_999_999 {
		t.Errorf("哨兵值 %d 太大，渲染到界面上没法读", retentionUnlimitedSentinel)
	}
}

// TestUnlimitedIsFlaggedNotRenderedAsSentinel 「无限」必须**自报身份**，
// 不能靠一个魔数被读的人猜。
//
// 🔴 生产实测（v0.122.0，OPSCMDB-073）：同一份 /api/license 响应里
//
//	usage.retention_unlimited: true        ← 一种编码
//	exceeded[0].current: 999999            ← 另一种编码，同一件事
//
// 判定本身是对的（永不清理确实超过任何有限保留期上限），
// 错的是把只该用于比较的哨兵值当成事实值发出去。
// 读这份 JSON 的多半是 AI 或脚本，它们只能得出"其中一处是 bug"。
func TestUnlimitedIsFlaggedNotRenderedAsSentinel(t *testing.T) {
	now := time.Now()
	m := newManagerAt(func() time.Time { return now })
	m.Load(payload(grant(nil, map[string]int64{CapRetentionDays: 3650, CapNodes: 100}),
		now.AddDate(1, 0, 0), ""), "")

	ex := m.CheckCapacity(Usage{RetentionUnlimited: true, RetentionKnown: true, Nodes: 500})

	var ret, nodes *Exceeded
	for i := range ex {
		switch ex[i].Item {
		case CapRetentionDays:
			ret = &ex[i]
		case CapNodes:
			nodes = &ex[i]
		}
	}
	if ret == nil {
		t.Fatal("永不清理超过 3650 天上限，必须报超限")
	}
	if !ret.Unlimited {
		t.Errorf("超限项没标 Unlimited，调用方只能看到 current=%d 这个魔数", ret.Current)
	}
	// 反向：真实的有限用量绝不能被标成无限，否则界面会把 500 个节点显示成"不限"
	if nodes == nil {
		t.Fatal("500 > 100，节点数该超限")
	}
	if nodes.Unlimited {
		t.Errorf("有限用量被标成无限了：%+v", nodes)
	}
	// 反向：有限的保留天数同样不能被标成无限
	ex2 := m.CheckCapacity(Usage{RetentionDays: 5000, RetentionKnown: true})
	for _, e := range ex2 {
		if e.Item == CapRetentionDays && e.Unlimited {
			t.Errorf("5000 天是有限值，不该标无限：%+v", e)
		}
	}
}
