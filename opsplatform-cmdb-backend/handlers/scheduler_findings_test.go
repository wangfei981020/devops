package handlers

import (
	"context"
	"strings"
	"testing"
)

// 摘要必须带上具体对象——「危险 1 项」看不出要去处置什么，是本次改动的起因。
func TestSummarizeFindings(t *testing.T) {
	items := []TaskFinding{
		{Level: "critical", Target: "dev-k8s · 节点 node17", Value: "93.6%"},
		{Level: "warning", Target: "dev-k8s · 节点 node2", Value: "88.8%"},
		{Level: "warning", Target: "infra-01 · PVC logging/loki-chunks-cache-0", Value: "87.0%"},
		{Level: "warning", Target: "infra-01 · PVC logging/es-warm-v2-0", Value: "85.2%"},
	}

	got := SummarizeFindings(items, 2)
	if !strings.Contains(got, "node17 93.6%") {
		t.Errorf("摘要必须含最严重对象的名字和数值，实际: %q", got)
	}
	if !strings.Contains(got, "等 4 项") {
		t.Errorf("超出展示上限时要说明总数，实际: %q", got)
	}
	if strings.Contains(got, "es-warm-v2-0") {
		t.Errorf("超过 maxShow 的项不该出现在摘要里，实际: %q", got)
	}

	// 没有 value 的项（如「未检查」）不该拼出多余空格
	one := SummarizeFindings([]TaskFinding{{Target: "infra-02"}}, 3)
	if one != "infra-02" {
		t.Errorf("无数值时应只有对象名，实际: %q", one)
	}

	if SummarizeFindings(nil, 3) != "" {
		t.Error("空列表应返回空串，不能拼出「等 0 项」这种噪声")
	}
}

// AddFinding 在没有收集器的 ctx 上必须静默忽略：核心函数可能被 MCP、
// 单元测试或其它入口直接调用，不能因为拿不到 sink 就 panic 掉整个任务。
func TestAddFindingWithoutSink(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("无收集器时不应 panic，实际: %v", r)
		}
	}()
	AddFinding(context.Background(), TaskFinding{Level: "info", Target: "x"})
}

// 收集器要按上报顺序完整保留，且 list() 返回副本——
// finishRunLog 拿到后会序列化写库，不能受后续写入影响。
func TestFindingSinkCollect(t *testing.T) {
	ctx, sink := withFindingSink(context.Background())
	AddFinding(ctx, TaskFinding{Level: "critical", Target: "a"})
	AddFinding(ctx, TaskFinding{Level: "warning", Target: "b"})

	got := sink.list()
	if len(got) != 2 || got[0].Target != "a" || got[1].Target != "b" {
		t.Fatalf("收集结果不对: %+v", got)
	}

	AddFinding(ctx, TaskFinding{Level: "info", Target: "c"})
	if len(got) != 2 {
		t.Error("list() 必须返回副本，后续上报不应改动已取走的切片")
	}
	if len(sink.list()) != 3 {
		t.Error("新上报的项应该能被下一次 list() 取到")
	}
}
