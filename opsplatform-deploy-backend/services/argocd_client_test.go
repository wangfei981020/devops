package services

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestSync_BodyDoesNotIncludeRevision 防回归：sync API body 不能写死 revision: HEAD
//
//	历史 bug：写死 "revision": "HEAD" 时，跟 Application.spec.source.targetRevision="main"
//	不一致 + auto-sync 开 → ArgoCD 拒绝 "Cannot sync to HEAD: auto-sync currently set to main"
//	修法：省略 revision 字段，让 ArgoCD 用 Application 自己的 targetRevision
func TestSync_BodyDoesNotIncludeRevision(t *testing.T) {
	var capturedBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &capturedBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := NewArgocdClient(srv.URL, "fake-token")
	if err := c.Sync(context.Background(), "my-app"); err != nil {
		t.Fatalf("Sync returned error: %v", err)
	}

	if _, ok := capturedBody["revision"]; ok {
		t.Errorf("sync body 不应包含 revision 字段（写死 HEAD 会跟 targetRevision=main 的 app 冲突，触发 'Cannot sync to HEAD' 错误）。实际 body: %+v", capturedBody)
	}

	// 同时验证其他必备字段还在
	for _, k := range []string{"prune", "dryRun", "strategy"} {
		if _, ok := capturedBody[k]; !ok {
			t.Errorf("sync body 缺少字段 %q，期望保留", k)
		}
	}
}
