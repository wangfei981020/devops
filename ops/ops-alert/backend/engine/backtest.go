package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"ops-alert-backend/datasource"
	"ops-alert-backend/internal/store"
	"ops-alert-backend/logx"
)

// 回放（backtest）：把一份规则草稿放到过去 N 天的数据上跑一遍，
// 回答三个问题——真实故障还抓不抓得到、噪音降了多少、深夜会不会叫醒人。
//
// # 为什么这是沙箱
//
// 回放绝不产生事件、不发通知、不碰 rule_states。这一点必须在代码里强制，
// 不能靠调用方自觉：一次"顺手复用了执行路径"的重构就会让回放开始真发告警，
// 而那是在半夜把 30 天的历史告警一次性发出去。

// BacktestDraft 是被回放的规则草稿。
type BacktestDraft struct {
	DatasourceID int64    `json:"datasource_id"`
	Query        string   `json:"query"`
	LookbackSec  int      `json:"lookback_sec"`
	Threshold    int      `json:"threshold"`
	ForPeriods   int      `json:"for_periods"`
	IntervalSec  int      `json:"interval_sec"`
	GroupBy      []string `json:"group_by"`
	Absent       bool     `json:"absent"`
}

// BacktestResult 是回放结论。
type BacktestResult struct {
	Windows      int            `json:"windows"`       // 回放了多少个周期
	Fires        int            `json:"fires"`         // 会触发多少次
	PerDay       map[string]int `json:"per_day"`       // 按天分布
	PerGroup     map[string]int `json:"per_group"`     // 按分组分布
	NightFires   int            `json:"night_fires"`   // 深夜（00:00-06:00）触发次数
	DailyAvg     float64        `json:"daily_avg"`     // 日均触发
	QueryErrors  int            `json:"query_errors"`  // 回放期间查询失败次数
	Note         string         `json:"note"`
}

// RunBacktest 在给定时间范围内按 interval 步进回放。
//
// 每一步都真的去查数据源（历史窗口），所以耗时与范围成正比：
// 30 天 × 1 分钟周期 = 43200 次查询，显然不能这么跑。
// 因此步长按「不少于 5 分钟」收敛，并在结论里说明——
// 悄悄降采样而不告诉人，会让回放数字比真实少一个数量级。
func (e *Engine) RunBacktest(ctx context.Context, sc *store.Scoped, d BacktestDraft,
	from, to time.Time,
) (*BacktestResult, error) {
	ad, _, err := e.openDatasource(sc, d.DatasourceID)
	if err != nil {
		return nil, err
	}
	step := time.Duration(d.IntervalSec) * time.Second
	const minStep = 5 * time.Minute
	note := ""
	if step < minStep {
		note = fmt.Sprintf("回放步长按 %s 收敛（规则实际周期 %ds）：结论反映趋势与量级，不是逐周期精确复现",
			minStep, d.IntervalSec)
		step = minStep
	}
	lookback := time.Duration(d.LookbackSec) * time.Second

	res := &BacktestResult{
		PerDay: map[string]int{}, PerGroup: map[string]int{}, Note: note,
	}
	streak := map[string]int{}

	for cursor := from; cursor.Before(to); cursor = cursor.Add(step) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		res.Windows++
		q := datasource.Query{
			Expr: d.Query, From: cursor.Add(-lookback), To: cursor,
			Limit: 1000, GroupBy: d.GroupBy,
		}
		out, err := ad.Query(ctx, q)
		if err != nil {
			// 单个窗口失败不中断整体：历史数据可能有保留期缺口。
			// 但要计数并在结论里体现——基于半截数据的结论不能当成完整结论。
			res.QueryErrors++
			continue
		}
		groups := out.Groups
		if len(d.GroupBy) == 0 {
			groups = map[string]int{"": int(out.Total)}
		}
		for g, cnt := range groups {
			hit := cnt >= d.Threshold
			if d.Absent {
				hit = cnt < d.Threshold
			}
			if !hit {
				streak[g] = 0
				continue
			}
			streak[g]++
			// 只在「刚好达到 for」的那一步计一次触发，
			// 持续满足的后续窗口不重复计——那是同一条事件。
			if streak[g] == maxInt(d.ForPeriods, 1) {
				res.Fires++
				res.PerDay[cursor.Format("2006-01-02")]++
				res.PerGroup[g]++
				if h := cursor.Hour(); h < 6 {
					res.NightFires++
				}
			}
		}
	}
	days := to.Sub(from).Hours() / 24
	if days > 0 {
		res.DailyAvg = float64(res.Fires) / days
	}
	return res, nil
}

// SaveBacktest 落库一次回放结果。
func (e *Engine) SaveBacktest(sc *store.Scoped, id int64, res *BacktestResult, runErr error) {
	if runErr != nil {
		if _, err := sc.Exec(`UPDATE backtests SET status='failed', error=?, finished_at=NOW(3)
			WHERE tenant_id = ? AND id = ?`, truncate(runErr.Error(), 500), id); err != nil {
			logx.J("engine", "backtest_save_error", map[string]any{"error": err.Error()})
		}
		return
	}
	blob, _ := json.Marshal(res)
	if _, err := sc.Exec(`UPDATE backtests SET status='done', result=?, finished_at=NOW(3)
		WHERE tenant_id = ? AND id = ?`, blob, id); err != nil {
		logx.J("engine", "backtest_save_error", map[string]any{"error": err.Error()})
	}
}

// CompareBacktests 把两次回放结果并排，给出降噪比例与覆盖差异。
//
// 「少吵了」必须和「漏报了」一起看：只报噪音下降百分比的对比是危险的，
// 阈值调到无穷大也能让噪音降 100%。
func CompareBacktests(base, draft *BacktestResult) map[string]any {
	reduction := 0.0
	if base.Fires > 0 {
		reduction = float64(base.Fires-draft.Fires) / float64(base.Fires) * 100
	}
	// 基线触发过、草稿没触发的分组 = 潜在漏报，必须逐个列出来让人确认。
	missed := []string{}
	for g := range base.PerGroup {
		if draft.PerGroup[g] == 0 {
			missed = append(missed, g)
		}
	}
	sort.Strings(missed)
	return map[string]any{
		"base_fires":     base.Fires,
		"draft_fires":    draft.Fires,
		"reduction_pct":  reduction,
		"night_before":   base.NightFires,
		"night_after":    draft.NightFires,
		"missed_groups":  missed,
		"missed_warning": len(missed) > 0,
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }
