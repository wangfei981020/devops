package appcat

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateCode(t *testing.T) {
	ok := []string{"jira", "harbor", "minio-console", "app1", "a1", "x-1-y"}
	for _, c := range ok {
		if err := ValidateCode(c); err != nil {
			t.Errorf("%q 应合法：%v", c, err)
		}
	}
	bad := map[string]string{
		"":                      "空",
		"a":                     "只有一位",
		"Jira":                  "大写 —— MySQL 排序规则不区分大小写而 Go map 区分，同一个 code 在库里唯一、在内存里是两个键",
		"my_app":                "下划线",
		"-jira":                 "以短横线开头",
		"jira-":                 "以短横线结尾",
		"两个中文":                  "非 ASCII",
		strings.Repeat("a", 65): "超长",
	}
	for c, why := range bad {
		if err := ValidateCode(c); err == nil {
			t.Errorf("%q 应被拒（%s）", c, why)
		} else if !errors.Is(err, ErrInvalidCode) {
			t.Errorf("%q 的错误类型不对：%v", c, err)
		}
	}
}

func TestConnectTypeZeroChange(t *testing.T) {
	// 「零改造接入」是售前话术与统计口径，必须只有一处定义 ——
	// 各处自己判断的话，两个页面上的数字会对不上
	if !ConnectGateway.ZeroChange() || !ConnectFormFill.ZeroChange() {
		t.Error("网关代管与表单注入属于零改造")
	}
	if ConnectOIDC.ZeroChange() || ConnectSAML.ZeroChange() {
		t.Error("OIDC/SAML 需要应用侧支持，不算零改造")
	}
	if ConnectType("carrier-pigeon").Valid() {
		t.Error("未知接入方式必须判为非法")
	}
}

func TestAppValidate(t *testing.T) {
	base := func() App {
		return App{Code: "jira", Name: "Jira", ConnectType: ConnectSAML, Env: "PROD"}
	}
	if err := base().Validate(); err != nil {
		t.Fatalf("正常应用应通过：%v", err)
	}

	cases := []struct {
		name string
		mut  func(*App)
	}{
		{"空名称", func(a *App) { a.Name = "  " }},
		{"未知接入方式", func(a *App) { a.ConnectType = "telnet" }},
		{"空环境", func(a *App) { a.Env = "" }},
	}
	for _, c := range cases {
		a := base()
		c.mut(&a)
		if err := a.Validate(); err == nil {
			t.Errorf("%s 应被拒", c.name)
		}
	}
}

// ★ 主分组必须在归属列表里 —— 否则门户不知道把它排到哪一栏，
// 表现是应用"消失"在界面上，而不是报错
func TestPrimaryGroupMustBeAmongGroups(t *testing.T) {
	a := App{Code: "jira", Name: "Jira", ConnectType: ConnectSAML, Env: "PROD",
		GroupIDs: []int64{1, 2}, PrimaryGroupID: 9}
	if err := a.Validate(); err == nil {
		t.Fatal("主分组不在归属列表里必须报错")
	}

	a.PrimaryGroupID = 2
	if err := a.Validate(); err != nil {
		t.Fatalf("主分组在列表里应通过：%v", err)
	}

	// 不指定主分组是允许的（落到「未分组」栏）
	a.PrimaryGroupID = 0
	if err := a.Validate(); err != nil {
		t.Fatalf("不指定主分组应允许：%v", err)
	}
}

// 环境值原样照搬客户的枚举，不做白名单 ——
// 客户内部叫 "生产A" / "PRE" / "gray" 都是他们的自由，
// 我们做白名单只会逼他们改自己的术语
func TestEnvIsNotWhitelisted(t *testing.T) {
	for _, env := range []string{"PROD", "UAT", "gray", "PRE-2", "生产A"} {
		a := App{Code: "x1", Name: "X", ConnectType: ConnectOIDC, Env: env}
		if err := a.Validate(); err != nil {
			t.Errorf("环境 %q 应被接受：%v", env, err)
		}
	}
}

func TestGroupValidate(t *testing.T) {
	if err := (Group{Code: "devtool", Name: "研发工具"}).Validate(); err != nil {
		t.Fatalf("正常分组应通过：%v", err)
	}
	if err := (Group{Code: "devtool", Name: " "}).Validate(); err == nil {
		t.Error("空名称应被拒")
	}
	if err := (Group{Code: "DevTool", Name: "研发工具"}).Validate(); err == nil {
		t.Error("大写标识应被拒")
	}
}
