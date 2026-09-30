package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"opsplatform/database"
)

// 站点 × 桌台 对应关系。
//
// 为什么这层关系必须在这里维护：中台的桌台接口给不了它。桌台对象上唯一沾到站点的
// 字段是 gameRoomMaintainList，而它只在这张桌台处于维护时才非空；siteStatus 实测
// 恒为 null。所以「哪个站点在用哪张桌台」没有任何上游数据源，只能由这边落库。
//
// 关系决定心跳告警的监控范围。这和之前「从日志里出现过什么来推断监控谁」有本质
// 区别：后者永远发现不了一张整周没有日志的在用桌台，而那恰恰是最该告警的情况。

type taRoomSiteRow struct {
	ID        string `json:"id"`
	RoomID    string `json:"room_id"`
	RoomNo    string `json:"room_no"`
	TableNo   string `json:"table_no"`
	InService bool   `json:"in_service"`
	SiteID    string `json:"site_id"`
	SiteName  string `json:"site_name"`
	Watched   bool   `json:"watched"`
	Source    string `json:"source"`
	Enabled   bool   `json:"enabled"`
	Hits      int64  `json:"hits"`
	ScanRange string `json:"scan_range"`
	Remark    string `json:"remark"`
	CreatedBy string `json:"created_by"`
	LastSeen  string `json:"last_seen_at"`
}

// HandleTAListRoomSiteMap GET /api/table-alert/room-site-map?env_id=&watched_only=
//
// 左连桌台和站点表而不是内连：一条关系如果指向已经不存在的桌台或站点，内连会让它
// 凭空消失，而那正是需要有人去删掉的那一行。
func HandleTAListRoomSiteMap(w http.ResponseWriter, r *http.Request) {
	if !taRequirePerm(w, r, taPermRead) {
		return
	}
	envID := r.URL.Query().Get("env_id")
	if envID == "" {
		respondError(w, http.StatusBadRequest, "缺少 env_id")
		return
	}

	where := "rs.env_id=?"
	args := []interface{}{envID}
	if r.URL.Query().Get("watched_only") == "1" {
		where += " AND COALESCE(s.watched,0)=1"
	}
	if r.URL.Query().Get("in_service_only") == "1" {
		where += " AND COALESCE(rm.in_service,0)=1"
	}

	rows, err := database.DB.Query(`
		SELECT rs.id, rs.room_id, COALESCE(rm.room_no,''), COALESCE(rm.table_no,''),
		       COALESCE(rm.in_service,0), rs.site_id, COALESCE(s.site_name,''),
		       COALESCE(s.watched,0), rs.source, rs.enabled, rs.hits, rs.scan_range,
		       rs.remark, rs.created_by, rs.last_seen_at
		FROM table_alert_room_sites rs
		LEFT JOIN table_alert_rooms rm ON rm.env_id=rs.env_id AND rm.room_id=rs.room_id
		LEFT JOIN table_alert_sites s  ON s.env_id=rs.env_id  AND s.site_id=rs.site_id
		WHERE `+where+`
		ORDER BY COALESCE(s.watched,0) DESC, s.site_name, rm.room_no, rs.room_id`, args...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "查询失败: "+err.Error())
		return
	}
	defer rows.Close()

	list := []taRoomSiteRow{}
	for rows.Next() {
		var it taRoomSiteRow
		var seen *time.Time
		if rows.Scan(&it.ID, &it.RoomID, &it.RoomNo, &it.TableNo, &it.InService,
			&it.SiteID, &it.SiteName, &it.Watched, &it.Source, &it.Enabled,
			&it.Hits, &it.ScanRange, &it.Remark, &it.CreatedBy, &seen) != nil {
			continue
		}
		if seen != nil {
			it.LastSeen = seen.Format("2006-01-02 15:04:05")
		}
		list = append(list, it)
	}

	// 监控范围的实际大小单独算一遍给前端看：关注站点 × 在用桌台 × 关系启用。
	// 只列出关系条数会高估范围——里面混着没关注的站点和已下线的桌台。
	var inScope int
	database.DB.QueryRow(`
		SELECT COUNT(*) FROM table_alert_room_sites rs
		JOIN table_alert_rooms rm ON rm.env_id=rs.env_id AND rm.room_id=rs.room_id
		JOIN table_alert_sites s  ON s.env_id=rs.env_id  AND s.site_id=rs.site_id
		WHERE rs.env_id=? AND rs.enabled=1 AND rm.in_service=1 AND s.watched=1`, envID).Scan(&inScope)

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"list": list, "total": len(list), "in_scope": inScope,
	})
}

type taRoomSiteImportReq struct {
	EnvID     string `json:"env_id"`
	ScanRange string `json:"scan_range"`
	Pairs     []struct {
		RoomID string `json:"room_id"`
		SiteID string `json:"site_id"`
		Hits   int64  `json:"hits"`
	} `json:"pairs"`
}

// HandleTAImportRoomSiteMap POST /api/table-alert/room-site-map/import
//
// 接收日志告警平台扫出来的候选组合。日志里出现过就证明这个组合真实存在，所以导入
// 即生效（enabled=1），不需要人再逐条确认——否则首次使用要手工勾几十行。人工要做的
// 只剩两件：补日志里没出现过的，和排除已下线的。
//
// 导入只增不删。扫描窗口里没出现的组合可能是「这周确实没人玩」，不是「关系不存在」，
// 按缺席去删关系会把最该告警的那批桌台从监控范围里抹掉。
func HandleTAImportRoomSiteMap(w http.ResponseWriter, r *http.Request) {
	if !taRequirePerm(w, r, taPermSiteManage) {
		return
	}
	var req taRoomSiteImportReq
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		respondError(w, http.StatusBadRequest, "无效的请求")
		return
	}
	if req.EnvID == "" || len(req.Pairs) == 0 {
		respondError(w, http.StatusBadRequest, "env_id 和 pairs 不能为空")
		return
	}

	now := time.Now()
	op := taOperator(r)
	added, updated, skipped := 0, 0, 0
	for _, p := range req.Pairs {
		roomID, siteID := strings.TrimSpace(p.RoomID), strings.TrimSpace(p.SiteID)
		if roomID == "" || siteID == "" {
			skipped++
			continue
		}
		// 人工行只更新命中数，不动 enabled 和 source：人明确排除过的组合，
		// 不能因为日志里又出现了就自己恢复。
		res, err := database.DB.Exec(`
			INSERT INTO table_alert_room_sites
			  (id, env_id, room_id, site_id, source, enabled, hits, scan_range, created_by, first_seen_at, last_seen_at)
			VALUES (?,?,?,?, 'auto', 1, ?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE hits=VALUES(hits), scan_range=VALUES(scan_range), last_seen_at=VALUES(last_seen_at)`,
			uuid.NewString(), req.EnvID, roomID, siteID, p.Hits, req.ScanRange, op, now, now)
		if err != nil {
			skipped++
			continue
		}
		// MySQL 的 ON DUPLICATE KEY：插入算 1 行，更新算 2 行
		if n, _ := res.RowsAffected(); n == 1 {
			added++
		} else {
			updated++
		}
	}
	taRefreshDictAfterMapChange(req.EnvID)
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"added": added, "updated": updated, "skipped": skipped,
	})
}

// taRefreshDictAfterMapChange 关系变动后立刻重算字典指纹。
//
// 不重算的话，对面要等到下一次采集才会发现名单变了——而改关系的人正等着看新范围
// 在预览里生效，中间这段时间会以为没保存上。
func taRefreshDictAfterMapChange(envID string) {
	if envID == "" {
		return
	}
	env, err := taGetEnv(envID)
	if err != nil || env == nil {
		return
	}
	taRecalcDictVersion(env, time.Now())
}

// taLoadRoomSiteMap 一次取出整个环境的对应关系，按 room_id 分组。
//
// 给桌台列表用：那页一屏三十行，每行查一次库就是三十次往返。关系表整体也就几十
// 上百行，一次取完在内存里分组反而更快。
func taLoadRoomSiteMap(envID string) map[string][]map[string]interface{} {
	out := map[string][]map[string]interface{}{}
	rows, err := database.DB.Query(`
		SELECT rs.room_id, rs.site_id, COALESCE(s.site_name,''), COALESCE(s.watched,0),
		       rs.source, rs.enabled, rs.hits
		FROM table_alert_room_sites rs
		LEFT JOIN table_alert_sites s ON s.env_id=rs.env_id AND s.site_id=rs.site_id
		WHERE rs.env_id=?
		ORDER BY COALESCE(s.watched,0) DESC, s.site_name, rs.site_id`, envID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var roomID, siteID, siteName, source string
		var watched, enabled bool
		var hits int64
		if rows.Scan(&roomID, &siteID, &siteName, &watched, &source, &enabled, &hits) != nil {
			continue
		}
		out[roomID] = append(out[roomID], map[string]interface{}{
			"site_id": siteID, "site_name": siteName, "watched": watched,
			"source": source, "enabled": enabled, "hits": hits,
		})
	}
	return out
}

// HandleTASetRoomSites PUT /api/table-alert/rooms/{room_id}/use-sites
//
// 整行覆盖某张桌台的使用站点：在桌台列表上直接勾选，比逐条增删自然得多。
//
// 自动发现的关系在这里被取消勾选时保留行、只置 enabled=0，而不是删掉——删掉的话
// 下次导入候选又会把它加回来，人的判断就白做了。
func HandleTASetRoomSites(w http.ResponseWriter, r *http.Request) {
	if !taRequirePerm(w, r, taPermSiteManage) {
		return
	}
	roomID := mux.Vars(r)["room_id"]
	var req struct {
		EnvID   string   `json:"env_id"`
		SiteIDs []string `json:"site_ids"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.EnvID == "" {
		respondError(w, http.StatusBadRequest, "无效的请求")
		return
	}

	want := map[string]bool{}
	for _, v := range req.SiteIDs {
		if v = strings.TrimSpace(v); v != "" {
			want[v] = true
		}
	}

	// 先看现有的，才能区分「新增」「重新启用」「取消勾选」三种动作
	existing := map[string]bool{} // site_id → enabled
	rows, err := database.DB.Query(
		`SELECT site_id, enabled FROM table_alert_room_sites WHERE env_id=? AND room_id=?`,
		req.EnvID, roomID)
	if err == nil {
		for rows.Next() {
			var sid string
			var en bool
			if rows.Scan(&sid, &en) == nil {
				existing[sid] = en
			}
		}
		rows.Close()
	}

	now := time.Now()
	op := taOperator(r)
	for sid := range want {
		if _, ok := existing[sid]; ok {
			database.DB.Exec(`UPDATE table_alert_room_sites SET enabled=1
				WHERE env_id=? AND room_id=? AND site_id=?`, req.EnvID, roomID, sid)
			continue
		}
		database.DB.Exec(`INSERT INTO table_alert_room_sites
			(id, env_id, room_id, site_id, source, enabled, created_by, first_seen_at, last_seen_at)
			VALUES (?,?,?,?, 'manual', 1, ?, ?, ?)
			ON DUPLICATE KEY UPDATE enabled=1`,
			uuid.NewString(), req.EnvID, roomID, sid, op, now, now)
	}
	for sid := range existing {
		if !want[sid] {
			database.DB.Exec(`UPDATE table_alert_room_sites SET enabled=0
				WHERE env_id=? AND room_id=? AND site_id=?`, req.EnvID, roomID, sid)
		}
	}

	taRefreshDictAfterMapChange(req.EnvID)
	respondJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "count": len(want)})
}

// HandleTABatchSetRoomSites POST /api/table-alert/rooms/use-sites/batch
//
// 批量给多张桌台设置使用站点。
//
// 没有这个的话,109 张在用桌台要开 109 次弹窗——那不是"有点麻烦",是根本没人会去用,
// 于是对应关系永远配不全,心跳告警永远覆盖不到该覆盖的桌台。
//
// mode 两种语义,默认 add:
//
//	add     —— 并集。给一批桌台都加上某个站点,不动它们各自已有的其他站点。
//	replace —— 覆盖。整批桌台的使用站点变成完全一样。危险但有用:刚导完候选发现
//	           某批桌台配错了,一次改回来。
func HandleTABatchSetRoomSites(w http.ResponseWriter, r *http.Request) {
	if !taRequirePerm(w, r, taPermSiteManage) {
		return
	}
	var req struct {
		EnvID   string   `json:"env_id"`
		RoomIDs []string `json:"room_ids"`
		SiteIDs []string `json:"site_ids"`
		Mode    string   `json:"mode"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.EnvID == "" || len(req.RoomIDs) == 0 {
		respondError(w, http.StatusBadRequest, "env_id 和 room_ids 不能为空")
		return
	}
	if req.Mode == "" {
		req.Mode = "add"
	}
	// replace 且站点为空 = 把这批桌台全部移出监控范围。这是合法操作(比如整批下线),
	// 但不能是手滑的结果,所以要求显式传 mode=replace 才允许。
	if len(req.SiteIDs) == 0 && req.Mode != "replace" {
		respondError(w, http.StatusBadRequest, "没有选择站点。若要清空这批桌台的使用站点，请用 mode=replace")
		return
	}

	now := time.Now()
	op := taOperator(r)
	changed := 0
	for _, roomID := range req.RoomIDs {
		if roomID = strings.TrimSpace(roomID); roomID == "" {
			continue
		}
		for _, sid := range req.SiteIDs {
			if sid = strings.TrimSpace(sid); sid == "" {
				continue
			}
			database.DB.Exec(`INSERT INTO table_alert_room_sites
				(id, env_id, room_id, site_id, source, enabled, created_by, first_seen_at, last_seen_at)
				VALUES (?,?,?,?, 'manual', 1, ?, ?, ?)
				ON DUPLICATE KEY UPDATE enabled=1`,
				uuid.NewString(), req.EnvID, roomID, sid, op, now, now)
		}
		if req.Mode == "replace" {
			// 没选中的置 enabled=0 而不是删行,和单行保存保持一致:
			// 删掉的话下次导入候选又会加回来,人的判断就白做了。
			args := []interface{}{req.EnvID, roomID}
			q := `UPDATE table_alert_room_sites SET enabled=0 WHERE env_id=? AND room_id=?`
			if len(req.SiteIDs) > 0 {
				q += ` AND site_id NOT IN (?` + strings.Repeat(",?", len(req.SiteIDs)-1) + `)`
				for _, sid := range req.SiteIDs {
					args = append(args, sid)
				}
			}
			database.DB.Exec(q, args...)
		}
		changed++
	}

	taRefreshDictAfterMapChange(req.EnvID)
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"ok": true, "rooms": changed, "sites": len(req.SiteIDs), "mode": req.Mode,
	})
}
