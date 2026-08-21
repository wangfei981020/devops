// Package export 把一次对账结果导出成 Excel。
//
// 为什么导出这件事值得单独一个包：这张表**会被转发**。
// 它一旦离开界面就没有上下文了 —— 收到附件的人不知道数据是什么时候采的、
// 基准是哪一列、哪些列其实没采到。所以第一个 sheet 必须是「数据说明」，
// 而不是直接甩一张对比表过去。
package export

import (
	"fmt"
	"time"

	"github.com/xuri/excelize/v2"

	"ops-version-backend/internal/compare"
	"ops-version-backend/providers"
)

// Input 导出所需的全部数据。
type Input struct {
	Result   compare.Result
	Plan     compare.Plan
	PlanName string
	// Pods 各列的 Pod 明细，key 是 Column.Key()
	Pods map[string][]providers.PodInfo
	// Operator 谁导的。导出会外发，必须能追溯
	Operator string
	// FilterNote 导出时界面上应用了什么筛选。空 = 没筛，导的是全量
	FilterNote string
	// Now 导出时刻，由调用方传入而不是包内取 —— 好让测试可重复
	Now time.Time
}

// 判定 → 中文标签。与界面用同一套词，避免「界面说落后、表里说 behind」
var verdictLabel = map[compare.Verdict]string{
	compare.VerdictSame:     "一致",
	compare.VerdictBehind:   "落后",
	compare.VerdictAhead:    "超前",
	compare.VerdictMissing:  "该列没有",
	compare.VerdictExtra:    "基准没有",
	compare.VerdictUnknown:  "无法判定",
	compare.VerdictConflict: "同名冲突",
	compare.VerdictNoData:   "数据不可用",
}

// Build 生成 xlsx 字节流。
func Build(in Input) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	st, err := newStyles(f)
	if err != nil {
		return nil, err
	}

	// 🔴 说明放第一个 sheet，不是最后。转发出去的表，人只会看第一屏
	if err := writeReadme(f, in, st); err != nil {
		return nil, err
	}
	if err := writeMatrix(f, in, st); err != nil {
		return nil, err
	}
	if err := writeDiff(f, in, st); err != nil {
		return nil, err
	}
	if err := writeDetails(f, in, st); err != nil {
		return nil, err
	}

	// NewFile 会自带一个空的 Sheet1，留着会让人以为漏了内容
	if idx, _ := f.GetSheetIndex("Sheet1"); idx >= 0 {
		_ = f.DeleteSheet("Sheet1")
	}
	f.SetActiveSheet(0)

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Format("2006-01-02 15:04:05")
}

// age 把启动时刻换算成「跑了多久」。
//
// ⚠️ 基准是**导出时刻**，不是采集时刻 —— 两者可能差几分钟到几小时，
// 而人看到的「Age」是在读这张表的时候理解的。
func age(started, now time.Time) string {
	if started.IsZero() {
		return "—"
	}
	d := now.Sub(started)
	if d < 0 {
		return "—"
	}
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%d 分钟", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%.1f 小时", d.Hours())
	default:
		return fmt.Sprintf("%.1f 天", d.Hours()/24)
	}
}
