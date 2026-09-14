package lark

import (
	"encoding/json"
	"strings"
	"testing"

	"opsplatform-alert-backend/models"
)

// buildFor mirrors what SendCard hands the truncator.
func buildFor(t *testing.T) func(string) map[string]interface{} {
	t.Helper()
	s := &Sender{}
	return func(body string) map[string]interface{} {
		return s.buildCard("G32 UAT 错误码告警", body, "S1", nil, false)
	}
}

func marshalLen(t *testing.T, card map[string]interface{}) int {
	t.Helper()
	b, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return len(b)
}

func TestSendCardLeavesAFittingContentAlone(t *testing.T) {
	build := buildFor(t)
	content := "**错误码:** 1351\n原始日志:\n```\nboom\n```"

	if got := marshalLen(t, build(content)); got > maxCardBytes {
		t.Fatalf("fixture is already over the cap at %d bytes", got)
	}
	// The sender only truncates when over the cap, so the content must survive
	// byte-for-byte in the ordinary case.
	if c := build(content)["card"].(map[string]interface{}); c == nil {
		t.Fatal("no card built")
	}
	if strings.Contains(contentOf(t, build(content)), cardTruncationMarker) {
		t.Error("a fitting card must not be marked truncated")
	}
}

func TestTruncateCardContentFitsTheCap(t *testing.T) {
	build := buildFor(t)
	// Chinese runes are 3 bytes in UTF-8, so a rune-count estimate would come
	// out three times short — the case an escaping guess gets wrong.
	huge := strings.Repeat("订单结算失败，账本拒绝提交。", 4000)

	got := truncateCardContent(build, huge)

	if n := marshalLen(t, build(got)); n > maxCardBytes {
		t.Errorf("truncated card is %d bytes, cap is %d", n, maxCardBytes)
	}
	if !strings.HasSuffix(got, cardTruncationMarker) {
		t.Errorf("truncated content must end with the marker, got %q", got[len(got)-40:])
	}
	if !strings.HasPrefix(got, "订单结算失败") {
		t.Error("truncation should keep the head of the content")
	}
}

// Cutting mid-fence would leave the ``` unterminated, and Lark renders
// everything after an unclosed fence as code — swallowing the marker itself.
func TestTruncateCardContentClosesAnOpenFence(t *testing.T) {
	build := buildFor(t)
	content := "**错误码:** 7702\n原始日志:\n```\n" + strings.Repeat("\tat com.sl.order.Service.settle(Service.java:344)\n", 2000)

	got := truncateCardContent(build, content)

	if strings.Count(got, "```")%2 != 0 {
		t.Errorf("truncated content left an unbalanced fence: %q", got[len(got)-80:])
	}
	if !strings.HasSuffix(got, cardTruncationMarker) {
		t.Errorf("marker must sit outside the closed fence, got %q", got[len(got)-80:])
	}
	if i := strings.LastIndex(got, "```"); i > strings.Index(got, cardTruncationMarker) {
		t.Error("the closing fence must come before the marker, not after it")
	}
	if n := marshalLen(t, build(got)); n > maxCardBytes {
		t.Errorf("truncated card is %d bytes, cap is %d", n, maxCardBytes)
	}
}

// The cap is on the whole JSON body, so a long title and a wall of mentions
// have to leave less room for the log, not silently push the card over.
func TestTruncateCardContentAccountsForTitleAndMentions(t *testing.T) {
	s := &Sender{}
	users := make([]models.AtUser, 0, 60)
	for i := 0; i < 60; i++ {
		users = append(users, models.AtUser{UserID: "ou_0123456789abcdef0123456789abcdef", Name: "值班工程师"})
	}
	build := func(body string) map[string]interface{} {
		return s.buildCard(strings.Repeat("很长的告警标题", 40), body, "S1", users, false)
	}

	got := truncateCardContent(build, strings.Repeat("订单结算失败。", 4000))

	if n := marshalLen(t, build(got)); n > maxCardBytes {
		t.Errorf("card with title and mentions is %d bytes, cap is %d", n, maxCardBytes)
	}
}

func TestFinishTruncatedLeavesABalancedFenceAlone(t *testing.T) {
	got := finishTruncated("原始日志:\n```\nboom\n```")

	if strings.Count(got, "```") != 2 {
		t.Errorf("finishTruncated() added a fence to balanced content: %q", got)
	}
}

// contentOf digs the markdown element's content back out of a built card.
func contentOf(t *testing.T, card map[string]interface{}) string {
	t.Helper()
	elements := card["card"].(map[string]interface{})["elements"].([]interface{})
	return elements[0].(map[string]interface{})["content"].(string)
}
