package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// KubeSphere 的真实形态：根路径和 /kapis 都是 404，/kapis/version 才是 200。
// 这正是这次报"连通失败 HTTP 404"的场景——探根路径必然误判。
func TestProbeEndpoint_KubeSphereRootIs404ButVersionIs200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/kapis/version" {
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"gitVersion":"v3.4.1"}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	got := probeEndpoint(srv.URL, "", probePaths("kubesphere"), "kubesphere")
	if got["ok"] != true {
		t.Fatalf("应判为连通，got %+v", got)
	}
	if got["path"] != "/kapis/version" {
		t.Errorf("应报告命中的路径，got %v", got["path"])
	}
	if got["status"] != 200 {
		t.Errorf("status 应为 200，got %v", got["status"])
	}
}

// 服务活着但全部路径都不是 2xx —— 通常是地址填错，要说清楚而不是只丢一个 404。
func TestProbeEndpoint_AllNon2xxGivesActionableError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	got := probeEndpoint(srv.URL, "", probePaths("kubesphere"), "kubesphere")
	if got["ok"] != false {
		t.Fatalf("应判为失败，got %+v", got)
	}
	msg, _ := got["error"].(string)
	if msg == "" || !contains(msg, "地址") {
		t.Errorf("失败原因要能指导排查，got %q", msg)
	}
	tried, ok := got["tried"].([]gin.H)
	if !ok {
		t.Fatalf("tried 应为明细列表，got %T", got["tried"])
	}
	if len(tried) != len(probePaths("kubesphere")) {
		t.Errorf("应记录每条探测路径，got %d 条", len(tried))
	}
}

// 401/403 要和"连不上"区分开：地址是对的，缺的是 token。
func TestProbeEndpoint_UnauthorizedIsReportedAsPermission(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()

	got := probeEndpoint(srv.URL, "", probePaths("prometheus"), "prometheus")
	msg, _ := got["error"].(string)
	if got["ok"] != false || !contains(msg, "权限") {
		t.Errorf("401 应报成权限问题，got %+v", got)
	}
}

// 地址完全连不上（服务没起/端口错）要报连接错误，而不是"路径不对"。
func TestProbeEndpoint_ConnectionRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // 立刻关掉，制造连不上

	got := probeEndpoint(url, "", []string{"/healthz"}, "")
	msg, _ := got["error"].(string)
	if got["ok"] != false || !contains(msg, "连不上") {
		t.Errorf("应报连接失败，got %+v", got)
	}
}

// 第一条就通时不该继续探后面的（省一次多余请求）。
func TestProbeEndpoint_StopsAtFirstSuccess(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(200)
	}))
	defer srv.Close()

	got := probeEndpoint(srv.URL, "", probePaths("loki"), "loki")
	if got["ok"] != true || hits != 1 {
		t.Errorf("应在第一条成功后停止，hits=%d got=%+v", hits, got)
	}
}

func TestProbePaths_CoverKnownTypes(t *testing.T) {
	for _, typ := range []string{"loki", "prometheus", "kubesphere", "vm", "victoriametrics"} {
		if len(probePaths(typ)) == 0 {
			t.Errorf("%s 没有探测路径", typ)
		}
	}
	// KubeSphere 不能只探根路径——那正是这次误判的原因。
	ks := probePaths("kubesphere")
	if ks[0] == "" {
		t.Error("kubesphere 首选路径不能是根路径")
	}
}

// 夜莺只认 X-User-Token。发成 Authorization: Bearer 的话，token 明明是好的，
// 「测试连通」却报"401 token 没配或已失效"——功能正常但测试说坏了，
// 会让人去改一个根本没问题的配置。
func TestProbeNightingaleUsesUserTokenHeader(t *testing.T) {
	var gotUserToken, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserToken = r.Header.Get("X-User-Token")
		gotAuth = r.Header.Get("Authorization")
		if gotUserToken != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"dat":{"list":[],"total":0}}`))
	}))
	defer srv.Close()

	got := probeEndpoint(srv.URL, "secret", probePaths("n9e"), "n9e")
	if got["ok"] != true {
		t.Fatalf("夜莺探活应成功，实际: %v", got)
	}
	if gotUserToken != "secret" {
		t.Errorf("X-User-Token = %q，期望 secret", gotUserToken)
	}
	if gotAuth != "" {
		t.Errorf("不该再发 Authorization 头，实际 %q", gotAuth)
	}
}

// 其余类型仍走 Bearer，别在修夜莺的时候把 Prometheus 带坏
func TestProbeOthersStillUseBearer(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	probeEndpoint(srv.URL, "tok", probePaths("prometheus"), "prometheus")
	if gotAuth != "Bearer tok" {
		t.Errorf("Authorization = %q，期望 Bearer tok", gotAuth)
	}
}
