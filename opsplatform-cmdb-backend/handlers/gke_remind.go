// GKE 升级提醒：把「会被自动升级」提前变成日程上的待办。
//
// 五条触发规则（计划 §五）：
//  1. 预计自动升级 T-30
//  2. 预计自动升级 T-7
//  3. autoUpgradeStartTime 首次出现 —— 官方只在升级即将开始时才填，是最后的拦截机会
//  4. 维护排除到期前 T-30 —— 否则排除期一过就被自动升，等于白挡
//  5. 当前小版本标准支持截止 T-30 —— 硬期限兜底
//
// ⚠️ 日期粒度必须带进文案。官网对未来版本只给到月/季度（如 2026-09、2026-Q4），
// 我们为排序归一化到了首日；直接说「还有 N 天」会系统性提前（季度粒度最多 89 天），
// 所以非 day 粒度一律用「最早 N 天 / 2026 年 9 月内」的说法。
package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"opsplatform-cmdb-backend/logx"
	"opsplatform-cmdb-backend/notify"
)

type gkeRemindItem struct {
	Cluster string
	Env     string
	Rule    string // 规则名，用于日志和去重
	Urgent  bool
	Lines   []string
}

// gkeUpgradeRemindCore 每天跑一次，把命中规则的集群汇总成一张卡片发出去。
func gkeUpgradeRemindCore(db *sql.DB) (string, []TaskFailure, bool) {
	today := localMidnight()
	rows, err := db.Query(`
		SELECT c.name, COALESCE(NULLIF(c.display_name,''), c.name), c.environment, c.provider,
		       COALESCE(u.release_channel,''), COALESCE(u.current_master_version,''),
		       COALESCE(u.minor_target_version,''), u.predicted_upgrade_at,
		       COALESCE(u.predicted_precision,''), COALESCE(u.predicted_source,''),
		       COALESCE(u.auto_upgrade_status,''), COALESCE(u.paused_reason,''),
		       u.eos_standard_at, COALESCE(u.maintenance_policy_json,'')
		  FROM k8s_clusters c
		  JOIN gke_cluster_upgrade u ON u.cluster_id=c.id
		 WHERE c.provider='gke' AND c.enabled=1
		 ORDER BY FIELD(c.environment,'PROD','UAT','TEST','DEV'), c.name`)
	if err != nil {
		return "查询失败：" + err.Error(), []TaskFailure{{Target: "db", Reason: err.Error()}}, false
	}
	defer rows.Close()

	items := []gkeRemindItem{}
	for rows.Next() {
		var name, disp, env, provider, channel, master, minorTarget string
		var precision, source, status, paused, maintJSON string
		var predAt, eosAt sql.NullString
		if rows.Scan(&name, &disp, &env, &provider, &channel, &master, &minorTarget,
			&predAt, &precision, &source, &status, &paused, &eosAt, &maintJSON) != nil {
			continue
		}
		kind, _ := classifyPause(paused)
		it := gkeRemindItem{Cluster: disp, Env: env}

		// 规则 5：支持截止是硬期限，优先级最高
		if eos := dateStr(eosAt); eos != "" {
			if d := daysUntil(eos, today); d != nil && *d >= 0 && *d <= 30 {
				it.Rule, it.Urgent = "eos_t30", true
				it.Lines = append(it.Lines,
					fmt.Sprintf("当前版本 %s 的标准支持 %s 到期（还有 %d 天）——硬期限，之后不再有安全补丁", master, eos, *d))
			}
		}

		// 规则 1/2：预计自动升级 T-30 / T-7。用区间最早端做保守判定
		if p := dateStr(predAt); p != "" {
			start, _ := dateWindow(p, precision)
			if d := daysUntil(start, today); d != nil && *d >= 0 && *d <= 30 {
				when := fmt.Sprintf("%d 天后（%s 起）", *d, p)
				if precision != "day" {
					when = fmt.Sprintf("最早 %d 天后（%s，官网只给到%s粒度）", *d, windowText(p, precision), precisionCN(precision))
				}
				tag := "T-30"
				if *d <= 7 {
					tag, it.Urgent = "T-7", true
				}
				if kind == "excluded" {
					// 已被维护排除挡住的，不必按 T-30 催——规则 4 会盯排除期本身
					logx.J("gke_remind", "skip_blocked", map[string]any{"cluster": name, "paused": paused})
				} else {
					if it.Rule == "" {
						it.Rule = tag
					}
					target := minorTarget
					if target == "" {
						target = "（GKE 未排期，按当前版本下一个小版本推断）"
					}
					it.Lines = append(it.Lines,
						fmt.Sprintf("%s → %s，预计%s自动升级；通道 %s", master, target, when, channelOr(channel)))
					if source == "inferred_next_minor" {
						it.Lines = append(it.Lines, "  ⚠ 目标版本是推断值：GKE 尚未排期，按当前小版本 +1 估算")
					}
				}
			}
		}

		// 规则 4：维护排除到期前 T-30——排除期一过就恢复自动升级
		if kind == "excluded" {
			if end, d := exclusionEnd(maintJSON, today); end != "" && d != nil && *d >= 0 && *d <= 30 {
				if it.Rule == "" {
					it.Rule = "exclusion_t30"
				}
				it.Lines = append(it.Lines,
					fmt.Sprintf("维护排除 %s 到期（还有 %d 天），之后会恢复自动升级，需在此前完成计划内升级", end, *d))
			}
		}

		if len(it.Lines) > 0 {
			items = append(items, it)
		}
	}

	if len(items) == 0 {
		return "没有需要提醒的集群（30 天内无自动升级、无支持到期、无排除期临近）", nil, true
	}

	sent := sendUpgradeRemind(db, items)
	summary := fmt.Sprintf("%d 个集群命中提醒规则，飞书投递：%s", len(items), sent)
	logx.J("gke_remind", "done", map[string]any{"items": len(items), "delivery": sent})
	return summary, nil, true
}

func channelOr(c string) string {
	if c == "" || c == "UNSPECIFIED" {
		return "未入通道（按 Stable 排期）"
	}
	return c
}

// exclusionEnd 从 maintenancePolicy JSON 里找最晚的维护排除结束时间。
func exclusionEnd(maintJSON string, today time.Time) (string, *int) {
	if maintJSON == "" {
		return "", nil
	}
	var mp struct {
		Window struct {
			MaintenanceExclusions map[string]struct {
				EndTime string `json:"endTime"`
			} `json:"maintenanceExclusions"`
		} `json:"window"`
	}
	if json.Unmarshal([]byte(maintJSON), &mp) != nil {
		return "", nil
	}
	latest := ""
	for _, ex := range mp.Window.MaintenanceExclusions {
		if len(ex.EndTime) >= 10 && ex.EndTime[:10] > latest {
			latest = ex.EndTime[:10]
		}
	}
	if latest == "" {
		return "", nil
	}
	return latest, daysUntil(latest, today)
}

func sendUpgradeRemind(db *sql.DB, items []gkeRemindItem) string {
	webhook, group := larkWebhookForTask(db, "gke_upgrade_remind")
	if webhook == "" {
		logx.J("gke_remind", "no_group", map[string]any{
			"note": "gke_upgrade_remind 未配飞书群，提醒只进日志和任务记录",
		})
		return "未配置群"
	}
	urgent := 0
	for _, it := range items {
		if it.Urgent {
			urgent++
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "⚠️ GKE 自动升级预警（%d 个集群", len(items))
	if urgent > 0 {
		fmt.Fprintf(&b, "，其中 %d 个紧急", urgent)
	}
	b.WriteString("）\n")
	for _, it := range items {
		icon := "🟡"
		if it.Urgent {
			icon = "🔴"
		}
		fmt.Fprintf(&b, "\n%s %s（%s）\n", icon, it.Cluster, it.Env)
		for _, l := range it.Lines {
			fmt.Fprintf(&b, "   %s\n", l)
		}
	}
	b.WriteString("\n建议：先在非生产环境验证目标版本，生产集群设维护排除挡住后再计划内手动升级。")
	if err := notify.SendFeishu(webhook, b.String()); err != nil {
		logx.J("gke_remind", "send_failed", map[string]any{"group": group, "err": err.Error()})
		return "投递失败：" + err.Error()
	}
	return "已发送到 " + group
}
