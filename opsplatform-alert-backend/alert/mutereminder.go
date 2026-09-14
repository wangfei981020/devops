package alert

import (
	"fmt"
	"log"
	"time"

	"opsplatform-alert-backend/database"
	"opsplatform-alert-backend/notify"
	"opsplatform-alert-backend/timezone"
)

// A mute is the easiest thing on this platform to set and forget. Somebody
// silences a noisy container for two days during a migration, the migration
// finishes early, and either the alerts come back at 3am with nobody expecting
// them, or — worse — the window ends while the underlying problem is still
// there and the silence looked like a fix.
//
// So each mute gets one heads-up shortly before it lapses, sent to the same
// channels the rule itself alerts on: those are the people watching, and it is
// the thread where the silence was noticed in the first place.

const (
	// muteReminderLead is how far ahead the notice goes out. Long enough to
	// extend the mute before it lapses, short enough that the reminder is still
	// about something imminent.
	muteReminderLead = 10 * time.Minute

	// muteReminderSchedule runs every minute: the lead time is the thing that
	// decides when a notice lands, and checking often keeps the actual send
	// close to the intended lead rather than up to a poll period early.
	muteReminderSchedule = "0 * * * * *"

	muteReminderLockKey = "alert:lock:mute-reminder"
)

type expiringMute struct {
	ID        int
	RuleID    int
	RuleName  string
	ChannelID int
	GroupKey  string
	Reason    string
	CreatedBy string
	MuteUntil time.Time
	SecondsTo int64
}

// SendMuteReminders notifies about mutes about to lapse and marks them so the
// notice goes out once. Safe to call from several replicas.
func SendMuteReminders() (int, error) {
	release, ok := tryLock(muteReminderLockKey, 5*time.Minute)
	if !ok {
		return 0, nil
	}
	defer release()

	due, err := dueMuteReminders()
	if err != nil {
		return 0, err
	}

	sent := 0
	for _, m := range due {
		// Claim it before sending. A notice that goes out and is then not
		// recorded would repeat every minute until the mute lapsed; one that is
		// claimed and then fails to send is missed once, which is the smaller
		// harm for a courtesy message.
		res, err := database.DB.Exec(
			`UPDATE alert_mutes SET reminded_at = NOW() WHERE id = ? AND reminded_at IS NULL`, m.ID)
		if err != nil {
			log.Printf("[MuteReminder] cannot claim mute %d: %v", m.ID, err)
			continue
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue // another replica took it between the read and here
		}

		resp, err := sendMuteReminder(m)
		if err != nil {
			log.Printf("[MuteReminder] mute %d (%s): %v; per-channel: %s", m.ID, m.GroupKey, err, resp)
			continue
		}
		sent++
	}
	return sent, nil
}

// dueMuteReminders finds mutes inside the lead window that have not been
// announced.
//
// Three things are excluded, each because the notice would carry nothing:
//   - one already announced, so the heads-up goes out once
//   - one that has already lapsed, since "about to expire" after the fact is
//     worse than silence
//   - one whose whole life is barely longer than the lead. Somebody silencing a
//     container for five minutes is watching it; telling them a minute later
//     that it is about to come back is noise, and noise on the alert channel is
//     the thing this platform exists to reduce.
func dueMuteReminders() ([]expiringMute, error) {
	lead := int(muteReminderLead.Seconds())
	rows, err := database.DB.Query(`SELECT m.id, m.rule_id, COALESCE(r.name, ''), COALESCE(r.lark_config_id, 0),
			m.group_key, COALESCE(m.reason, ''), COALESCE(m.created_by, ''),
			m.mute_until, TIMESTAMPDIFF(SECOND, NOW(), m.mute_until)
		FROM alert_mutes m
		LEFT JOIN alert_rules r ON r.id = m.rule_id
		WHERE m.reminded_at IS NULL
		  AND m.mute_until > NOW()
		  AND m.mute_until <= NOW() + INTERVAL ? SECOND
		  AND m.mute_until >= m.created_at + INTERVAL ? SECOND
		ORDER BY m.mute_until`, lead, 2*lead)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []expiringMute
	for rows.Next() {
		var m expiringMute
		if err := rows.Scan(&m.ID, &m.RuleID, &m.RuleName, &m.ChannelID, &m.GroupKey,
			&m.Reason, &m.CreatedBy, &m.MuteUntil, &m.SecondsTo); err != nil {
			continue
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// muteReminderBody is what lands in the group. Kept separate from the sending
// so its wording can be read and tested without a channel.
func muteReminderBody(m expiringMute) string {
	ruleName := m.RuleName
	if ruleName == "" {
		ruleName = fmt.Sprintf("规则 #%d", m.RuleID)
	}

	body := fmt.Sprintf("**规则:** %s\n**屏蔽对象:** %s\n**恢复时间:** %s\n**剩余:** 约 %d 分钟",
		ruleName, m.GroupKey, timezone.FormatWithZone(m.MuteUntil), (m.SecondsTo+59)/60)
	if m.Reason != "" {
		body += "\n**屏蔽原因:** " + m.Reason
	}
	if m.CreatedBy != "" {
		body += "\n**操作人:** " + m.CreatedBy
	}
	return body + "\n\n到期后该对象的告警将恢复发送。若问题尚未解决，请到「屏蔽管理」延长。"
}

// sendMuteReminder returns the per-channel result alongside the error: with
// several channels behind one send, "it failed" without saying which one is not
// enough to act on.
func sendMuteReminder(m expiringMute) (string, error) {
	channels, err := getChannelsForRule(m.RuleID, m.ChannelID)
	if err != nil {
		return "", fmt.Errorf("channels: %w", err)
	}
	if len(channels) == 0 {
		return "", fmt.Errorf("rule %d has no enabled channel", m.RuleID)
	}

	sender, err := notify.NewMulti(channels)
	if err != nil {
		return "", fmt.Errorf("sender: %w", err)
	}

	// A heads-up, not an incident: the lowest severity the card renderer has,
	// so it does not carry an alarm's colour or prefix into the group.
	return sender.SendCard("屏蔽即将到期", muteReminderBody(m), "info", nil, false)
}

// startMuteReminderJob registers the reminder sweep.
func (e *Engine) startMuteReminderJob() error {
	_, err := e.cron.AddFunc(muteReminderSchedule, func() {
		n, err := SendMuteReminders()
		if err != nil {
			log.Printf("[MuteReminder] sweep failed: %v", err)
			return
		}
		if n > 0 {
			log.Printf("[MuteReminder] sent %d reminder(s)", n)
		}
	})
	if err != nil {
		return err
	}
	log.Printf("[MuteReminder] armed: notify %s before a mute lapses", muteReminderLead)
	return nil
}
