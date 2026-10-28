package handlers

import "testing"

func TestValidateChannel(t *testing.T) {
	lark := "https://open.larksuite.com/open-apis/bot/v2/hook/abc"
	teams := "https://abc.environment.api.powerplatform.com:443/powerautomate/automations/direct/workflows/x/triggers/manual/paths/invoke?sig=s"
	cases := []struct {
		lark, teams string
		ok          bool
	}{
		{lark, "", true},
		{"", teams, true},
		{lark, teams, true},
		{"", "", false},
		{"https://example.com/hook", "", false},
		{lark, "https://example.com/powerautomate/x?sig=1", false},
	}
	for _, c := range cases {
		if err := validateChannel(c.lark, c.teams); (err == nil) != c.ok {
			t.Errorf("validateChannel(%q, %q) err=%v, want ok=%v", c.lark, c.teams, err, c.ok)
		}
	}
}

func TestLarkBotReqAuditViewHidesTeams(t *testing.T) {
	u := "https://abc.environment.api.powerplatform.com/powerautomate/x?sig=SECRET"
	m := auditDetailFromReq(larkBotReq{Name: "n", TeamsWebhook: &u}.auditView())
	if v, _ := m["teams_webhook"].(string); v == u || v == "" {
		t.Fatalf("审计里 Teams 地址应被替换: %v", m["teams_webhook"])
	}
}
