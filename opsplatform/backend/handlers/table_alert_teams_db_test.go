package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"opsplatform/database"

	_ "github.com/go-sql-driver/mysql"
)

// 桌台告警接 Teams：建表/补字段、Teams 群保存校验、通讯人 Teams 账号、Lark ID → Teams 账号、
// 发送记录的渠道类型。全是 SQL 行为，要真连库测。默认跳过：
//
//	TA_TEAMS_TEST_DSN='root:xxx@tcp(127.0.0.1:33319)/ta_teams_test?parseTime=True&loc=Local' \
//	  go test ./handlers/ -run TestTableAlertTeams -v
func taTeamsDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TA_TEAMS_TEST_DSN")
	if dsn == "" {
		t.Skip("未设置 TA_TEAMS_TEST_DSN，跳过（需要一个真 MySQL）")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil || db.Ping() != nil {
		t.Fatalf("连接测试库失败: %v", err)
	}
	rows, _ := db.Query(`SELECT table_name FROM information_schema.tables WHERE table_schema=DATABASE()`)
	var names []string
	for rows.Next() {
		var n string
		rows.Scan(&n)
		names = append(names, n)
	}
	rows.Close()
	for _, n := range names {
		db.Exec("DROP TABLE IF EXISTS " + n)
	}
	db.Exec(`CREATE TABLE system_settings (setting_key VARCHAR(100) PRIMARY KEY, setting_value TEXT,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP)`)
	database.DB = db
	return db
}

func taCall(t *testing.T, h http.HandlerFunc, method string, body interface{}, vars map[string]string) (int, string) {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(method, "/x", strings.NewReader(string(b)))
	req = req.WithContext(context.WithValue(req.Context(), ctxAPIKeyID, int64(1)))
	if vars != nil {
		req = mux.SetURLVars(req, vars)
	}
	w := httptest.NewRecorder()
	h(w, req)
	return w.Code, w.Body.String()
}

const teamsHook = "https://default00000000000000000000000000000.00.environment.api.powerplatform.com:443/powerautomate/automations/direct/workflows/0a1b2c/triggers/manual/paths/invoke?api-version=1&sig=Sig_Test_123"

func TestTableAlertTeamsFlow(t *testing.T) {
	db := taTeamsDB(t)
	// 老库没有新列：先建一遍、删掉新列、重跑必须补回来
	if err := database.InitTableAlertTables(); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	for _, c := range [][2]string{{"table_alert_lark_bots", "kind"}, {"table_alert_contacts", "teams_id"}, {"table_alert_notify_logs", "channel_kind"}} {
		db.Exec("ALTER TABLE " + c[0] + " DROP COLUMN " + c[1])
	}
	if err := database.InitTableAlertTables(); err != nil {
		t.Fatalf("重跑建表失败: %v", err)
	}
	for _, c := range [][2]string{{"table_alert_lark_bots", "kind"}, {"table_alert_contacts", "teams_id"}, {"table_alert_notify_logs", "channel_kind"}} {
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND COLUMN_NAME=?`, c[0], c[1]).Scan(&n)
		if n != 1 {
			t.Fatalf("老库升级后 %s.%s 没补回来", c[0], c[1])
		}
	}

	// ── Teams 群：地址校验、清洗不可见字符、签名不存
	code, body := taCall(t, HandleTASaveBot, "POST", map[string]interface{}{"name": "G32-PROD告警群", "kind": "teams",
		"webhook": "https://open.larksuite.com/open-apis/bot/v2/hook/x", "enabled": true}, nil)
	if code != 400 || !strings.Contains(body, "environment.api.powerplatform.com") {
		t.Errorf("Teams 群填 Lark 地址应被拒: %d %s", code, body)
	}
	code, body = taCall(t, HandleTASaveBot, "POST", map[string]interface{}{"name": "G32-PROD告警群", "kind": "teams",
		"webhook": " " + strings.Replace(teamsHook, "/triggers/", "/trig​gers/", 1) + " ", "secret": "should-not-store", "enabled": true}, nil)
	if code != 200 {
		t.Fatalf("Teams 群保存失败: %d %s", code, body)
	}
	var stored, secret, kind, botID string
	db.QueryRow(`SELECT id, webhook, secret, kind FROM table_alert_lark_bots WHERE name='G32-PROD告警群'`).Scan(&botID, &stored, &secret, &kind)
	if stored != teamsHook || secret != "" || kind != "teams" {
		t.Errorf("保存结果不对：地址清洗=%v secret=%q kind=%q", stored == teamsHook, secret, kind)
	}
	// 不允许改类型
	code, _ = taCall(t, HandleTASaveBot, "POST", map[string]interface{}{"id": botID, "name": "x", "kind": "lark", "enabled": true}, nil)
	if code != 400 {
		t.Errorf("改类型应被拒，实际 %d", code)
	}
	// 老的 Lark 群不传 kind 照常能建
	code, _ = taCall(t, HandleTASaveBot, "POST", map[string]interface{}{"name": "老Lark群", "webhook": "https://open.larksuite.com/open-apis/bot/v2/hook/x", "enabled": true}, nil)
	if code != 200 {
		t.Errorf("Lark 群（不传 kind）应照常保存，实际 %d", code)
	}
	code, body = taCall(t, HandleTAListBots, "GET", nil, nil)
	if code != 200 || !strings.Contains(body, `"kind":"teams"`) || !strings.Contains(body, `"kind":"lark"`) || strings.Contains(body, "Sig_Test_123") {
		t.Errorf("群列表应带 kind 且不泄露完整 sig: %s", body)
	}

	// ── 默认后缀 + 通讯人
	code, _ = taCall(t, HandleTASaveTeamsSettings, "PUT", map[string]string{"mention_domain": "@EXAMPLE.com"}, nil)
	if code != 200 || taTeamsDomain() != "example.com" {
		t.Fatalf("默认后缀应规整为 example.com，实际 %d %q", code, taTeamsDomain())
	}
	code, _ = taCall(t, HandleTASaveContact, "POST", map[string]string{"name": "alice", "lark_id": "ou_alice"}, nil)
	code2, _ := taCall(t, HandleTASaveContact, "POST", map[string]string{"name": "carol", "lark_id": "ou_carol", "teams_id": "Carol.W@EXAMPLE.com"}, nil)
	code3, _ := taCall(t, HandleTASaveContact, "POST", map[string]string{"name": "Mary Ann Lee", "lark_id": "ou_md"}, nil)
	code4, _ := taCall(t, HandleTASaveContact, "POST", map[string]string{"name": "bad", "lark_id": "ou_bad", "teams_id": "not-an-email"}, nil)
	if code != 200 || code2 != 200 || code3 != 200 || code4 != 400 {
		t.Fatalf("通讯人保存结果不对: %d %d %d %d（最后一个应 400）", code, code2, code3, code4)
	}
	_, body = taCall(t, HandleTAListContacts, "GET", nil, nil)
	for _, want := range []string{`"teams_account":"alice@example.com","teams_derived":true`, `"teams_account":"carol.w@example.com","teams_derived":false`, `"teams_id":"carol.w@example.com"`} {
		if !strings.Contains(body, want) {
			t.Errorf("通讯人列表缺少 %s\n%s", want, body)
		}
	}

	// ── Lark ID → Teams 提及
	ms := taTeamsMentions([]string{"ou_alice", "ou_carol", "ou_md", "ou_gone_abcdef123"})
	if len(ms) != 4 || ms[0].Account != "alice@example.com" || !ms[0].Derived || ms[1].Account != "carol.w@example.com" ||
		ms[2].Account != "" || ms[3].Account != "" || !strings.Contains(ms[3].Name, "def123") {
		t.Errorf("Lark ID 换 Teams 提及不对: %+v", ms)
	}

	// ── 发送记录带渠道类型
	db.Exec(`INSERT INTO table_alert_notify_logs (id, event_id, env_name, table_no, bot_id, bot_name, kind, seq, at_lark_ids, title, body, ok, error_msg, sent_at, channel_kind)
		VALUES ('l1','','PROD','N13',?, 'G32-PROD告警群','alert',1,'','t','b',1,'',NOW(),'teams')`, botID)
	_, body = taCall(t, HandleTAListNotifyLogs, "GET", nil, nil)
	if !strings.Contains(body, `"channel_kind":"teams"`) {
		t.Errorf("发送记录应带 channel_kind: %s", body)
	}
}
