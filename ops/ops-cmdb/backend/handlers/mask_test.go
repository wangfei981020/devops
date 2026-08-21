package handlers

import "testing"

// 脱敏错了不会报错，只会安静地泄露或安静地写坏配置，所以必须单测。
func TestMaskToken(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"abc", "********"},               // 太短：全遮，别把长度也泄露了
		{"12345678", "********"},          // 边界：正好 8 位仍全遮
		{"123456789", "1234********6789"}, // 9 位起保留首尾各 4
		{"87f3c7f8-98a7-4a19-8355-140dff22c2e2", "87f3********c2e2"},
	}
	for _, c := range cases {
		if got := maskToken(c.in); got != c.want {
			t.Errorf("maskToken(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

// webhook 要保留到"能认出是哪个群"，但把真正是密钥的末段打掉
func TestMaskWebhookURL(t *testing.T) {
	in := "https://open.larksuite.com/open-apis/bot/v2/hook/abcd1234-5678-efgh"
	want := "https://open.larksuite.com/open-apis/bot/v2/hook/abcd********efgh"
	if got := maskWebhookURL(in); got != want {
		t.Errorf("= %q，期望 %q", got, want)
	}
	if maskWebhookURL("") != "" {
		t.Error("空串不该被加工")
	}
	// 没有斜杠时退化成普通 token 掩码，别 panic
	if got := maskWebhookURL("no-slash-token-value"); got == "no-slash-token-value" {
		t.Error("无斜杠的值也必须掩码")
	}
}

// 这个判定挡的是"管理员点一次保存就把 webhook 写成掩码串"。
// 判错方向的代价不对称：漏判 = 配置被写坏且要等到告警发不出去才发现。
func TestIsMasked(t *testing.T) {
	masked := []string{
		"abcd********efgh",
		"********",
		"https://open.larksuite.com/open-apis/bot/v2/hook/abcd********efgh",
	}
	for _, s := range masked {
		if !isMasked(s) {
			t.Errorf("%q 应判为掩码值", s)
		}
	}
	real := []string{
		"",
		"https://open.larksuite.com/open-apis/bot/v2/hook/abcd1234-5678-efgh",
		"87f3c7f8-98a7-4a19-8355-140dff22c2e2",
	}
	for _, s := range real {
		if isMasked(s) {
			t.Errorf("%q 是真值，不该被当成掩码而跳过写入", s)
		}
	}
}
