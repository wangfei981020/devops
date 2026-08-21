package httpx

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func bind(t *testing.T, query string, keys ...string) PageQuery {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/x?"+query, nil)
	return BindPage(c, keys...)
}

func TestBindPageDefaults(t *testing.T) {
	q := bind(t, "")
	if q.Page != 1 || q.Size != DefaultSize {
		t.Fatalf("默认分页应为 1/%d，得到 %d/%d", DefaultSize, q.Page, q.Size)
	}
}

func TestBindPageIgnoresGarbage(t *testing.T) {
	// 分页参数写错应该退回默认值继续查，而不是让整个列表 400。
	// 用户看到"翻页没生效"，比看到一个错误页强。
	q := bind(t, "page=abc&size=-5")
	if q.Page != 1 || q.Size != DefaultSize {
		t.Fatalf("非法参数应退回默认值，得到 %d/%d", q.Page, q.Size)
	}
}

func TestBindPageCapsSize(t *testing.T) {
	// 没有上限的话，一个 size=1000000 就能把内存打满
	q := bind(t, "size=999999")
	if q.Size != MaxSize {
		t.Fatalf("size 应被截到 %d，得到 %d", MaxSize, q.Size)
	}
}

func TestBindPageSortPrefix(t *testing.T) {
	if q := bind(t, "sort=-cost"); q.SortBy != "cost" || !q.SortDesc {
		t.Fatalf("-cost 应解析成 cost/desc，得到 %s/%v", q.SortBy, q.SortDesc)
	}
	if q := bind(t, "sort=name"); q.SortBy != "name" || q.SortDesc {
		t.Fatalf("name 应解析成 name/asc，得到 %s/%v", q.SortBy, q.SortDesc)
	}
}

func TestBindPageOnlyDeclaredFilters(t *testing.T) {
	// 未声明的筛选键必须被忽略：前端拼错参数名会静默失效，
	// 而界面看起来筛选生效了 —— 这种错比报错难查得多，所以只认白名单。
	q := bind(t, "status=running&cluter=typo&cluster=g32", "status", "cluster")
	if q.Filters["status"] != "running" || q.Filters["cluster"] != "g32" {
		t.Fatalf("声明过的筛选没生效: %#v", q.Filters)
	}
	if _, ok := q.Filters["cluter"]; ok {
		t.Fatal("未声明的筛选键不该被接受")
	}
}

func TestBindPageAllMeansNoFilter(t *testing.T) {
	// 前端下拉的默认值是 "all"，它表示不筛选，不能当成一个真实取值去匹配
	q := bind(t, "status=all", "status")
	if _, ok := q.Filters["status"]; ok {
		t.Fatal("status=all 应视为不筛选")
	}
}

func TestOrderByRejectsInjection(t *testing.T) {
	allowed := map[string]string{"cost": "h.monthly_cost", "name": "c.name"}

	// 不在白名单里的一律退回 fallback。
	// 直接把 SortBy 拼进 SQL 就是注入：sort=name;DROP TABLE hosts--
	for _, evil := range []string{
		"name; DROP TABLE hosts--",
		"(SELECT 1)",
		"c.name",        // 即使是真实列名，没在白名单里也不许
		"cost) UNION (", //
	} {
		if got := bind2(evil).OrderBy(allowed, "c.id ASC"); got != "c.id ASC" {
			t.Fatalf("注入串 %q 应退回 fallback，得到 %q", evil, got)
		}
	}

	if got := bind2("-cost").OrderBy(allowed, "c.id ASC"); got != "h.monthly_cost DESC" {
		t.Fatalf("白名单字段应正常翻译，得到 %q", got)
	}
}

func bind2(sort string) PageQuery {
	desc := strings.HasPrefix(sort, "-")
	return PageQuery{SortBy: strings.TrimPrefix(sort, "-"), SortDesc: desc}
}

func TestLikeEscapesWildcards(t *testing.T) {
	// 用户搜 "50%" 时，% 若不转义就成了通配符，
	// 结果是"搜出一堆不相关的"，而没人会想到是转义问题
	w := &WhereBuilder{}
	w.Like("c.name LIKE ?", "50%_test")
	args := w.Args()
	if len(args) != 1 {
		t.Fatalf("应有 1 个参数，得到 %d", len(args))
	}
	got, _ := args[0].(string)
	if !strings.Contains(got, "\\%") || !strings.Contains(got, "\\_") {
		t.Fatalf("通配符未转义: %q", got)
	}
}

func TestLikeSkipsEmpty(t *testing.T) {
	w := &WhereBuilder{}
	w.Like("c.name LIKE ?", "")
	if w.SQL() != "" {
		t.Fatalf("空关键词不该产生条件，得到 %q", w.SQL())
	}
}

func TestWhereBuilderClone(t *testing.T) {
	// count 查询要复用 list 的条件，但不能共享底层数组 ——
	// 共享的话后续给 list 加条件会连带改掉 count，两个数字就对不上了
	base := &WhereBuilder{}
	base.Add("a = ?", 1)
	clone := base.Clone()
	base.Add("b = ?", 2)

	if strings.Contains(clone.SQL(), "b = ?") {
		t.Fatal("Clone 之后对原对象的修改不该影响副本")
	}
	if len(clone.Args()) != 1 {
		t.Fatalf("副本参数数应为 1，得到 %d", len(clone.Args()))
	}
}

func TestNewListNeverNil(t *testing.T) {
	// nil 切片序列化成 JSON null，前端 items.length 直接抛异常。
	// 而这只在"结果为空"时发生，有数据时一切正常，最容易漏测。
	r := NewList[int](nil, PageQuery{Page: 1, Size: 50}, 0)
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"items":[]`) {
		t.Fatalf("空列表必须序列化成 []，得到 %s", b)
	}
}

func TestOffset(t *testing.T) {
	if got := (PageQuery{Page: 3, Size: 50}).Offset(); got != 100 {
		t.Fatalf("第 3 页 offset 应为 100，得到 %d", got)
	}
}
