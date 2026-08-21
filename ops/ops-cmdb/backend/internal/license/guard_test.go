package license

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"ops-kit/licensekit"
)

func TestIsWrite(t *testing.T) {
	// 只读降级只拦写操作。判错的方向很致命：
	// 把 GET 判成写，客户过期后连自己的数据都导不出来 —— 那不是催款是扣押
	writes := []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete}
	reads := []string{http.MethodGet, http.MethodHead, http.MethodOptions}
	for _, m := range writes {
		if !isWrite(m) {
			t.Errorf("%s 应判为写", m)
		}
	}
	for _, m := range reads {
		if isWrite(m) {
			t.Errorf("%s 不该判为写 —— 只读降级不能拦读", m)
		}
	}
}

func TestAlwaysWritableCoversSelfRescue(t *testing.T) {
	// 这几条是"过期之后还能不能自救"的全部依赖。
	// 少一条，客户付了钱也贴不上激活码，只能找我们远程改库。
	for _, k := range []string{
		"POST /api/login",   // 进不去就贴不了码
		"POST /api/license", // 贴激活码本身
	} {
		if !alwaysWritable[k] {
			t.Errorf("%s 必须在只读降级下仍可写，否则过期后无法自救", k)
		}
	}
}

func TestReadOnlyGuard(t *testing.T) {
	gin.SetMode(gin.TestMode)

	run := func(mgr *Manager, method, path string) int {
		r := gin.New()
		r.Use(ReadOnlyGuard(mgr))
		h := func(c *gin.Context) { c.Status(http.StatusOK) }
		r.GET("/api/hosts", h)
		r.POST("/api/hosts", h)
		r.POST("/api/license", h)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		return w.Code
	}

	// 未激活 + CEWritable=true → 可写（CE 得能配数据源，否则装上去是个空壳）
	ce := NewManager()
	if got := run(ce, http.MethodPost, "/api/hosts"); got != http.StatusOK {
		t.Errorf("CE 档写操作应放行，得到 %d", got)
	}

	// 构造一个只读态。
	//
	// 用 CEWritable=false 的未激活管理器：ReadOnly() 对它返回 true，
	// 与"过期"走的是同一个判定出口，因此足以覆盖本中间件的行为。
	// 真正的状态机（active/grace/expired/lapsed…）由 licensekit 自己的测试锁。
	ro := &Manager{Manager: licensekit.NewManager(licensekit.Options{
		Product:    ProductID,
		CEWritable: false,
	})}
	if got := run(ro, http.MethodGet, "/api/hosts"); got != http.StatusOK {
		t.Errorf("只读降级下**读**必须放行，得到 %d", got)
	}
	if got := run(ro, http.MethodPost, "/api/hosts"); got != http.StatusForbidden {
		t.Errorf("只读降级下写应被拦，得到 %d", got)
	}
	// 自救通道
	if got := run(ro, http.MethodPost, "/api/license"); got != http.StatusOK {
		t.Errorf("只读降级下仍必须能激活授权，得到 %d —— 否则过期即死锁", got)
	}
}

// 门控表与 implemented 表必须同步。
//
// 这两张表分开在两个文件里，而它们必须同时改：
//
//	只点亮不挂门控 = 白送（接口根本不问 Has()，谁都能用，且没有任何迹象）
//	只挂门控不点亮 = 全关（Has() 恒 false，已购客户也被挡在外面）
//
// 后者会被客户当场发现，前者不会 —— 它只是永远收不到那份钱。
func TestFeatureRoutesAreImplemented(t *testing.T) {
	for route, f := range featureRoutes {
		if !implemented[string(f)] {
			t.Errorf("%s 挂了功能门控 %q，但它不在 implemented 里 —— "+
				"Has() 恒为 false，这条接口对所有人关闭，包括已购客户", route, f)
		}
		if !allFeatures[f] {
			t.Errorf("%s 引用了未知功能 %q", route, f)
		}
	}

	// 反向：点亮了却没有任何门控的功能。
	// 例外必须写在 notRouteGated 里并说明理由，不允许沉默地存在。
	for name := range implemented {
		f := Feature(name)
		if _, ok := notRouteGated[f]; ok {
			continue
		}
		gated := false
		for _, want := range featureRoutes {
			if want == f {
				gated = true
				break
			}
		}
		// ⚠️ 只读功能挂在 featureReadPrefixes 上，也要算进来 ——
		//	漏掉这一段的话，第一个纯只读付费功能会被报成"白送"，
		//	而它其实挂着门控（OPSCMDB-072）。
		if !gated {
			for _, r := range featureReadPrefixes {
				if r.Feature == f {
					gated = true
					break
				}
			}
		}
		if !gated {
			t.Errorf("功能 %q 已点亮，却没有任何路由挂它的门控 —— "+
				"等于白送。要么补进 featureRoutes / featureReadPrefixes，"+
				"要么写进 notRouteGated 说明理由", name)
		}
	}
}

// 门控只针对写接口。挂到读接口上，客户会连自己的数据都看不到。
func TestFeatureRoutesAreWritesOnly(t *testing.T) {
	for route := range featureRoutes {
		method, _, ok := strings.Cut(route, " ")
		if !ok {
			t.Fatalf("路由 key 格式应为 `METHOD /path`，实际 %q", route)
		}
		if !isWrite(method) {
			t.Errorf("%s 是读接口，不该挂功能门控 —— 只读一律放行", route)
		}
	}
}

// 已点亮的功能，被授权时放行、未授权时拦截。
func TestFeatureGuard(t *testing.T) {
	gin.SetMode(gin.TestMode)

	run := func(mgr *Manager) int {
		r := gin.New()
		r.Use(FeatureGuard(mgr))
		r.POST("/api/k8s/cost/snapshot", func(c *gin.Context) { c.Status(http.StatusOK) })
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/k8s/cost/snapshot", nil))
		return w.Code
	}

	// 社区版：没买成本归因，写操作被拦
	if got := run(NewManager()); got != http.StatusForbidden {
		t.Errorf("CE 档写成本接口应被拦，得到 %d", got)
	}

	// 买了 professional：放行
	m := NewManager()
	m.Load(&licensekit.Payload{
		Products:  map[string]licensekit.ProductGrant{ProductID: {Features: []string{"plan:professional"}}},
		ExpiresAt: time.Now().AddDate(1, 0, 0),
	}, "")
	if got := run(m); got != http.StatusOK {
		t.Errorf("professional 档应放行成本接口，得到 %d", got)
	}
}

// 路由自检本身要能发现问题 —— 一个永远返回空的检查等于没有检查。
func TestUnmatchedFeatureRoutes(t *testing.T) {
	var live []gin.RouteInfo
	for key := range featureRoutes {
		method, path, _ := strings.Cut(key, " ")
		live = append(live, gin.RouteInfo{Method: method, Path: path})
	}
	if got := UnmatchedFeatureRoutes(live); len(got) > 0 {
		t.Errorf("路由全部存在时应为空，得到 %v", got)
	}
	if got := UnmatchedFeatureRoutes(live[1:]); len(got) != 1 {
		t.Errorf("少一条路由时应恰好点名一条，得到 %v", got)
	}
	if got := UnmatchedFeatureRoutes(nil); len(got) != len(featureRoutes) {
		t.Errorf("路由表全空时应点名全部 %d 条，得到 %v", len(featureRoutes), got)
	}
}

// ★ 只读功能的门控（OPSCMDB-072）。
//
// 🔴 这一组锁的是**两条规则不能再混起来**：
//
//	ReadOnlyGuard  授权过期 → 只拦写（读要放行，否则是扣押数据）
//	FeatureGuard   功能没买 → 读写都拦（否则纯只读的功能永远免费）
//
// 混起来的后果实测过：exposure 在 features_missing 里、侧栏打了 EE 标记，
// 而 GET /api/exposure-list 照样 200 返回全量 63 条。
func TestFeatureGuard_只读功能没买时读也要拦(t *testing.T) {
	gin.SetMode(gin.TestMode)

	run := func(t *testing.T, granted []string, method, path string) int {
		t.Helper()
		now := time.Now()
		m := newManagerAt(func() time.Time { return now })
		m.Load(payload(grant(granted, nil), now.AddDate(1, 0, 0), ""), "")
		r := gin.New()
		r.Use(FeatureGuard(m))
		h := func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) }
		switch method {
		case http.MethodGet:
			r.GET(path, h)
		default:
			r.POST(path, h)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		return w.Code
	}

	t.Run("没买 exposure 时读接口必须被拦", func(t *testing.T) {
		// 只给 cost，不给 exposure
		if got := run(t, []string{string(FeatureCost)}, http.MethodGet, "/api/exposure-list"); got != http.StatusForbidden {
			t.Fatalf("GET /api/exposure-list 应被拦（403），实际 %d —— 只读功能又变成白送了", got)
		}
	})

	t.Run("买了 exposure 时读接口必须放行", func(t *testing.T) {
		if got := run(t, []string{string(FeatureExposure)}, http.MethodGet, "/api/exposure-list"); got != http.StatusOK {
			t.Fatalf("买了却被拦（%d）—— 这是「买了不给用」，比白送更糟", got)
		}
	})

	t.Run("不属于任何付费功能的读接口一律放行", func(t *testing.T) {
		// CE 的能力不经过 Has()，一个 feature 都没买也要能用
		if got := run(t, nil, http.MethodGet, "/api/hosts"); got != http.StatusOK {
			t.Fatalf("CE 的只读接口被误拦（%d）—— 那会让免费版整个不可用", got)
		}
	})
}

// ★ 只读功能的路由前缀必须指向已点亮的 feature。
//
// 前缀写了但 feature 没点亮 → Has() 恒 false → **所有人**（含已购客户）都被挡在外面。
func TestFeatureReadPrefixesAreImplemented(t *testing.T) {
	for _, r := range featureReadPrefixes {
		if !implemented[string(r.Feature)] {
			t.Errorf("%s 挂了读门控但 feature %s 没在 implemented 里点亮 —— 已购客户也会被拦",
				r.Prefix, r.Feature)
		}
		if r.Prefix == "" || len(r.Prefix) < len("/api/xx") {
			t.Errorf("前缀 %q 太短，会误伤一大片接口", r.Prefix)
		}
	}
}
