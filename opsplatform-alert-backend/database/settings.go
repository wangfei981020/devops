package database

import (
	"errors"
	"strconv"
)

// Platform settings live in one small key/value table. They are read at boot
// and whenever an operator changes one, never per request.

// SettingDisplayTimezone is the IANA name of the zone every rendered timestamp
// is shown in, and the zone a rule's cron expression is interpreted in.
const SettingDisplayTimezone = "display_timezone"

// SettingLogRetentionDays is how many days of alert history to keep. Zero, the
// default, keeps everything: deleting an installation's own audit trail is the
// operator's decision, not a default.
const SettingLogRetentionDays = "log_retention_days"

// GetSetting returns the stored value, or fallback when the key has never been
// set. A missing row is the normal state on a fresh install, not an error.
func GetSetting(key, fallback string) string {
	// Settings are read from scheduled jobs, and cron runs those as bare
	// goroutines — a nil-pointer dereference here would take the process down
	// rather than fail one read. The default is the honest answer when there is
	// nothing to read from.
	if DB == nil {
		return fallback
	}
	var value string
	if err := DB.QueryRow(
		`SELECT setting_value FROM system_settings WHERE setting_key = ?`, key).Scan(&value); err != nil {
		return fallback
	}
	if value == "" {
		return fallback
	}
	return value
}

// GetSettingInt returns a stored value parsed as a non-negative integer, or
// fallback when the key is unset or holds anything else. A malformed value
// falls back rather than erroring: the settings these back are read on hot
// paths, and the caller has no better answer than the default.
func GetSettingInt(key string, fallback int) int {
	raw := GetSetting(key, "")
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return fallback
	}
	return n
}

// PutSetting writes a value, recording who changed it.
func PutSetting(key, value, updatedBy string) error {
	if DB == nil {
		return errors.New("数据库未初始化")
	}
	_, err := DB.Exec(`INSERT INTO system_settings (setting_key, setting_value, updated_by)
		VALUES (?, ?, ?)
		ON DUPLICATE KEY UPDATE setting_value = VALUES(setting_value), updated_by = VALUES(updated_by)`,
		key, value, updatedBy)
	return err
}
