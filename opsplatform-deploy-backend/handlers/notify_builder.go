package handlers

import (
	"fmt"
	"strings"
	"time"

	"opsplatform-deploy-backend/models"
	"opsplatform-deploy-backend/services"
)

// =========================================================================
//   Lark 发布通知 body 构造器
//
// 用户约定的最终格式（见 PR 讨论）：
//   - 标题：发布/重启/回滚 + 成功/失败/部分成功/无变更
//   - 名称: {operator username，不带 @}
//   - 更新时间: {now}
//   - 分组顺序：成功 → 跳过 → 失败 → @艾特
//   - 每个模块独立一块（容器名 / 命名空间 / 版本号 / 失败原因）
//   - 不论多少模块全部列出（30/50 个也不省略）
// =========================================================================

type deployNotifyItem struct {
	Module    string // 容器名（模块名）
	Namespace string // k8s namespace（来自 module.namespace，扫描时从 apps/values.yaml 读）
	FromTag   string // 旧 tag；restart 不适用
	ToTag     string // 新 tag（restart 就填当前 tag）
	FailMsg   string // 失败原因，只对 failed 组有意义
}

// buildDeployNotifyBody 构造 Lark 卡片正文
//
//	opLabel: "发布" / "重启" / "回滚"
//	atLarkID: 空串 = 不艾特；否则在 body 末尾插入 <at id="...">
//
// 返回 title / color / body，供 services.SendLarkCard 使用。
func buildDeployNotifyBody(
	opLabel, operator, atLarkID string,
	successes, skippeds, faileds []deployNotifyItem,
) (title, color, body string) {
	n := len(successes) + len(skippeds) + len(faileds)
	nS, nK, nF := len(successes), len(skippeds), len(faileds)

	switch {
	case n == 0:
		title = fmt.Sprintf("%s · 0 个模块", opLabel)
		color = "blue"
	case nF == 0 && nS == 0 && nK > 0:
		title = fmt.Sprintf("ℹ️ 无变更 · %d 个模块都已是目标版本", n)
		color = "blue"
	case nF > 0 && nS == 0:
		title = fmt.Sprintf("❌ %s失败 · %d 个模块", opLabel, n)
		color = "red"
	case nF == 0:
		title = fmt.Sprintf("✅ %s成功 · %d 个模块", opLabel, n)
		color = "green"
	default:
		title = fmt.Sprintf("⚠️ %s部分成功 · %d 个模块（%d 失败 / %d 跳过 / %d 成功）",
			opLabel, n, nF, nK, nS)
		color = "orange"
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "**名称**: %s\n", safeStr(operator))
	fmt.Fprintf(&sb, "**更新时间**: %s\n\n", time.Now().Format("2006-01-02 15:04:05"))

	writeSection := func(emoji, label string, items []deployNotifyItem, showReason bool) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(&sb, "━━━━━━ %s (%d) ━━━━━━\n", label, len(items))
		for _, it := range items {
			fmt.Fprintf(&sb, "%s **容器名**: %s\n", emoji, it.Module)
			fmt.Fprintf(&sb, "   **命名空间**: %s\n", safeStr(it.Namespace))
			switch {
			case it.FromTag != "" && it.ToTag != "" && it.FromTag != it.ToTag:
				fmt.Fprintf(&sb, "   **版本号**: %s → %s\n", it.FromTag, it.ToTag)
			case it.ToTag != "" && it.FromTag == it.ToTag:
				// 发布输入 tag 与当前一致 → 跳过组
				fmt.Fprintf(&sb, "   **版本号**: %s（当前版本相同，跳过）\n", it.ToTag)
			case it.ToTag != "":
				// restart 场景，只显示当前 tag
				fmt.Fprintf(&sb, "   **版本号**: %s\n", it.ToTag)
			}
			if showReason && it.FailMsg != "" {
				fmt.Fprintf(&sb, "   **失败原因**: %s\n", it.FailMsg)
			}
			sb.WriteString("\n")
		}
	}

	// 固定顺序：成功 → 跳过 → 失败 → @艾特（最紧急贴着 @ 一起，@ 驱动阅读时第一眼看到失败）
	writeSection("✓", "成功", successes, false)
	writeSection("⏭️", "跳过", skippeds, false)
	writeSection("❌", "失败", faileds, true)

	if atLarkID != "" {
		if nF > 0 {
			fmt.Fprintf(&sb, `<at id="%s"></at> 请检查失败模块`, atLarkID)
		} else {
			fmt.Fprintf(&sb, `<at id="%s"></at>`, atLarkID)
		}
	}

	body = sb.String()
	return
}

func safeStr(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// findModuleByArgocdApp 从 modules map 里反查 module name / namespace / current_tag
// argocd_app_name（如 g50-xxx-g50-uat）是 restart/update 都使用的 app ID
func findModuleByArgocdApp(modules map[string]services.Module, appName string) (mod, ns, tag string) {
	for name, m := range modules {
		if m.ArgocdApp == appName {
			return name, m.Namespace, m.CurrentTag
		}
	}
	return appName, "", ""
}

// buildRestartNotifyItems 把 RestartResult 拆成成功/失败两组
func buildRestartNotifyItems(
	modules map[string]services.Module,
	res *services.RestartResult,
) (successes, faileds []deployNotifyItem) {
	for _, ar := range res.ArgocdResults {
		modName, ns, tag := findModuleByArgocdApp(modules, ar.App)
		item := deployNotifyItem{
			Module:    modName,
			Namespace: ns,
			ToTag:     tag, // restart 显示当前 tag
		}
		if ar.SyncStatus == "Synced" && ar.Health == "Healthy" {
			successes = append(successes, item)
		} else {
			item.FailMsg = ar.Msg
			if item.FailMsg == "" {
				item.FailMsg = fmt.Sprintf("%s / %s", ar.SyncStatus, ar.Health)
			}
			faileds = append(faileds, item)
		}
	}
	return
}

// buildUpdateNotifyItems 把 UpdateImageResult 拆成成功/跳过/失败三组
// UpdateImage 和 Rollback 都用这个。
func buildUpdateNotifyItems(
	modules map[string]services.Module,
	res *services.UpdateImageResult,
) (successes, skippeds, faileds []deployNotifyItem) {
	// 按 module 名索引 Changes（拿 from/to tag）
	changesByMod := map[string]models.Change{}
	for _, c := range res.Changes {
		changesByMod[c.Module] = c
	}

	// ArgocdResults 每个对应一个改动的 app；成功/失败从这里分
	for _, ar := range res.ArgocdResults {
		modName, ns, _ := findModuleByArgocdApp(modules, ar.App)
		item := deployNotifyItem{
			Module:    modName,
			Namespace: ns,
		}
		if c, ok := changesByMod[modName]; ok {
			item.FromTag = c.FromTag
			item.ToTag = c.ToTag
		}
		if ar.SyncStatus == "Synced" && ar.Health == "Healthy" {
			successes = append(successes, item)
		} else {
			item.FailMsg = ar.Msg
			if item.FailMsg == "" {
				item.FailMsg = fmt.Sprintf("%s / %s", ar.SyncStatus, ar.Health)
			}
			faileds = append(faileds, item)
		}
	}

	// Skipped：版本相同的，直接显示当前 tag
	for _, name := range res.Skipped {
		m := modules[name]
		skippeds = append(skippeds, deployNotifyItem{
			Module:    name,
			Namespace: m.Namespace,
			FromTag:   m.CurrentTag, // FromTag == ToTag 触发"版本相同，跳过"格式
			ToTag:     m.CurrentTag,
		})
	}

	return
}
