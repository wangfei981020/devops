package handlers

import (
	"testing"

	"opsplatform-deploy-backend/services"
)

// TestRefreshResultsHealthy 验证：对账判成功时把旧的 argocd_results 快照
// 覆写成 Synced+Healthy + note，且保留原有 DurationSec；旧快照没有的 app 补一条。
func TestRefreshResultsHealthy(t *testing.T) {
	// 旧快照：一个 app 卡在 Progressing / 等待 Pod 就绪 / 30s（模拟 #1536）
	argoRaw := `[{"app":"merchant-client-backend-uat","sync_status":"Synced","health":"Progressing","duration_sec":30,"msg":"等待 Pod 就绪"}]`
	apps := []string{"merchant-client-backend-uat", "extra-app"}
	statuses := map[string]*services.AppStatus{
		"merchant-client-backend-uat": {SyncStatus: "Synced", Health: "Healthy"},
		// extra-app 没给 status，应回落到 Synced/Healthy 默认
	}
	out := refreshResultsHealthy(argoRaw, apps, statuses, "后端重启后对账确认已就绪")

	if len(out) != 2 {
		t.Fatalf("want 2 results, got %d", len(out))
	}
	m := out[0]
	if m.App != "merchant-client-backend-uat" {
		t.Fatalf("app mismatch: %s", m.App)
	}
	if m.Health != "Healthy" {
		t.Errorf("health should be refreshed to Healthy, got %q", m.Health)
	}
	if m.SyncStatus != "Synced" {
		t.Errorf("sync should be Synced, got %q", m.SyncStatus)
	}
	if m.Msg != "后端重启后对账确认已就绪" {
		t.Errorf("msg not refreshed: %q", m.Msg)
	}
	if m.DurationSec != 30 {
		t.Errorf("duration_sec should be preserved (30), got %d", m.DurationSec)
	}
	if m.LastPolledAt.IsZero() {
		t.Errorf("last_polled_at should be set")
	}
	// 旧快照没有的 app 也要补出来，且是 Healthy
	if out[1].App != "extra-app" || out[1].Health != "Healthy" {
		t.Errorf("missing app not backfilled healthy: %+v", out[1])
	}
}
