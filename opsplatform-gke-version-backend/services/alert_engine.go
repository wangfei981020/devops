package services

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"opsplatform-gke-version-backend/models"
)

type AlertEngine struct {
	db        *sql.DB
	detailURL string // 详情页基址，如 http://localhost:30827
}

func NewAlertEngine(db *sql.DB, detailURL string) *AlertEngine {
	return &AlertEngine{db: db, detailURL: detailURL}
}

// Evaluate: scrape 完成后调，遍历所有 enabled rule 检查触发
func (e *AlertEngine) Evaluate() {
	rules, err := e.loadEnabledRules()
	if err != nil {
		log.Printf("alert: load rules: %v", err)
		return
	}
	for _, r := range rules {
		if err := e.evalRule(r); err != nil {
			log.Printf("alert: rule %d (%s): %v", r.ID, r.Name, err)
		}
	}
}

func (e *AlertEngine) evalRule(r models.AlertRule) error {
	clusters, err := e.loadCandidateClusters(r.ClusterIDs)
	if err != nil {
		return err
	}
	webhook, mentionNames, mentionIDs, err := e.loadRuleContext(r)
	if err != nil {
		return err
	}
	for _, cl := range clusters {
		snap, err := e.loadSnapshot(cl.ID)
		if err != nil || snap == nil {
			continue
		}
		switch r.Target {
		case "cluster":
			if snap.CurrentToLatestVersionsBehind < r.VersionsBehindThreshold {
				continue
			}
			if e.dedupHit(r.ID, cl.ID, "", r.IntervalMinutes) {
				continue
			}
			e.sendAndRecord(r, cl, snap, "", snap.CurrentVersion, snap.LatestAvailableVersion,
				snap.CurrentToLatestVersionsBehind, snap.StdSupportEnd,
				webhook.URL, mentionIDs, mentionNames)
		case "nodepool":
			for _, np := range snap.NodePools {
				if np.CurrentToLatestVersionsBehind < r.VersionsBehindThreshold {
					continue
				}
				if e.dedupHit(r.ID, cl.ID, np.Name, r.IntervalMinutes) {
					continue
				}
				e.sendAndRecord(r, cl, snap, np.Name, np.CurrentVersion, np.LatestAvailableVersion,
					np.CurrentToLatestVersionsBehind, np.StdSupportEnd,
					webhook.URL, mentionIDs, mentionNames)
			}
		}
	}
	return nil
}

func (e *AlertEngine) loadEnabledRules() ([]models.AlertRule, error) {
	rows, err := e.db.Query(`SELECT id, name, target, versions_behind_threshold, eol_days_threshold,
		COALESCE(cluster_ids, JSON_ARRAY()), webhook_id, COALESCE(mention_user_ids, JSON_ARRAY()),
		interval_minutes, enabled FROM alert_rules WHERE enabled=1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.AlertRule{}
	for rows.Next() {
		var (
			r          models.AlertRule
			eolDays    sql.NullInt64
			clusterIDs []byte
			mention    []byte
		)
		if err := rows.Scan(&r.ID, &r.Name, &r.Target, &r.VersionsBehindThreshold, &eolDays,
			&clusterIDs, &r.WebhookID, &mention, &r.IntervalMinutes, &r.Enabled); err != nil {
			return nil, err
		}
		if eolDays.Valid {
			v := int(eolDays.Int64)
			r.EOLDaysThreshold = &v
		}
		_ = json.Unmarshal(clusterIDs, &r.ClusterIDs)
		_ = json.Unmarshal(mention, &r.MentionUserIDs)
		out = append(out, r)
	}
	return out, nil
}

func (e *AlertEngine) loadCandidateClusters(ids []int) ([]*models.Cluster, error) {
	q := `SELECT id, project_id, location, name, enabled FROM clusters WHERE enabled=1`
	args := []any{}
	if len(ids) > 0 {
		q += " AND id IN ("
		for i, id := range ids {
			if i > 0 {
				q += ","
			}
			q += "?"
			args = append(args, id)
		}
		q += ")"
	}
	rows, err := e.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*models.Cluster{}
	for rows.Next() {
		c := &models.Cluster{}
		if err := rows.Scan(&c.ID, &c.ProjectID, &c.Location, &c.Name, &c.Enabled); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func (e *AlertEngine) loadRuleContext(r models.AlertRule) (models.LarkWebhook, []string, []string, error) {
	var w models.LarkWebhook
	err := e.db.QueryRow(`SELECT id, name, url FROM lark_webhooks WHERE id=?`, r.WebhookID).Scan(&w.ID, &w.Name, &w.URL)
	if err != nil {
		return w, nil, nil, fmt.Errorf("webhook %d: %w", r.WebhookID, err)
	}
	names := []string{}
	ids := []string{}
	for _, uid := range r.MentionUserIDs {
		var u models.NotifyUser
		if err := e.db.QueryRow(`SELECT name, lark_id FROM notify_users WHERE id=?`, uid).Scan(&u.Name, &u.LarkID); err == nil {
			names = append(names, u.Name)
			ids = append(ids, u.LarkID)
		}
	}
	return w, names, ids, nil
}

func (e *AlertEngine) loadSnapshot(clusterID int) (*models.ClusterSnapshot, error) {
	var (
		snap   models.ClusterSnapshot
		std, ext sql.NullString
		npJSON sql.NullString
	)
	err := e.db.QueryRow(`SELECT current_version, max_upgradable_version, latest_available_version,
		current_to_max_versions_behind, max_to_latest_versions_behind, current_to_latest_versions_behind,
		std_support_end, ext_support_end, nodepools_json
		FROM cluster_snapshots WHERE cluster_id=?`, clusterID).Scan(
		&snap.CurrentVersion, &snap.MaxUpgradableVersion, &snap.LatestAvailableVersion,
		&snap.CurrentToMaxVersionsBehind, &snap.MaxToLatestVersionsBehind, &snap.CurrentToLatestVersionsBehind,
		&std, &ext, &npJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	snap.ClusterID = clusterID
	snap.StdSupportEnd = std.String
	snap.ExtSupportEnd = ext.String
	if npJSON.Valid && npJSON.String != "" {
		_ = json.Unmarshal([]byte(npJSON.String), &snap.NodePools)
	}
	return &snap, nil
}

func (e *AlertEngine) dedupHit(ruleID, clusterID int, nodepool string, intervalMin int) bool {
	var lastTime time.Time
	q := `SELECT MAX(trigger_time) FROM alert_history WHERE rule_id=? AND cluster_id=? AND `
	args := []any{ruleID, clusterID}
	if nodepool != "" {
		q += "nodepool_name=?"
		args = append(args, nodepool)
	} else {
		q += "(nodepool_name IS NULL OR nodepool_name='')"
	}
	var t sql.NullTime
	if err := e.db.QueryRow(q, args...).Scan(&t); err != nil || !t.Valid {
		return false
	}
	lastTime = t.Time
	return time.Since(lastTime) < time.Duration(intervalMin)*time.Minute
}

func (e *AlertEngine) sendAndRecord(r models.AlertRule, cl *models.Cluster, snap *models.ClusterSnapshot,
	nodepoolName, curVer, latestVer string, behind int, stdEOL string,
	webhookURL string, mentionIDs, mentionNames []string) {

	var daysToEOL *int
	if stdEOL != "" {
		if t, err := time.Parse("2006-01-02", stdEOL); err == nil {
			d := int(t.Sub(time.Now()).Hours() / 24)
			daysToEOL = &d
		}
	}

	target := "cluster"
	title := fmt.Sprintf("GKE 集群版本落后 - %s", r.Name)
	if r.Target == "nodepool" {
		target = "nodepool"
		title = fmt.Sprintf("GKE 节点池版本落后 - %s", r.Name)
	}

	payload := LarkAlertPayload{
		Title:          title,
		Project:        cl.ProjectID,
		ClusterName:    cl.Name,
		Location:       cl.Location,
		Target:         target,
		NodepoolName:   nodepoolName,
		CurrentVersion: curVer,
		LatestVersion:  latestVer,
		VersionsBehind: behind,
		Threshold:      r.VersionsBehindThreshold,
		StdEOL:         stdEOL,
		DaysToEOL:      daysToEOL,
		MentionLarkIDs: mentionIDs,
		MentionNames:   mentionNames,
		DetailURL:      fmt.Sprintf("%s/clusters/%d", e.detailURL, cl.ID),
	}
	card := BuildLarkCard(payload)
	resp, err := SendLarkCard(webhookURL, card)
	status := "sent"
	if err != nil {
		status = "failed"
		log.Printf("alert send failed: %v, resp=%s", err, resp)
	} else {
		log.Printf("alert sent: rule=%s cluster=%s nodepool=%s behind=%d", r.Name, cl.Name, nodepoolName, behind)
	}
	_, _ = e.db.Exec(`INSERT INTO alert_history (rule_id, cluster_id, nodepool_name, versions_behind, status, lark_response) VALUES (?, ?, NULLIF(?, ''), ?, ?, ?)`,
		r.ID, cl.ID, nodepoolName, behind, status, resp)
}
