package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"opsplatform-alert-backend/alert"
	"opsplatform-alert-backend/database"
	"opsplatform-alert-backend/timezone"
)

// displayZone labels a zone for a log line or an audit entry. The previous
// value is a bare string rather than the package's current state, so it needs
// the same treatment DisplayName gives the new one.
func displayZone(name string) string {
	if name == "" {
		return "未配置（跟随本机）"
	}
	return name
}

// currentUsername names whoever is making the change, for the audit trail.
func currentUsername(r *http.Request) string {
	if u := r.Context().Value(contextUsername); u != nil {
		if s, ok := u.(string); ok {
			return s
		}
	}
	return ""
}

// scheduleRebuilder is the alert engine, kept behind an interface so this file
// does not depend on the whole engine. Changing the display timezone changes
// what a rule's cron expression means, so the scheduler has to be rebuilt.
var scheduleRebuilder interface {
	RebuildSchedule() error
}

func SetScheduleRebuilder(e interface {
	RebuildSchedule() error
}) {
	scheduleRebuilder = e
}

// HandleGetSettings returns the platform settings and enough context for the
// UI to explain what the timezone affects.
func HandleGetSettings(w http.ResponseWriter, r *http.Request) {
	now := time.Now()

	var logRows int
	database.DB.QueryRow(`SELECT COUNT(*) FROM alert_logs`).Scan(&logRows)

	jsonSuccess(w, map[string]interface{}{
		"display_timezone": timezone.Name(),
		"available_zones":  timezone.Available(),
		// A live sample removes the guesswork: the operator can see what the
		// choice actually produces before saving.
		"server_time_utc": now.UTC(),
		"sample":          timezone.FormatWithZone(now),

		"log_retention_days": alert.RetentionDays(),
		// The current row count makes the setting concrete — "keep 30 days" means
		// something different against 800 rows than against 8 million.
		"log_rows": logRows,
	})
}

// HandleUpdateSettings changes the display timezone.
//
// Nothing stored is rewritten. Instants keep their stored form and every
// timestamp in the UI and in future alert messages simply renders in the new
// zone — including history, which is the point of never baking a zone into
// storage. The one thing that is not merely cosmetic is scheduling: a rule's
// cron expression is interpreted in this zone, so the scheduler is rebuilt
// before the call returns.
func HandleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DisplayTimezone  string `json:"display_timezone"`
		LogRetentionDays *int   `json:"log_retention_days"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}

	// Retention is a pointer so that omitting it leaves the stored value alone;
	// a plain int would read as 0 and silently turn pruning off.
	if req.LogRetentionDays != nil {
		days := *req.LogRetentionDays
		if days < 0 || days > 3650 {
			jsonError(w, http.StatusBadRequest, "保留天数需在 0 到 3650 之间（0 表示不清理）")
			return
		}
		previousDays := alert.RetentionDays()
		if days != previousDays {
			if err := database.PutSetting(database.SettingLogRetentionDays,
				strconv.Itoa(days), currentUsername(r)); err != nil {
				jsonError(w, http.StatusInternalServerError, "保存保留天数失败")
				return
			}
			SaveAuditLog(r, "update_settings", "settings", "log_retention_days",
				fmt.Sprintf("告警日志保留 %d 天 → %d 天", previousDays, days))
			log.Printf("[Settings] 日志保留 %d → %d 天 (by %s)", previousDays, days, currentUsername(r))
		}
	}

	previous := timezone.Name()

	// Validate before persisting, so a typo cannot leave a value in the table
	// that fails to load on the next boot.
	if err := timezone.Set(req.DisplayTimezone); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	username := currentUsername(r)

	if err := database.PutSetting(database.SettingDisplayTimezone, timezone.Name(), username); err != nil {
		// Put the running process back where it was; a setting that did not
		// persist must not silently apply until the next restart undoes it.
		timezone.Set(previous)
		jsonError(w, http.StatusInternalServerError, "保存设置失败")
		return
	}

	if scheduleRebuilder != nil {
		if err := scheduleRebuilder.RebuildSchedule(); err != nil {
			log.Printf("[Settings] 时区改为 %s 后重建调度失败: %v", timezone.Name(), err)
		}
	}

	SaveAuditLog(r, "update_settings", "settings", "display_timezone",
		"显示时区 "+displayZone(previous)+" → "+timezone.DisplayName())
	log.Printf("[Settings] 显示时区 %s → %s (by %s)", displayZone(previous), timezone.DisplayName(), username)

	now := time.Now()
	jsonSuccess(w, map[string]interface{}{
		"display_timezone":   timezone.Name(),
		"server_time_utc":    now.UTC(),
		"sample":             timezone.FormatWithZone(now),
		"log_retention_days": alert.RetentionDays(),
	})
}
