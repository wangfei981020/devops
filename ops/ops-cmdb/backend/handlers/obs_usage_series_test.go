package handlers

import (
	"encoding/json"
	"testing"
)

// usageSeriesFrom 守的是 OPSCMDB-031 P0-14。
//
// 那一条的表现是：后端返回 61 个数据点、status=success、seriesFetched=225，
// 前端却显示「这个时间范围内没有数据点。**可能是对象名写错了**」。
// 真因是后端只把 Prometheus 原始信封塞进 `data`，真实路径成了 `data.data.result`
// （两层信封），而前端读一层的 `series`。
//
// 报文用**验证报告里记下来的真实形状**，不是我臆造的：
// 这类"差一层"的 bug 只有拿真实报文才测得出来，按文档臆造的形状往往恰好是对的那一层。
const realQueryRangeBody = `{
  "status": "success",
  "isPartial": false,
  "data": {
    "resultType": "matrix",
    "result": [
      {
        "metric": {},
        "values": [[1786940040,"3.8046"],[1786940100,"3.7723"],[1786940160,"3.9011"]]
      }
    ]
  },
  "stats": { "seriesFetched": "225", "executionTimeMsec": 9 }
}`

func TestUsageSeriesFromRealMatrix(t *testing.T) {
	series, pts := usageSeriesFrom(realQueryRangeBody)
	if pts != 3 {
		t.Fatalf("点数 %d，期望 3 —— 这正是 P0-14 的症状：数据在报文里，解析取到 0", pts)
	}
	if len(series) != 1 {
		t.Fatalf("序列数 %d，期望 1", len(series))
	}
	// 聚合查询（sum(...)）的 metric 是空的 {}，序列没有名字。
	// 这时**必须返回空串**，由前端拿用户填的对象名兜底 ——
	// 在后端编一个 "unknown" 会让界面显示一个不存在的对象名
	if name := series[0]["name"]; name != "" {
		t.Errorf("聚合后 metric 为空，name 应该是空串，实际 %q", name)
	}
	points, ok := series[0]["points"].([]gin_H)
	_ = points
	_ = ok
	// 值必须是**数字**，不是 Prometheus 原始的字符串 —— 图表直接吃它
	b, _ := json.Marshal(series[0]["points"])
	var got []struct {
		T int64   `json:"t"`
		V float64 `json:"v"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("points 形状不对：%v（原文 %s）", err, b)
	}
	if got[0].T != 1786940040 || got[0].V < 3.80 || got[0].V > 3.81 {
		t.Errorf("第一个点 t=%d v=%v，期望 t=1786940040 v≈3.8046", got[0].T, got[0].V)
	}
}

// 空结果要能和"解析失败"分开：两者都返回 0 点，但含义完全不同。
// 报告里用 10.146.40.251:9100 试过一次，后端返回 result:[] / seriesFetched:"0" ——
// **空就是真的空，报文形状一眼可辨**。
func TestUsageSeriesFromTrulyEmpty(t *testing.T) {
	body := `{"status":"success","data":{"resultType":"matrix","result":[]},"stats":{"seriesFetched":"0"}}`
	series, pts := usageSeriesFrom(body)
	if pts != 0 {
		t.Fatalf("点数 %d，期望 0", pts)
	}
	if series == nil {
		t.Error("空结果也要返回空切片而不是 nil：nil 序列化成 JSON 是 null，前端 ?? [] 挡不住 null 之外的形状变化")
	}
}

// 单层信封（有人把 Prometheus 的 data 直接当顶层传进来）不能崩，也不能瞎解析出东西。
func TestUsageSeriesFromGarbage(t *testing.T) {
	for _, body := range []string{"", "not json", "{}", `{"data":null}`, `{"data":{"result":null}}`} {
		series, pts := usageSeriesFrom(body)
		if pts != 0 {
			t.Errorf("body=%q 解析出 %d 个点，应该是 0", body, pts)
		}
		if series == nil {
			t.Errorf("body=%q 返回了 nil 切片", body)
		}
	}
}

// resultType=vector（瞬时查询）只有单个 value，也要能出一个点。
func TestUsageSeriesFromVector(t *testing.T) {
	body := `{"status":"success","data":{"resultType":"vector","result":[
		{"metric":{"node":"gke-x-1"},"value":[1786940040,"1.25"]}]}}`
	series, pts := usageSeriesFrom(body)
	if pts != 1 {
		t.Fatalf("点数 %d，期望 1", pts)
	}
	if series[0]["name"] != "gke-x-1" {
		t.Errorf("name=%v，期望 gke-x-1（metric 里有 node 就该用它）", series[0]["name"])
	}
}

// 解析不出来的值必须**丢掉**，不能当 0。
// 一个解析失败的值被画成 0，曲线上就多一个"这一刻负载归零"的假谷底，
// 而那个谷底会被当成真事去排查。
func TestUsageSeriesDropsUnparsableInsteadOfZero(t *testing.T) {
	body := `{"status":"success","data":{"resultType":"matrix","result":[
		{"metric":{},"values":[[1,"1.5"],[2,"NaN-oops"],[3,"2.5"]]}]}}`
	_, pts := usageSeriesFrom(body)
	if pts != 2 {
		t.Fatalf("点数 %d，期望 2（坏点丢掉，不是补 0）", pts)
	}
}

// 内存指标的两种写法必须等价。
// 原来只认 `mem`，前端下拉传 `memory` → **选内存查出来的是 CPU 曲线**。
// 图正常画出来、轴上有数，只是画的不是你选的指标，没有任何迹象提示这一点。
func TestBuildPromQLMemAliasesAgree(t *testing.T) {
	for _, target := range []string{"pod", "workload", "node", "host"} {
		a := buildPromQL(target, "ns", "obj", "mem", `cluster="c1"`, "")
		b := buildPromQL(target, "ns", "obj", "memory", `cluster="c1"`, "")
		if a != b {
			t.Errorf("target=%s：mem 与 memory 生成的 PromQL 不同\nmem   =%s\nmemory=%s", target, a, b)
		}
		// 而且必须真的是内存指标，不能两个都退化成 CPU
		if !hasSub(a, "memory") {
			t.Errorf("target=%s 的内存查询里没有内存指标：%s", target, a)
		}
	}
}

// 单位要跟着指标走：cpu=核、mem=字节。图上不写单位等于没法读数。
func TestPromUnit(t *testing.T) {
	for metric, want := range map[string]string{
		"cpu": "cores", "mem": "bytes", "memory": "bytes", "MEM": "bytes", "": "",
	} {
		if got := promUnit(metric); got != want {
			t.Errorf("promUnit(%q)=%q，期望 %q", metric, got, want)
		}
	}
}

// gin_H 只为下面那句类型断言占位，不参与逻辑
type gin_H = map[string]any
