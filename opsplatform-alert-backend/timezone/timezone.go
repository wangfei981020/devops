// Package timezone holds the platform's single display timezone.
//
// Three kinds of time exist in this system and only the middle one belongs
// here:
//
//   - The instant something happened. Stored in MySQL TIMESTAMP columns, which
//     keep UTC internally, and carried over the API as RFC3339. It has no
//     timezone of its own and is never rewritten.
//   - How that instant is shown to a person — this package. It is a rendering
//     choice, so changing it re-renders history rather than rewriting it.
//   - A timestamp inside log text, written by some other system. Its zone is
//     unknown to us and it is reproduced verbatim, never converted.
//
// The location set here also decides what a rule's cron expression means: "0 3
// * * *" is 03:00 in this zone, not in whatever zone the container happens to
// run in.
package timezone

import (
	"fmt"
	"sync/atomic"
	"time"
)

// DisplayLayout is the wall-clock form shown to people. The zone is appended
// separately by FormatWithZone so a reader in another country can convert.
const DisplayLayout = "2006-01-02 15:04:05"

// current holds a *time.Location, configured the IANA name that produced it.
// Reads happen on every alert send and every API response; writes happen only
// when an operator changes the setting.
var (
	current    atomic.Value
	configured atomic.Value
)

func init() {
	current.Store(time.Local)
	configured.Store("")
}

// Set validates and installs a location by IANA name ("Asia/Shanghai",
// "Europe/Berlin").
//
// An empty name means "not configured", and resolves to the process's own zone
// — not UTC. That distinction is what makes an upgrade safe: before this
// setting existed, the scheduler ran in the process zone and the UI rendered in
// the viewer's browser zone. Defaulting to UTC would have moved every displayed
// timestamp on an installation that never asked for a timezone, and shifted
// when scheduled rules fire. Unset therefore preserves the old behaviour
// exactly, and the setting is opt-in.
func Set(name string) error {
	if name == "" {
		current.Store(time.Local)
		configured.Store("")
		return nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return fmt.Errorf("未知的时区 %q: %w", name, err)
	}
	current.Store(loc)
	configured.Store(name)
	return nil
}

// Location returns the display location, never nil.
func Location() *time.Location {
	if loc, ok := current.Load().(*time.Location); ok && loc != nil {
		return loc
	}
	return time.Local
}

// Name is the configured IANA name, e.g. "Asia/Shanghai". It is empty when no
// timezone has been configured, which callers read as "follow local".
func Name() string {
	if s, ok := configured.Load().(string); ok {
		return s
	}
	return ""
}

// DisplayName is Name for a log line or an audit entry, where an empty string
// would read as a missing value rather than a deliberate one — "显示时区 ，当前
// 时间 …" looks like a bug to whoever is reading the boot log.
func DisplayName() string {
	if name := Name(); name != "" {
		return name
	}
	return "未配置（跟随本机）"
}

// Format renders an instant as a bare wall clock in the configured zone. Use
// it where the surrounding text already establishes the zone.
func Format(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.In(Location()).Format(DisplayLayout)
}

// FormatWithZone renders an instant with its offset and zone name, as
// "2026-09-08 04:31:42 (+08:00 Asia/Shanghai)". This is the form used in alert
// messages, which are read in chat by people who may not share the zone.
func FormatWithZone(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	local := t.In(Location())
	// With no zone configured the offset is the whole truth we have; naming the
	// Go location ("Local") would tell the reader nothing.
	if name := Name(); name != "" {
		return fmt.Sprintf("%s (%s %s)", local.Format(DisplayLayout), local.Format("-07:00"), name)
	}
	return fmt.Sprintf("%s (%s)", local.Format(DisplayLayout), local.Format("-07:00"))
}

// Now is the current instant in the configured zone, for callers that go on to
// format it themselves.
func Now() time.Time {
	return time.Now().In(Location())
}

// Available lists the zones offered in the settings UI. A free-text IANA name
// is still accepted by Set; this is a shortlist, not a whitelist.
func Available() []string {
	return []string{
		"UTC",
		"Asia/Shanghai",
		"Asia/Singapore",
		"Asia/Tokyo",
		"Asia/Dubai",
		"Europe/London",
		"Europe/Berlin",
		"Europe/Paris",
		"Europe/Moscow",
		"America/New_York",
		"America/Chicago",
		"America/Los_Angeles",
		"Australia/Sydney",
	}
}
