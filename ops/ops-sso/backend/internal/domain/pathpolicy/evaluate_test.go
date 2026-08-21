package pathpolicy

import (
	"math/rand"
	"testing"
	"time"
)

const (
	appHarbor = 1
	gDev      = 201
	uZhang    = 1001
	ciRobot   = 9001
)

func zhang() Request {
	return Request{
		AppID: appHarbor, UserID: uZhang,
		GroupIDs: map[int64]bool{gDev: true}, RoleIDs: map[int64]bool{}, DeptDepth: map[int64]int{},
		DeviceState: "managed", SourceKind: "office",
		At: time.Date(2026, 8, 7, 10, 0, 0, 0, time.Local),
	}
}

// 服务账号：没有"设备"概念。这一点是原型里 report-job 被误杀的根源。
func robot() Request {
	return Request{
		AppID: appHarbor, UserID: ciRobot,
		GroupIDs: map[int64]bool{}, RoleIDs: map[int64]bool{}, DeptDepth: map[int64]int{},
		DeviceState: "", SourceKind: "internal",
		At: time.Date(2026, 8, 7, 2, 0, 0, 0, time.Local),
	}
}

func r(id int64, methods, path string, st SubjectType, sid int64, d Decision) Rule {
	return Rule{ID: id, AppID: appHarbor, Methods: methods, PathPattern: path,
		SubjectType: st, SubjectID: sid, Decision: d, MFATTLSec: 1800}
}

func TestDefaultDenyWhenNoRule(t *testing.T) {
	req := zhang()
	req.Method, req.Path = "GET", "/api/v2/projects"
	got := Evaluate(req, nil)
	if got.Decision != Deny || got.Reason != ReasonDefaultDeny {
		t.Fatalf("无规则必须拒绝，得到 %+v", got)
	}
}

func TestLongerPathWins(t *testing.T) {
	rules := []Rule{
		r(1, "*", "/**", SubjectPublic, 0, Allow),
		r(2, "*", "/api/v2/projects/pay/**", SubjectPublic, 0, Deny),
	}
	req := zhang()
	req.Method, req.Path = "GET", "/api/v2/projects/pay/repos"
	if got := Evaluate(req, rules); got.Decision != Deny {
		t.Fatalf("更长的路径应更具体，得到 %+v", got)
	}
	req.Path = "/api/v2/projects/other"
	if got := Evaluate(req, rules); got.Decision != Allow {
		t.Fatalf("不在子树内应走通配规则，得到 %+v", got)
	}
}

func TestExactBeatsSubtree(t *testing.T) {
	rules := []Rule{
		r(1, "*", "/api/v2/**", SubjectPublic, 0, Deny),
		r(2, "GET", "/api/v2/health", SubjectPublic, 0, Allow),
	}
	req := zhang()
	req.Method, req.Path = "GET", "/api/v2/health"
	if got := Evaluate(req, rules); got.Decision != Allow {
		t.Fatalf("精确路径应比子树具体，得到 %+v", got)
	}
}

func TestMethodSpecificity(t *testing.T) {
	rules := []Rule{
		r(1, "*", "/api/v2/**", SubjectPublic, 0, Allow),
		r(2, "POST,PUT", "/api/v2/**", SubjectPublic, 0, Challenge),
	}
	req := zhang()
	req.Method, req.Path = "POST", "/api/v2/projects"
	if got := Evaluate(req, rules); got.Decision != Challenge {
		t.Fatalf("指定方法应比通配具体，得到 %+v", got)
	}
	req.Method = "GET"
	if got := Evaluate(req, rules); got.Decision != Allow {
		t.Fatalf("GET 不该命中 POST 规则，得到 %+v", got)
	}
}

func TestSubjectSpecificityBeatsPath(t *testing.T) {
	// 点名张三的粗路径规则，应盖过按组的细路径规则 —— 与 access 包同一套语义
	rules := []Rule{
		r(1, "*", "/**", SubjectUser, uZhang, Deny),
		r(2, "*", "/api/v2/projects/pay/**", SubjectGroup, gDev, Allow),
	}
	req := zhang()
	req.Method, req.Path = "GET", "/api/v2/projects/pay/repos"
	if got := Evaluate(req, rules); got.Decision != Deny {
		t.Fatalf("点名的主体应优先，得到 %+v", got)
	}
}

// ★ 这条是 P0-1 的直接回归：顺序不再影响结果
func TestOrderIndependent(t *testing.T) {
	rules := []Rule{
		r(1, "*", "/**", SubjectPublic, 0, Allow),
		r(2, "POST", "/api/v2/**", SubjectGroup, gDev, Challenge),
		r(3, "DELETE", "/api/v2/**", SubjectPublic, 0, Deny),
		r(4, "*", "/api/v2/artifacts/**", SubjectUser, ciRobot, Allow),
	}
	req := zhang()
	req.Method, req.Path = "POST", "/api/v2/projects"
	want := Evaluate(req, rules)

	rnd := rand.New(rand.NewSource(42))
	for i := 0; i < 50; i++ {
		shuffled := append([]Rule(nil), rules...)
		rnd.Shuffle(len(shuffled), func(a, b int) { shuffled[a], shuffled[b] = shuffled[b], shuffled[a] })
		got := Evaluate(req, shuffled)
		if got.Decision != want.Decision || got.Rule.ID != want.Rule.ID {
			t.Fatalf("第 %d 次打乱后结论变了：%v(#%d) vs %v(#%d)",
				i, got.Decision, got.Rule.ID, want.Decision, want.Rule.ID)
		}
	}
}

// ★ 原型预演里抓到的那个坑：服务账号没有"设备"，不能被"设备未纳管"规则误杀
func TestServiceAccountHasNoDevice(t *testing.T) {
	rules := []Rule{
		r(1, "*", "/api/v2/artifacts/**", SubjectUser, ciRobot, Allow),
		{ID: 2, AppID: appHarbor, Methods: "*", PathPattern: "/**",
			SubjectType: SubjectPublic, Decision: Deny, DeviceState: "unmanaged"},
	}
	req := robot()
	req.Method, req.Path = "POST", "/api/v2/artifacts/sha256:abc"
	got := Evaluate(req, rules)
	if got.Decision != Allow {
		t.Fatalf("服务账号不该被设备规则误杀，得到 %+v", got)
	}
}

func TestDeviceCondition(t *testing.T) {
	rules := []Rule{
		r(1, "*", "/**", SubjectPublic, 0, Allow),
		{ID: 2, AppID: appHarbor, Methods: "*", PathPattern: "/**",
			SubjectType: SubjectPublic, Decision: Deny, DeviceState: "unmanaged"},
	}
	req := zhang()
	req.Method, req.Path = "GET", "/x"
	if got := Evaluate(req, rules); got.Decision != Allow {
		t.Fatalf("已纳管设备应放行，得到 %+v", got)
	}
	req.DeviceState = "unmanaged"
	if got := Evaluate(req, rules); got.Decision != Deny {
		t.Fatalf("未纳管设备应拒绝，得到 %+v", got)
	}
}

func TestTimeWindow(t *testing.T) {
	rules := []Rule{
		{ID: 1, AppID: appHarbor, Methods: "DELETE", PathPattern: "/api/v2/**",
			SubjectType: SubjectPublic, Decision: Challenge, TimeWindow: "08:00-22:00"},
	}
	req := zhang()
	req.Method, req.Path = "DELETE", "/api/v2/projects/x"
	if got := Evaluate(req, rules); got.Decision != Challenge {
		t.Fatalf("窗口内应命中，得到 %+v", got)
	}
	req.At = time.Date(2026, 8, 7, 23, 30, 0, 0, time.Local)
	if got := Evaluate(req, rules); got.Decision != Deny || got.Reason != ReasonDefaultDeny {
		t.Fatalf("窗口外规则不命中 → 应落到默认拒绝，得到 %+v", got)
	}
}

func TestTicketRequired(t *testing.T) {
	rules := []Rule{
		{ID: 1, AppID: appHarbor, Methods: "DELETE", PathPattern: "/api/v2/**",
			SubjectType: SubjectPublic, Decision: Allow, RequireTicket: true},
	}
	req := zhang()
	req.Method, req.Path = "DELETE", "/api/v2/projects/x"
	got := Evaluate(req, rules)
	if got.Decision != Challenge || got.Reason != ReasonNoTicket {
		t.Fatalf("缺工单应降为 challenge 并给出可执行原因，得到 %+v", got)
	}
	req.HasTicket = true
	if got := Evaluate(req, rules); got.Decision != Allow {
		t.Fatalf("带工单应放行，得到 %+v", got)
	}
}

func TestSameRankTakesStrictest(t *testing.T) {
	rules := []Rule{
		r(1, "*", "/api/**", SubjectPublic, 0, Allow),
		r(2, "*", "/api/**", SubjectPublic, 0, Deny),
	}
	req := zhang()
	req.Method, req.Path = "GET", "/api/x"
	if got := Evaluate(req, rules); got.Decision != Deny {
		t.Fatalf("同具体度应取最严，得到 %+v", got)
	}
}

func TestCrossAppIsolation(t *testing.T) {
	rules := []Rule{{ID: 1, AppID: 999, Methods: "*", PathPattern: "/**",
		SubjectType: SubjectPublic, Decision: Allow}}
	req := zhang()
	req.Method, req.Path = "GET", "/x"
	if got := Evaluate(req, rules); got.Decision != Deny {
		t.Fatalf("别的应用的规则不该生效，得到 %+v", got)
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name string
		r    Rule
		ok   bool
	}{
		{"合法", r(1, "*", "/**", SubjectPublic, 0, Allow), true},
		{"无应用", Rule{ID: 1, PathPattern: "/**", SubjectType: SubjectPublic, Decision: Allow}, false},
		{"路径不以/开头", r(1, "*", "api/**", SubjectPublic, 0, Allow), false},
		{"未知判定", r(1, "*", "/**", SubjectPublic, 0, "maybe"), false},
		{"public带id", r(1, "*", "/**", SubjectPublic, 5, Allow), false},
		{"user缺id", r(1, "*", "/**", SubjectUser, 0, Allow), false},
		{"坏时间窗", Rule{ID: 1, AppID: 1, PathPattern: "/**", SubjectType: SubjectPublic,
			Decision: Allow, TimeWindow: "25:99-8"}, false},
	}
	for _, c := range cases {
		if err := c.r.Validate(); (err == nil) != c.ok {
			t.Errorf("%s: 期望 ok=%v，得到 %v", c.name, c.ok, err)
		}
	}
}
