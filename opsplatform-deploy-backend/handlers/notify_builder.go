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
//   - 操作人: {operator username，不带 @}
//   - 更新时间: {now}
//   - 分组顺序：成功 → 跳过 → 失败 → @艾特
//   - 每个模块独立一块（容器名 / 命名空间 / 版本号 / 失败原因）
//   - 不论多少模块全部列出（30/50 个也不省略）
// =========================================================================

type deployNotifyItem struct {
	Module    string   // 容器名（模块名）
	Namespace string   // k8s namespace（来自 module.namespace，扫描时从 apps/values.yaml 读）
	FromTag   string   // 旧 tag；restart 不适用
	ToTag     string   // 新 tag（restart 就填当前 tag）
	Domains   []string // 访问域名（前端模块才有；多个逐行列，空则不显示这段）
	FailMsg   string   // 失败原因，只对 failed 组有意义
}

// buildDeployNotifyBody 构造**单一类型** Lark 卡片正文（要么纯成功 / 跳过 / 失败之一）。
//
//	opLabel: "发布" / "重启" / "回滚"
//	kind: "success" / "skip" / "fail"，决定标题、颜色、emoji
//	envName / envType: 项目环境名（如 "g33-uat"）/ 类型（"uat" / "prod"）—— 标题与 body 都展示
//	  生产用同一 namespace 部署 uat/prod 时，靠这两个字段区分（namespace 一样看不出来）。
//	  envType 在 body 里以大写括号呈现：g33-uat (UAT) / g33-prod (PROD)。
//	atLarkID: 空串 = 不艾特；否则 body 末尾追加 <at id="...">（成功 / 失败都艾特）
//
// **partial 场景由调用方拆成两次调用**（一次 success，一次 fail），每次卡片只关心一类。
//
// 失败原因（FailMsg）已从卡片移除，运维同学要看详情按"查看发布详情"按钮跳前端。
func buildDeployNotifyBody(
	opLabel, operator, atLarkID, kind, envName, envType string,
	items []deployNotifyItem,
) (title, color, body string) {
	n := len(items)
	if n == 0 {
		// 调用方应保证 items 非空才进来，这里只是兜底
		title = fmt.Sprintf("%s · 0 个模块", opLabel)
		color = "blue"
		return
	}

	// 标题加 [envName] 前缀，让群里通知列表一眼看出是哪个 env 的发布
	envPrefix := ""
	if envName != "" {
		envPrefix = "[" + envName + "] "
	}

	var emoji, sectionLabel string
	switch kind {
	case "fail":
		title = fmt.Sprintf("❌ %s%s失败 · %d 个模块", envPrefix, opLabel, n)
		color = "red"
		emoji = "❌"
		sectionLabel = "失败"
	case "skip":
		title = fmt.Sprintf("ℹ️ %s无变更 · %d 个模块都已是目标版本", envPrefix, n)
		color = "blue"
		emoji = "⏭️"
		sectionLabel = "跳过"
	default: // "success"
		title = fmt.Sprintf("✅ %s%s成功 · %d 个模块", envPrefix, opLabel, n)
		color = "green"
		emoji = "✓"
		sectionLabel = "成功"
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "**操作人**: %s\n", safeStr(operator))
	if envName != "" {
		// envType 大写括号显式区分 uat/prod（同 namespace 时唯一区分依据）
		envLine := envName
		if envType != "" {
			envLine = fmt.Sprintf("%s (%s)", envName, strings.ToUpper(envType))
		}
		fmt.Fprintf(&sb, "**项目环境**: %s\n", envLine)
	}
	fmt.Fprintf(&sb, "**更新时间**: %s\n\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&sb, "━━━━━━ %s (%d) ━━━━━━\n", sectionLabel, n)
	for _, it := range items {
		fmt.Fprintf(&sb, "%s **容器名**: %s\n", emoji, it.Module)
		fmt.Fprintf(&sb, "   **命名空间**: %s\n", safeStr(it.Namespace))
		if it.ToTag != "" {
			if it.FromTag == it.ToTag && it.FromTag != "" {
				fmt.Fprintf(&sb, "   **版本号**: %s（当前版本相同，跳过）\n", it.ToTag)
			} else {
				fmt.Fprintf(&sb, "   **版本号**: %s\n", it.ToTag)
			}
		}
		// 访问域名（前端模块才有；多个逐行列，无则不显示这段）
		if len(it.Domains) > 0 {
			fmt.Fprintf(&sb, "   **域名**: %s\n", it.Domains[0])
			for _, d := range it.Domains[1:] {
				fmt.Fprintf(&sb, "         %s\n", d)
			}
		}
		sb.WriteString("\n")
	}

	// 一律 @ 操作人（不论成功失败，都让责任人知道）
	if atLarkID != "" {
		switch kind {
		case "fail":
			fmt.Fprintf(&sb, `<at id="%s"></at> 请检查失败模块`, atLarkID)
		case "skip":
			fmt.Fprintf(&sb, `<at id="%s"></at>`, atLarkID)
		default:
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
