package handlers

import (
	"strings"
	"testing"
)

// 最关键的一条：多集群共享数据源 + 查询没做隔离时，必须明说结果可能掺别的集群。
// 静默返回混合数据，比返回空更危险——数字看着正常，其实是几个集群加在一起的。
func TestApplyClusterWarnsWhenNotIsolated(t *testing.T) {
	q, isolated, note := applyCluster(`up`, `cluster="dev"`)
	if isolated {
		t.Error("查询未含占位符且数据源多集群共享时，不能声称已隔离")
	}
	if q != "up" {
		t.Errorf("未隔离时不该改写查询，实际 %q", q)
	}
	if !strings.Contains(note, "混入") || !strings.Contains(note, `cluster="dev"`) {
		t.Errorf("提示必须说明会混入其它集群、并给出该用的选择器，实际: %s", note)
	}
}

func TestApplyClusterSubstitutesPlaceholder(t *testing.T) {
	q, isolated, _ := applyCluster(`kafka_consumergroup_lag{$CLUSTER,topic="orders"}`, `cluster="dev"`)
	if !isolated {
		t.Error("写了占位符就应判为已隔离")
	}
	want := `kafka_consumergroup_lag{cluster="dev",topic="orders"}`
	if q != want {
		t.Errorf("占位符替换错误\n期望: %s\n实际: %s", want, q)
	}
}

// 单集群数据源：占位符要清理干净，不能把 {$CLUSTER} 原样发给 Prometheus（会语法错误）。
func TestApplyClusterCleansPlaceholderOnSingleCluster(t *testing.T) {
	for _, in := range []string{`up{$CLUSTER}`, `up{$CLUSTER,job="x"}`, `sum(rate(x{$CLUSTER}[5m]))`} {
		q, isolated, _ := applyCluster(in, "")
		if !isolated {
			t.Errorf("单集群数据源应视为已隔离: %s", in)
		}
		if strings.Contains(q, "$CLUSTER") {
			t.Errorf("占位符未清理干净，会导致 PromQL 语法错误: %q", q)
		}
	}
	// 独占一对花括号时整个 {} 都要去掉，留下空 {} 同样是语法错误
	if q, _, _ := applyCluster(`up{$CLUSTER}`, ""); q != "up" {
		t.Errorf("空标签选择器应整体去掉，实际 %q", q)
	}
}

// 步长要随窗口放大，否则查一天的数据会返回几千个点。
func TestAutoStepScalesWithWindow(t *testing.T) {
	prev := 0.0
	for _, m := range []int{30, 180, 720, 1440, 4320} {
		s := autoStep(m)
		cur := stepSeconds(s)
		if cur <= prev {
			t.Errorf("窗口 %d 分钟的步长 %s 未随窗口增大（上一档 %.0fs）", m, s, prev)
		}
		// 点数控制在 ~200 以内
		if pts := float64(m*60) / cur; pts > 220 {
			t.Errorf("窗口 %d 分钟按步长 %s 会产生 %.0f 个点，过密", m, s, pts)
		}
		prev = cur
	}
}

func stepSeconds(s string) float64 {
	switch s {
	case "15s":
		return 15
	case "1m":
		return 60
	case "5m":
		return 300
	case "10m":
		return 600
	case "1h":
		return 3600
	}
	return 0
}
