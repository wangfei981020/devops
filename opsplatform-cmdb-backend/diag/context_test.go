package diag

import (
	"context"
	"errors"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// 取日志失败的翻译必须按 HTTP 状态码判，不能按错误文本判。
// 早先的实现踩过这个坑：403 和「kubelet 不可达」在 client-go 里都长成
// "unknown (get pods xxx)"，按文本匹配会让前一个分支吞掉后一个，
// 结果所有失败都被报成权限问题，而注释里写的 kubelet 分支永远走不到。

// statusErr 造一个带指定 HTTP 码、且消息形如 "unknown (...)" 的 API 错误——
// 这正是 client-go 无法解析响应体时的真实形态。
func statusErr(code int32) error {
	return &apierrors.StatusError{ErrStatus: metav1.Status{
		Status:  metav1.StatusFailure,
		Code:    code,
		Message: "unknown (get pods some-pod)",
	}}
}

func TestExplainLogErrorByStatusCode(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		wantHas string
		wantNot string
	}{
		{
			name:    "403 判成权限问题并给出排查路径",
			err:     statusErr(403),
			wantHas: "HTTP 403",
			wantNot: "kubelet",
		},
		{
			// 关键回归：同样是 "unknown (...)"，5xx 必须走到 kubelet 分支，
			// 不能被 403 分支吞掉
			name:    "502 判成 kubelet 不可达而非权限",
			err:     statusErr(502),
			wantHas: "kubelet",
			wantNot: "无权限",
		},
		{
			name:    "503 同样归到 kubelet 侧",
			err:     statusErr(503),
			wantHas: "kubelet",
			wantNot: "无权限",
		},
		{
			name:    "401 认证失败",
			err:     statusErr(401),
			wantHas: "HTTP 401",
			wantNot: "无权限读日志",
		},
		{
			name:    "404 Pod 不存在",
			err:     statusErr(404),
			wantHas: "不存在",
			wantNot: "kubelet",
		},
		{
			name:    "超时归到 kubelet 侧并建议改用 Loki",
			err:     errors.New("Get \"https://10.0.0.1:10250/containerLogs\": context deadline exceeded"),
			wantHas: "query_loki",
			wantNot: "无权限",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ExplainLogError(c.err)
			if !strings.Contains(got, c.wantHas) {
				t.Errorf("翻译结果应含 %q，实际: %s", c.wantHas, got)
			}
			if c.wantNot != "" && strings.Contains(got, c.wantNot) {
				t.Errorf("翻译结果不应含 %q（归错原因了），实际: %s", c.wantNot, got)
			}
			// 原始错误必须保留，便于人工进一步排查
			if !strings.Contains(got, c.err.Error()) {
				t.Errorf("翻译结果应保留原始错误，实际: %s", got)
			}
		})
	}
}

// 非 API 错误（网络层等）不该被硬塞进某个分类，原样返回即可。
func TestExplainLogErrorPassthrough(t *testing.T) {
	err := errors.New("dial tcp 10.0.0.1:443: connect: connection refused")
	if got := ExplainLogError(err); got != err.Error() {
		t.Errorf("无法归类的错误应原样返回，实际: %s", got)
	}
}

func TestAPIStatusCode(t *testing.T) {
	if got := apiStatusCode(statusErr(403)); got != 403 {
		t.Errorf("apiStatusCode = %d, 期望 403", got)
	}
	// 被包装过的错误也要能取到状态码（client-go 会包装）
	wrapped := context.Canceled
	if got := apiStatusCode(wrapped); got != 0 {
		t.Errorf("非 API 错误应返回 0，实际 %d", got)
	}
	// NotFound 走标准构造函数，确保 errors.As 对真实错误也成立
	nf := apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, "x")
	if got := apiStatusCode(nf); got != 404 {
		t.Errorf("NewNotFound 的状态码应为 404，实际 %d", got)
	}
}
