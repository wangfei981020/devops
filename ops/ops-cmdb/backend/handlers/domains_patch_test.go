package handlers

import (
	"encoding/json"
	"strings"
	"testing"
)

// 🔴 守的是 OPSCMDB-083：只想改到期日，域名的**名字被清空了**。
//
//	PUT /api/domains/171  {"expiry_at": "2026-09-02"}  → 200 {"ok": true}
//	之后：ci_id=171  name=""
//
// 界面上一直看不出来（表单总是全量提交），而 MCP / 脚本 / AI 按 REST 直觉
// 发部分字段就会静默清空其余的。
func TestDomainPatchOnlyTouchesSentFields(t *testing.T) {
	parse := func(body string) domainPatch {
		var in domainPatch
		if err := json.Unmarshal([]byte(body), &in); err != nil {
			t.Fatalf("解析请求体失败: %v", err)
		}
		return in
	}

	t.Run("只传到期日：cis 一列都不能动", func(t *testing.T) {
		ci, dom := buildDomainPatch(parse(`{"expiry_at":"2026-09-02"}`))
		if !ci.Empty() {
			t.Fatalf("cis 不该被写：SET %s", ci.SQL())
		}
		if got := dom.SQL(); got != "expiry_at=NULLIF(?, '')" {
			t.Fatalf("domains 的 SET = %q", got)
		}
	})

	t.Run("全量提交（界面走的路径）：该改的都要改", func(t *testing.T) {
		ci, dom := buildDomainPatch(parse(`{
			"name":"a.example.com","project":"p","env":"prod","module":"m",
			"owner":"o","status":"active","dns_provider":"cf","expiry_at":"2027-01-01",
			"registrar_id":3}`))
		for _, col := range []string{"name=?", "project=?", "env=?", "module=?", "owner=?", "status=?"} {
			if !strings.Contains(ci.SQL(), col) {
				t.Errorf("全量提交却漏了 %s：SET %s", col, ci.SQL())
			}
		}
		if len(ci.Args()) != 6 {
			t.Errorf("参数个数 %d，want 6", len(ci.Args()))
		}
		if !strings.Contains(dom.SQL(), "registrar_id=?") || !strings.Contains(dom.SQL(), "dns_provider=?") {
			t.Errorf("domains 的 SET = %q", dom.SQL())
		}
	})

	t.Run("显式传空串 = 真的清空（不能被当成没传）", func(t *testing.T) {
		// 三态的第三态：调用方确实想把 owner 清掉
		ci, _ := buildDomainPatch(parse(`{"owner":""}`))
		if ci.SQL() != "owner=?" {
			t.Fatalf("显式清空没生效：SET %q", ci.SQL())
		}
		if v, ok := ci.Args()[0].(string); !ok || v != "" {
			t.Fatalf("参数不是空串：%#v", ci.Args()[0])
		}
	})

	t.Run("空 body：两张表都不动", func(t *testing.T) {
		ci, dom := buildDomainPatch(parse(`{}`))
		if !ci.Empty() || !dom.Empty() {
			t.Fatalf("空 body 却拼出了 SET：cis=%q domains=%q", ci.SQL(), dom.SQL())
		}
	})

	// ⚠️ 反向：如果哪天有人把字段改回非指针类型，上面「只传到期日」那条会红 ——
	//	因为零值会让 name 等六列全部进 SET。这条断言把意图写死。
	t.Run("字段必须是指针类型", func(t *testing.T) {
		var in domainPatch
		if err := json.Unmarshal([]byte(`{"expiry_at":"x"}`), &in); err != nil {
			t.Fatal(err)
		}
		if in.Name != nil {
			t.Fatal("没传 name，它却不是 nil —— 三态塌成了两态")
		}
	})
}

// patchSet 本身的行为。
func TestPatchSetSkipsNil(t *testing.T) {
	s := "v"
	p := &patchSet{}
	p.Add("a", (*string)(nil))
	p.Add("b", &s)
	p.AddExpr(false, "c=?", 1)
	p.AddExpr(true, "d=NULLIF(?, '')", "")
	if got := p.SQL(); got != "b=?, d=NULLIF(?, '')" {
		t.Fatalf("SQL = %q", got)
	}
	if len(p.Args()) != 2 {
		t.Fatalf("Args = %#v", p.Args())
	}
	if (&patchSet{}).Empty() != true {
		t.Fatal("空 patchSet 的 Empty() 该是 true")
	}
}

// TestPatchSetFloatSkipsNil 费率类字段用 float64 时的三态。
//
// 🔴 `cloud_compute_rates` 只传 vcpu 费率的话，用普通 float64 会把
// 内存费率写成 0 —— 那等于在成本核算里把内存算成免费的，
// 而账面上一切正常，没有任何报错（OPSCMDB-083）。
func TestPatchSetFloatSkipsNil(t *testing.T) {
	v := 0.031
	p := &patchSet{}
	p.Add("vcpu_hour_usd", &v)
	p.Add("ram_gb_hour_usd", (*float64)(nil))
	if got := p.SQL(); got != "vcpu_hour_usd=?" {
		t.Fatalf("没传的费率被写进了 SET：%q", got)
	}
	if len(p.Args()) != 1 || p.Args()[0].(float64) != v {
		t.Fatalf("Args = %#v", p.Args())
	}

	// 反向：显式传 0 是合法的（某些机型内存确实不单独计价）
	zero := 0.0
	p2 := &patchSet{}
	p2.Add("ram_gb_hour_usd", &zero)
	if p2.Empty() {
		t.Fatal("显式传 0 被当成了没传 —— 那就再也改不成 0 了")
	}
}
