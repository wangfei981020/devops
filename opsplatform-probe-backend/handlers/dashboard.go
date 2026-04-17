package handlers

import (
	"net/http"

	"opsplatform-probe-backend/database"
)

func HandleDashboard(w http.ResponseWriter, r *http.Request) {
	stats := map[string]interface{}{}

	var total, online, offline, pending int
	database.DB.QueryRow("SELECT COUNT(*) FROM agents").Scan(&total)
	database.DB.QueryRow("SELECT COUNT(*) FROM agents WHERE status='online' AND approved=1").Scan(&online)
	database.DB.QueryRow("SELECT COUNT(*) FROM agents WHERE status='offline' AND approved=1").Scan(&offline)
	database.DB.QueryRow("SELECT COUNT(*) FROM agents WHERE approved=0").Scan(&pending)
	stats["agents_total"] = total
	stats["agents_online"] = online
	stats["agents_offline"] = offline
	stats["agents_pending"] = pending

	var targets int
	database.DB.QueryRow("SELECT COUNT(*) FROM probe_targets WHERE enabled=1").Scan(&targets)
	stats["targets"] = targets

	var todayTotal, todayFail int
	database.DB.QueryRow("SELECT COUNT(*) FROM probe_results WHERE DATE(probed_at) = CURDATE()").Scan(&todayTotal)
	database.DB.QueryRow("SELECT COUNT(*) FROM probe_results WHERE DATE(probed_at) = CURDATE() AND success=0").Scan(&todayFail)
	stats["today_total"] = todayTotal
	stats["today_failed"] = todayFail
	if todayTotal > 0 {
		stats["today_success_rate"] = float64(todayTotal-todayFail) / float64(todayTotal)
	} else {
		stats["today_success_rate"] = 1.0
	}

	// Recent failures
	rows, _ := database.DB.Query(
		`SELECT agent_id, target_name, target_addr, error, probed_at FROM probe_results
		   WHERE success=0 ORDER BY id DESC LIMIT 10`,
	)
	recent := []map[string]interface{}{}
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var aid, tname, addr, errMsg, ts string
			rows.Scan(&aid, &tname, &addr, &errMsg, &ts)
			recent = append(recent, map[string]interface{}{
				"agent_id": aid, "target_name": tname, "target_addr": addr,
				"error": errMsg, "probed_at": ts,
			})
		}
	}
	stats["recent_failures"] = recent

	jsonSuccess(w, stats)
}
