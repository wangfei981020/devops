package handlers

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"opsplatform-alert-backend/alert"
	"opsplatform-alert-backend/timezone"
)

// A picked day is a day in the display timezone, not in UTC. This is the whole
// bug: the bounds used to go to MySQL as bare strings, which the +00:00 session
// read as UTC, so asking for 2026-09-28 in Beijing actually searched 08:00 on
// the 28th through 08:00 on the 29th — it missed that morning's alerts and
// picked up the next day's. The log cleaner runs the same two bounds through a
// DELETE, so the shift removed the wrong rows outright.
func TestAlertLogFiltersAnchorsDatesInDisplayZone(t *testing.T) {
	if err := timezone.Set("Asia/Shanghai"); err != nil {
		t.Fatalf("set timezone: %v", err)
	}
	t.Cleanup(func() { timezone.Set("") })

	_, args := alertLogFilters(httptest.NewRequest("GET",
		"/?start_date=2026-09-28&end_date=2026-09-28", nil))

	if len(args) != 2 {
		t.Fatalf("want 2 bounds, got %d: %v", len(args), args)
	}

	start, ok := args[0].(time.Time)
	if !ok {
		t.Fatalf("start bound is %T, want time.Time — a string would be read as UTC", args[0])
	}
	end := args[1].(time.Time)

	// 00:00 Beijing on the 28th is 16:00 UTC on the 27th.
	if got := start.UTC().Format("2006-01-02 15:04:05"); got != "2026-09-27 16:00:00" {
		t.Errorf("start = %s UTC, want 2026-09-27 16:00:00", got)
	}
	if got := end.UTC().Format("2006-01-02 15:04:05"); got != "2026-09-28 15:59:59" {
		t.Errorf("end = %s UTC, want 2026-09-28 15:59:59", got)
	}

	// An alert at 03:00 Beijing on the 28th falls inside the picked day; one at
	// 04:00 Beijing on the 29th does not. Under the old bounds it was the other
	// way round, which is what made the filter look random.
	early := time.Date(2026, 9, 27, 19, 0, 0, 0, time.UTC)
	late := time.Date(2026, 9, 28, 20, 0, 0, 0, time.UTC)
	if early.Before(start) || early.After(end) {
		t.Error("09-28 03:00 Beijing should be inside the picked day")
	}
	if !late.After(end) {
		t.Error("09-29 04:00 Beijing should be outside the picked day")
	}
}

// Every filter has to reach the SQL, because the count and the page query share
// this one clause. A filter that lands in neither silently widens the result.
func TestAlertLogFiltersCoverEveryParameter(t *testing.T) {
	where, args := alertLogFilters(httptest.NewRequest("GET",
		"/?rule_id=7&status=success&severity=S1&project_id=3", nil))

	for _, frag := range []string{"rule_id = ?", "status = ?", "severity = ?", "alert_projects"} {
		if !strings.Contains(where, frag) {
			t.Errorf("where clause missing %q: %s", frag, where)
		}
	}
	// rule_id, status, severity, and project_id twice (id OR parent_id).
	if len(args) != 5 {
		t.Errorf("want 5 args, got %d: %v", len(args), args)
	}
}

// A project id matches both levels of the tree with one parameter: given a
// top-level id it has to reach the rules filed under its environments too,
// which is where rules actually live.
func TestAlertLogFiltersProjectMatchesBothLevels(t *testing.T) {
	where, args := alertLogFilters(httptest.NewRequest("GET", "/?project_id=1", nil))

	if !strings.Contains(where, "id = ? OR parent_id = ?") {
		t.Errorf("project filter must match the node and its children: %s", where)
	}
	if len(args) != 2 || args[0] != 1 || args[1] != 1 {
		t.Errorf("want the id bound twice, got %v", args)
	}
}

func TestAlertLogFiltersIgnoresJunk(t *testing.T) {
	where, args := alertLogFilters(httptest.NewRequest("GET",
		"/?rule_id=abc&project_id=0&start_date=not-a-date", nil))

	if where != "1=1" {
		t.Errorf("unparseable values must not become conditions, got: %s", where)
	}
	if len(args) != 0 {
		t.Errorf("want no args, got %v", args)
	}
}

// The preview panel exists to answer "what was actually asked, and did the
// selector match anything". An empty result is either "nothing was logged" or
// "this selector matched no stream", and only the query plus the line count
// tell those apart.
func TestFormatNamespacedQueriesShowsQueryAndLineCount(t *testing.T) {
	out := formatNamespacedQueries([]alert.NamespacedQuery{
		{Namespace: "g32-openapi", LogQL: `{namespace="g32-openapi"} |= "ERROR"`, LineCount: 3},
		{Namespace: "g32-wallet", LogQL: `{namespace="g32-wallet"} |= "ERROR"`, LineCount: 0},
	})

	for _, want := range []string{
		`{namespace="g32-openapi"} |= "ERROR"`,
		"# g32-openapi → 返回 3 行",
		`{namespace="g32-wallet"} |= "ERROR"`,
		"# g32-wallet → 返回 0 行",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}

	// The statement must sit on a line of its own so it can be copied into the
	// log explorer, with the annotation commented out below it.
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.Contains(line, "返回") && !strings.HasPrefix(line, "#") {
			t.Errorf("annotation must be commented so a copied block stays valid LogQL: %q", line)
		}
	}
}

// A failed namespace is the case where seeing the query matters most, so the
// statement still shows up, with the error in place of a count.
func TestFormatNamespacedQueriesReportsErrors(t *testing.T) {
	out := formatNamespacedQueries([]alert.NamespacedQuery{
		{Namespace: "g32-wallet", LogQL: `{namespace="g32-wallet"} |~ "("`, Error: "parse error: unterminated"},
	})

	if !strings.Contains(out, `{namespace="g32-wallet"} |~ "("`) {
		t.Errorf("the attempted query must still be shown:\n%s", out)
	}
	if !strings.Contains(out, "查询失败: parse error: unterminated") {
		t.Errorf("the error must be shown:\n%s", out)
	}
	if strings.Contains(out, "返回 0 行") {
		t.Errorf("a failure must not be reported as an empty result:\n%s", out)
	}
}
