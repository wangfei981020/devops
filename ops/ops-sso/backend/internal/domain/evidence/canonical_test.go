package evidence

import (
	"encoding/json"
	"testing"
	"time"
)

// mysqlJSONRoundTrip 模拟 MySQL JSON 列干的事：把写进去的文本重排。
//
// 真实行为是「键按长度排序、再按字典序」，分隔符 `": "`。
// 这里不需要精确复刻 MySQL，只需要复刻**它会改写字节**这件事 ——
// 只要顺序变了，按原始字节算的哈希就必然对不上。
func mysqlJSONRoundTrip(t *testing.T, s string) string {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("测试数据不是合法 JSON: %v", err)
	}
	// 手工拼一个「键长度优先」的顺序，且带空格 —— 和 Go 的输出必然不同
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if len(keys[j]) < len(keys[i]) || (len(keys[j]) == len(keys[i]) && keys[j] < keys[i]) {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	out := "{"
	for i, k := range keys {
		if i > 0 {
			out += ", "
		}
		kb, _ := json.Marshal(k)
		vb, _ := json.Marshal(v[k])
		out += string(kb) + ": " + string(vb)
	}
	return out + "}"
}

// TestChainSurvivesJSONColumnRewrite 这是真实踩到的那个坑。
//
// 一条带 detail 的审计写进去、从 MySQL 的 JSON 列读回来，链必须仍然校验通过。
// 修复前：链上第一条（无 detail）能过，第二条一带 detail 就报 content_tampered，
// 而根本没人动过数据。
func TestChainSurvivesJSONColumnRewrite(t *testing.T) {
	at := time.Unix(1754740000, 0).UTC()

	detail := map[string]any{
		"note": "审计验证用，随后删除", "scope": "global", "effect": "allow",
		"enforced": false, "scope_id": int64(0), "subject_id": int64(4),
		"subject_type": "user",
	}

	// 写入侧：规范化后既参与哈希，也是存进库的那份
	stored := CanonicalJSON(detail)
	fields := map[string]any{"action": "policy.create", "detail": stored}
	e := Append(Entry{}, 1, at, fields)

	// 读回侧：库把字节重排了
	readBack := mysqlJSONRoundTrip(t, stored)
	if readBack == stored {
		t.Fatal("这个测试没意义了：模拟的 JSON 列没有改写字节")
	}

	// 按读回来的原始字节算 —— 必须断（证明这个坑是真的）
	naive := Verify([]Entry{{
		Seq: e.Seq, At: e.At, PrevHash: e.PrevHash, Hash: e.Hash,
		Fields: map[string]any{"action": "policy.create", "detail": readBack},
	}})
	if naive.OK {
		t.Fatal("按原始字节算竟然通过了 —— 说明这个测试没有复现出问题")
	}

	// 按规范化后算 —— 必须通过
	fixed := Verify([]Entry{{
		Seq: e.Seq, At: e.At, PrevHash: e.PrevHash, Hash: e.Hash,
		Fields: map[string]any{"action": "policy.create", "detail": CanonicalJSONString(readBack)},
	}})
	if !fixed.OK {
		t.Fatalf("规范化后仍然断裂：%+v", fixed)
	}
}

func TestCanonicalJSONIsOrderIndependent(t *testing.T) {
	a := CanonicalJSONString(`{"b":1,"a":2,"c":{"z":1,"y":[3,2,1]}}`)
	b := CanonicalJSONString(`{"c":{"y":[3,2,1],"z":1},"a":2,"b":1}`)
	if a != b {
		t.Fatalf("键顺序不同得到了不同结果:\n%s\n%s", a, b)
	}
	// 数组顺序**不能**被排序：[3,2,1] 和 [1,2,3] 是不同的值
	if c := CanonicalJSONString(`{"a":[1,2,3]}`); c == CanonicalJSONString(`{"a":[3,2,1]}`) {
		t.Fatal("数组被排序了 —— 那会让两条不同的记录算出同一个哈希")
	}
}

// 写入侧拿到的是 Go 的 int64，读回侧是 JSON 解析出来的 float64。
// 不统一成同一种数字类型的话，同一个值会算出两个哈希。
func TestCanonicalJSONNumbersMatchAcrossTypes(t *testing.T) {
	fromGo := CanonicalJSON(map[string]any{"n": int64(4), "big": int64(1) << 40})
	fromJSON := CanonicalJSONString(`{"n":4,"big":1099511627776}`)
	if fromGo != fromJSON {
		t.Fatalf("int64 与 float64 结果不同:\n%s\n%s", fromGo, fromJSON)
	}
}

// 解析不了就原样返回：这时候至少还能按字节比，不该凭空判定断链。
func TestCanonicalJSONStringPassesThroughGarbage(t *testing.T) {
	if got := CanonicalJSONString("not json"); got != "not json" {
		t.Fatalf("非法 JSON 被改写了: %q", got)
	}
	if got := CanonicalJSONString(""); got != "" {
		t.Fatalf("空串被改写了: %q", got)
	}
}

// 结构体和「从库里读回来的同一份数据」必须规范化成同一个字符串。
//
// 不成立的话，任何在审计 detail 里放结构体的调用方都会把链写断 ——
// 而断的那条记录谁也没动过。实测撞过一次（删应用的 AppDeps）。
func TestCanonicalStructMatchesParsedMap(t *testing.T) {
	// 字段顺序**故意**不是字典序：这正是 json.Marshal 会照搬的顺序
	type deps struct {
		OIDCClients int `json:"oidc_clients"`
		Routes      int `json:"routes"`
		PathRules   int `json:"path_rules"`
		Groups      int `json:"groups"`
	}
	write := map[string]any{
		"code":     "casc",
		"cascaded": deps{OIDCClients: 0, Routes: 1, PathRules: 1, Groups: 0},
	}

	// 校验侧看到的：detail 从 JSON 列读回来，全是 map/float64
	raw, err := json.Marshal(write)
	if err != nil {
		t.Fatal(err)
	}
	var read map[string]any
	if err := json.Unmarshal(raw, &read); err != nil {
		t.Fatal(err)
	}

	if got, want := CanonicalJSON(write), CanonicalJSON(read); got != want {
		t.Fatalf("结构体与解析回来的 map 规范化结果不同：\n写入侧 %s\n校验侧 %s", got, want)
	}
}

// 嵌套结构体同样要成立：真实的 detail 经常是两层。
func TestCanonicalNestedStruct(t *testing.T) {
	type inner struct {
		Z int `json:"z"`
		A int `json:"a"`
	}
	type outer struct {
		N inner  `json:"n"`
		S string `json:"s"`
	}
	v := map[string]any{"o": outer{N: inner{Z: 1, A: 2}, S: "x"}}
	raw, _ := json.Marshal(v)
	var read map[string]any
	if err := json.Unmarshal(raw, &read); err != nil {
		t.Fatal(err)
	}
	if CanonicalJSON(v) != CanonicalJSON(read) {
		t.Fatalf("嵌套结构体规范化不一致：%s vs %s", CanonicalJSON(v), CanonicalJSON(read))
	}
}
