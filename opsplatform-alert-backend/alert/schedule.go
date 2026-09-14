package alert

import (
	"strings"

	"github.com/robfig/cron/v3"
)

// The scheduler runs with seconds enabled, so a rule's stored expression — which
// users write in the usual five fields — gets a seconds field prepended before
// it is handed to cron. Anything that needs to reason about when a rule should
// have run has to apply the same rule, so it lives here rather than being
// spelled out at each call site.

// scheduleParser matches cron.New(cron.WithSeconds()).
var scheduleParser = cron.NewParser(
	cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
)

// NormalizeSchedule converts a five-field expression to the six-field form the
// scheduler expects. Anything that is not five fields — already six, or a
// descriptor like @hourly — is passed through untouched.
func NormalizeSchedule(schedule string) string {
	if len(strings.Fields(schedule)) == 5 {
		return "0 " + schedule
	}
	return schedule
}

// ParseSchedule normalizes and parses an expression the way the engine does,
// so callers can compute when a rule was next due.
func ParseSchedule(schedule string) (cron.Schedule, error) {
	return scheduleParser.Parse(NormalizeSchedule(schedule))
}
