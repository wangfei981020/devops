package licensekit

import (
	"strings"
	"time"
)

// 功能授权的声明语法。规范见 ops/LICENSING.md §2。
//
//	plan:enterprise   档次打底，展开成产品声明的 Plans["enterprise"]
//	+scim             单项加购
//	-branding         单项排除
//	scim              裸名，等同 +scim（兼容早期的显式清单写法）
const (
	planPrefix   = "plan:"
	grantPrefix  = "+"
	revokePrefix = "-"
)

// resolveFeatures 把 license 里的 features 声明展开成实际生效的功能集合。
//
// # 为什么签档次而不是签清单
//
// 签清单的话，每加一个新功能，所有已购该档次的客户都要**重签一次 license**
// 才能拿到——而重签意味着重新走一遍合同、邮件、粘贴、验证。
// 签档次之后，新功能加进 Plans 映射 → 随二进制发布 → 老客户升级即得。
//
// # 求值顺序
//
//  1. plan: 展开、裸名、+ 加购  —— 都是并集，顺序无关
//  2. - 排除                    —— **最后统一应用，永远赢**
//
// 第 2 步单独放在最后，是为了让 `["plan:enterprise", "-branding"]` 这种写法
// 无论 `-branding` 写在哪个位置，结论都一样。
// 如果边遍历边增删，`["-branding", "plan:enterprise"]` 会得到相反的结果——
// 而"调换两个元素的顺序改变了授权范围"是没人查得出来的那类 bug。
//
// # 声明了未知档次会怎样
//
// 得到空集，**不是全给**。产品没声明 `Plans["enterprise"]` 却收到
// `plan:enterprise`，说明 license 和二进制对不上（多半是产品版本太老）。
// 这时给空集会让客户看到"买了没生效"来问我们，而给全集是静默地多给。
// 前者是可修的，后者是收不回的。
func resolveFeatures(declared []string, plans map[string][]string) map[string]bool {
	granted := make(map[string]bool, len(declared))
	var revoked []string

	for _, raw := range declared {
		item := strings.TrimSpace(raw)
		if item == "" {
			continue
		}
		switch {
		case strings.HasPrefix(item, planPrefix):
			for _, f := range plans[strings.TrimPrefix(item, planPrefix)] {
				granted[f] = true
			}
		case strings.HasPrefix(item, revokePrefix):
			revoked = append(revoked, strings.TrimPrefix(item, revokePrefix))
		case strings.HasPrefix(item, grantPrefix):
			granted[strings.TrimPrefix(item, grantPrefix)] = true
		default:
			// 裸名：兼容早期直接列功能清单的写法
			granted[item] = true
		}
	}

	for _, f := range revoked {
		delete(granted, f)
	}
	return granted
}

// applyEntitlement 按维护期裁掉客户"还没资格拿"的新功能。
//
// # 为什么要这个
//
// 永久 license（Perpetual）如果不加约束，2026 年买的会在 2030 年白拿四年新功能。
// 商业软件的标准做法是 **license 到期 ≠ 维护期到期**：
//
//	ExpiresAt     功能还能不能用       到了转只读
//	EntitledUntil 能拿到哪一批新功能   到了之后已有功能照常，新功能不再展开
//
// # 零值 = 不限制
//
// `entitledUntil` 为零值时不做任何裁剪。这是为了兼容——没有这个字段的老 token
// 解出来就是零值，不能让它们突然什么都拿不到。
//
// ⚠️ 失效方向是"多给"：签发时忘了填 EntitledUntil，客户就能拿到所有新功能。
// 所以 LICENSING.md §8 要求**签发台必须始终显式设置它**，
// 而不是依赖这里的默认值。这条约束在签发侧，共享库这边只能保证不误伤老 token。
//
// featureSince 里查不到的 feature 视为"一直就有"（零值时间早于任何维护期），
// 一律放行——新增功能时忘了登记引入日期，结果是老客户多拿到它，
// 与上面同向，同样由签发/上线检查清单兜底。
func applyEntitlement(granted map[string]bool, featureSince map[string]time.Time, entitledUntil time.Time) map[string]bool {
	if entitledUntil.IsZero() || len(featureSince) == 0 {
		return granted
	}
	for f := range granted {
		if since, ok := featureSince[f]; ok && since.After(entitledUntil) {
			delete(granted, f)
		}
	}
	return granted
}
