package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"ops-alert-backend/datasource"
	"ops-alert-backend/internal/store"
	"ops-alert-backend/notify"
)

// 本文件是给 API 层用的导出封装。
//
// 为什么不直接把内部方法改成导出：内部方法的签名会随引擎重构而变，
// 而 API 层依赖的是「能力」而不是「实现」。这一层薄封装让引擎内部
// 可以自由重构，只要这几个语义不变。

// MatchRoutePublic 供路由试算使用。
func (e *Engine) MatchRoutePublic(sc *store.Scoped, labels map[string]string) RouteMatch {
	return e.matchRoute(sc, labels)
}

// OpenDatasource 供连通性测试与试运行使用。
func (e *Engine) OpenDatasource(sc *store.Scoped, id int64) (datasource.Adapter, string, error) {
	return e.openDatasource(sc, id)
}

// DryRunResult 是试运行结果：拿真实数据跑一遍判定，但不产生事件、不发通知。
type DryRunResult struct {
	Hits      int64          `json:"hits"`
	Groups    map[string]int `json:"groups"`
	WillFire  []string       `json:"will_fire"`
	TookMS    int            `json:"took_ms"`
	Truncated bool           `json:"truncated"`
	Samples   []string       `json:"samples"`
}

// DryRun 试运行一条（还没保存的）规则草稿。
//
// 必须先于保存：上一代要等下一个调度周期才知道查询写错了，
// 而那时错误只体现在一行 last_error 里，没人盯着。
func (e *Engine) DryRun(ctx context.Context, sc *store.Scoped, dsID int64,
	expr string, lookbackSec, threshold, limit int, groupBy []string,
) (*DryRunResult, error) {
	ad, _, err := e.openDatasource(sc, dsID)
	if err != nil {
		return nil, err
	}
	to := time.Now()
	res, err := ad.Query(ctx, datasource.Query{
		Expr:    expr,
		From:    to.Add(-time.Duration(lookbackSec) * time.Second),
		To:      to,
		Limit:   limit,
		GroupBy: groupBy,
	})
	if err != nil {
		return nil, err
	}
	groups := res.Groups
	if len(groupBy) == 0 {
		groups = map[string]int{"": int(res.Total)}
	}
	out := &DryRunResult{
		Hits: res.Total, Groups: groups, TookMS: res.TookMs, Truncated: res.Truncated,
	}
	for _, g := range datasource.SortedGroups(groups) {
		if groups[g] >= threshold {
			out.WillFire = append(out.WillFire, g)
		}
	}
	for i, h := range res.Hits {
		if i >= 5 {
			break
		}
		line := h.Line
		if line == "" {
			line = sampleFields(h)
		}
		out.Samples = append(out.Samples, truncate(line, 400))
	}
	return out, nil
}

func sampleFields(h datasource.Hit) string {
	b, _ := jsonMarshal(h.Fields)
	return string(b)
}

// BuildReportPreview 生成一份日报内容但不发送。给接口层做预览用。
//
// 复用 buildReport 而不是另写一份：两份实现必然分叉，
// 而分叉的表现是"预览看着好好的，实际发出来是另一个样子"。
func (e *Engine) BuildReportPreview(sc *store.Scoped, now time.Time) ([]notify.Field, string, error) {
	return e.buildReport(sc, now)
}

// SendReportNow 立即发一份日报，返回成功数与总数。
//
// ⚠️ 故意**不更新 last_sent_on**：试发不该顶掉当天的正式发送。
// 顶掉的表现是"我上午试发了一下，结果第二天早上的日报没来"，
// 而这个因果关系没人猜得到。
func (e *Engine) SendReportNow(ctx context.Context, sc *store.Scoped, now time.Time) (sent, total int, err error) {
	var rawNotifiers []byte
	if err := sc.QueryRow(`SELECT notifier_ids FROM report_configs WHERE tenant_id = ?`).
		Scan(&rawNotifiers); err != nil {
		if err == sql.ErrNoRows {
			return 0, 0, fmt.Errorf("还没有配置日报")
		}
		return 0, 0, err
	}
	var ids []int64
	_ = json.Unmarshal(rawNotifiers, &ids)
	if len(ids) == 0 {
		return 0, 0, fmt.Errorf("没有配置投递渠道")
	}
	fields, detail, err := e.buildReport(sc, now)
	if err != nil {
		return 0, len(ids), err
	}
	msg := notify.Message{
		Title:  fmt.Sprintf("OpsAlert 日报（试发）· %s", now.AddDate(0, 0, -1).Format("2006-01-02")),
		Fields: fields,
		Sample: detail,
	}
	for _, nid := range ids {
		if e := e.sendVia(ctx, sc, nid, msg); e != nil {
			err = e
			continue
		}
		sent++
	}
	return sent, len(ids), err
}

// TemplateVarNames 可用变量清单。界面上的"可用变量"提示用它，
// 与渲染实现出自同一处 —— 各写一份必然出现"照着提示写了，发出来是 (无此变量)"。
func (e *Engine) TemplateVarNames() [][2]string { return BuiltinVarNames() }

// PreviewTemplate 拿一条真实事件渲染模板。
//
// incidentID 为 0 时取最近一条事件。一条都没有时返回错误而不是拿假数据凑 ——
// 假数据里每个变量都有值，于是"这个变量在我的规则里其实取不到"这类问题
// 在预览里永远看不出来，而它恰恰是最常见的模板错误。
func (e *Engine) PreviewTemplate(sc *store.Scoped, titleTmpl, bodyTmpl string, incidentID int64) (
	title, body, warn, sampleFrom string, err error,
) {
	var id int64
	var ruleName, severity, sample string
	var count int
	var firstAt time.Time
	var rawLabels []byte

	q := `SELECT i.id, COALESCE(r.name, i.title), i.severity, i.count, i.first_at, i.labels, i.sample
		FROM incidents i LEFT JOIN rules r ON r.id = i.rule_id AND r.tenant_id = i.tenant_id
		WHERE i.tenant_id = ?`
	args := []any{}
	if incidentID > 0 {
		q += ` AND i.id = ?`
		args = append(args, incidentID)
	} else {
		q += ` ORDER BY i.first_at DESC LIMIT 1`
	}
	var sampleRaw []byte
	if err = sc.QueryRow(q, args...).Scan(&id, &ruleName, &severity, &count, &firstAt, &rawLabels, &sampleRaw); err != nil {
		if err == sql.ErrNoRows {
			return "", "", "", "", fmt.Errorf(
				"还没有任何事件，无法预览。预览必须拿真实事件渲染 —— " +
					"假数据里每个变量都有值，会把「这个变量其实取不到」这类问题藏起来")
		}
		return "", "", "", "", err
	}
	sample = string(sampleRaw)

	var labels map[string]string
	_ = json.Unmarshal(rawLabels, &labels)
	vars := templateVars(ruleName, severity, groupKeyOf(labels), "", count,
		firstAt, e.cfg.Location, sample, nil, labels)

	title, body, warn = renderMessage(
		msgTemplate{Name: "预览", Title: titleTmpl, Body: bodyTmpl}, vars)
	sampleFrom = fmt.Sprintf("事件 #%d · %s · %s", id, ruleName, firstAt.In(e.cfg.Location).Format("2006-01-02 15:04:05"))
	return title, body, warn, sampleFrom, nil
}
