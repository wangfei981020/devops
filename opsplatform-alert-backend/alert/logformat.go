package alert

import (
	"fmt"
	"sort"
	"strings"

	"opsplatform-alert-backend/notify"
)

// maxInlineLogRunes bounds a single log line embedded in an alert body. The
// limit is in runes, not bytes: log lines are routinely UTF-8, and slicing a
// byte string mid-rune produces invalid UTF-8 that renders as a replacement
// character on both Lark and Telegram.
const maxInlineLogRunes = 500

// logValueKeys are the variables whose values are raw log text rather than a
// short field. They get a code fence of their own instead of being printed
// inline after a bold label: they are frequently multi-line, and their content
// (asterisks, backticks, brackets, angle brackets) is markdown-active, so
// rendering them as prose both mangles the log and can leave unbalanced markup
// behind that corrupts the rest of the card.
var logValueKeys = map[string]bool{
	"message": true,
	"line":    true,
	"stack":   true,
}

// RenderVarsDefault renders the whole variable set as an alert body, for rules
// that have no message template of their own.
//
// Keys are emitted in a stable order — sorted, with the log-text keys last —
// because a map's iteration order is randomised per call: without sorting, the
// same rule produces the fields in a different order on every alert, which
// makes two alerts impossible to compare at a glance.
func RenderVarsDefault(vars map[string]interface{}) string {
	fields := make([]string, 0, len(vars))
	logs := make([]string, 0, 2)
	for k := range vars {
		if k == "_id" || k == "_index" {
			continue
		}
		if logValueKeys[k] {
			logs = append(logs, k)
			continue
		}
		fields = append(fields, k)
	}
	sort.Strings(fields)
	sort.Strings(logs)

	var sb strings.Builder
	for _, k := range fields {
		sb.WriteString(fmt.Sprintf("**%s:** %v\n", k, vars[k]))
	}
	for _, k := range logs {
		s := truncateLogRunes(fmt.Sprintf("%v", vars[k]), maxInlineLogRunes)
		if strings.TrimSpace(s) == "" {
			continue
		}
		sb.WriteString(fmt.Sprintf("**%s:**\n", k))
		sb.WriteString(notify.FencedBlock(s))
		sb.WriteString("\n")
	}
	return sb.String()
}

// truncateLogRunes cuts s to max runes, appending an ellipsis when it had to
// cut. Counting runes keeps multi-byte characters whole.
func truncateLogRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "..."
}
