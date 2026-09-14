package alert

import (
	"fmt"
	"os"
	"testing"
	"time"

	"opsplatform-alert-backend/config"
	"opsplatform-alert-backend/database"
	"opsplatform-alert-backend/timezone"
)

// Sends one real reminder to the channels bound to a rule and prints what the
// group receives, so the wording can be judged rather than imagined.
//
//	ALERT_DEMO_RULE=2 ALERT_E2E=1 go test ./alert/ -run MuteReminderDemo -v
//
// Opt-in twice over, because it puts a message in a real chat.
func TestMuteReminderDemoSend(t *testing.T) {
	ruleID := os.Getenv("ALERT_DEMO_RULE")
	if os.Getenv("ALERT_E2E") == "" || ruleID == "" {
		t.Skip("set ALERT_E2E=1 and ALERT_DEMO_RULE=<id> to send a real reminder")
	}

	cfg := config.Load()
	if err := database.InitMySQL(cfg); err != nil {
		t.Fatalf("mysql: %v", err)
	}
	if err := timezone.Set(database.GetSetting(database.SettingDisplayTimezone, cfg.DisplayTimezone)); err != nil {
		t.Fatalf("timezone: %v", err)
	}

	var id int
	fmt.Sscanf(ruleID, "%d", &id)

	var name string
	var channelID int
	if err := database.DB.QueryRow(
		`SELECT name, COALESCE(lark_config_id, 0) FROM alert_rules WHERE id = ?`, id).
		Scan(&name, &channelID); err != nil {
		t.Fatalf("rule %d: %v", id, err)
	}

	m := expiringMute{
		RuleID:    id,
		RuleName:  name,
		ChannelID: channelID,
		GroupKey:  "order-svc-canary",
		Reason:    "灰度发布中，预计 1 小时",
		CreatedBy: "admin",
		MuteUntil: time.Now().Add(10 * time.Minute),
		SecondsTo: 600,
	}

	fmt.Printf("\n=== 群里收到的正文 ===\n屏蔽即将到期\n%s\n\n", muteReminderBody(m))

	resp, err := sendMuteReminder(m)
	if err != nil {
		t.Fatalf("send: %v (per-channel: %s)", err, resp)
	}
	fmt.Printf("=== 各渠道回执 ===\n%s\n", resp)
}
