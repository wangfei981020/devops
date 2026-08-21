package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 🔴 响应超过体积上限时必须**报错**，不能返回半截数据。
//
// 生产实测（2026-08-18，KubeSphere 刚接入）：
// `pipeline_runs` 请求 limit=500，而每条 PipelineRun 都带着完整的 Jenkinsfile（~5KB），
// 响应约 2.5MB，被 `io.ReadAll(io.LimitReader(body, 1<<20))` **静默截断**成半截 JSON。
//
// 调用方 Unmarshal 失败后报「解析流水线运行记录失败」——
// 把人指向"数据格式不对"这个**完全错误的方向**，而真实原因是响应太大。
//
// ⚠️ 截断和格式错误必须分开报：
//
//	截断   → 调小 limit / 缩短时间窗 / 加过滤条件
//	格式错 → 去查上游到底返回了什么
//
// 指错方向比不给提示更费时间。
func TestObsGetReportsTruncation(t *testing.T) {
	t.Run("没超上限：正常返回", func(t *testing.T) {
		body := strings.Repeat("x", 1024)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		defer srv.Close()
		code, got, err := obsGet(srv.URL, "", 5*time.Second)
		if err != nil {
			t.Fatalf("不该报错：%v", err)
		}
		if code != 200 || len(got) != len(body) {
			t.Errorf("code=%d len=%d，期望 200 / %d", code, len(got), len(body))
		}
	})

	t.Run("正好等于上限：仍算正常，不误报", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("y", obsMaxBody)))
		}))
		defer srv.Close()
		if _, _, err := obsGet(srv.URL, "", 20*time.Second); err != nil {
			t.Errorf("正好到上限没有丢任何东西，不该报错：%v", err)
		}
	})

	t.Run("🔴 超过上限：必须报错，且说清是截断不是格式问题", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("z", obsMaxBody+4096)))
		}))
		defer srv.Close()
		_, got, err := obsGet(srv.URL, "", 20*time.Second)
		if err == nil {
			t.Fatal("超过上限却没报错 —— 调用方会拿到半截数据并报成「解析失败」")
		}
		if got != "" {
			t.Error("既然判定为截断，就不该再把半截数据交出去")
		}
		for _, want := range []string{"截断", "不是数据格式问题", "limit"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("错误信息里缺 %q（要把人指向正确方向）：%v", want, err)
			}
		}
	})
}
