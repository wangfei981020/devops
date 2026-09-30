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

type taRoomSiteSaveReq struct {
	EnvID   string   `json:"env_id"`
	RoomID  string   `json:"room_id"`
	SiteIDs []string `json:"site_ids"`
	Remark  string   `json:"remark"`
}

// HandleTAAddRoomSiteMap POST /api/table-alert/room-site-map
// 人工补关系：日志里从没出现过的组合只能靠这个加进来。
func HandleTAAddRoomSiteMap(w http.ResponseWriter, r *http.Request) {
	if !taRequirePerm(w, r, taPermSiteManage) {
		return
	}
	var req taRoomSiteSaveReq
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		respondError(w, http.StatusBadRequest, "无效的请求")
		return
	}
	if req.EnvID == "" || req.RoomID == "" || len(req.SiteIDs) == 0 {
		respondError(w, http.StatusBadRequest, "env_id / room_id / site_ids 不能为空")
		return
	}
	now := time.Now()
	op := taOperator(r)
	added := 0
	for _, sid := range req.SiteIDs {
		if sid = strings.TrimSpace(sid); sid == "" {
			continue
		}
		// source 固定写 manual：人工加的关系不能被后续扫描覆盖或降级，
		// 否则"日志里没有所以删掉"会把人刚补上的那条抹掉。
		res, err := database.DB.Exec(`
			INSERT INTO table_alert_room_sites
			  (id, env_id, room_id, site_id, source, enabled, remark, created_by, first_seen_at, last_seen_at)
			VALUES (?,?,?,?, 'manual', 1, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE
			  source='manual', enabled=1, remark=VALUES(remark), last_seen_at=VALUES(last_seen_at)`,
			uuid.NewString(), req.EnvID, req.RoomID, sid, req.Remark, op, now, now)
		if err == nil {
			if n, _ := res.RowsAffected(); n > 0 {
				added++
			}
		}
	}
	taRefreshDictAfterMapChange(req.EnvID)
	respondJSON(w, http.StatusOK, map[string]interface{}{"added": added})
}

// HandleTAToggleRoomSiteMap PUT /api/table-alert/room-site-map/{id}
// 排除而不是删除：删掉之后下一轮扫描又会把它加回来，人的判断就丢了。
func HandleTAToggleRoomSiteMap(w http.ResponseWriter, r *http.Request) {
	if !taRequirePerm(w, r, taPermSiteManage) {
		return
	}
	id := mux.Vars(r)["id"]
	var req struct {
		Enabled *bool  `json:"enabled"`
		Remark  string `json:"remark"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		respondError(w, http.StatusBadRequest, "无效的请求")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	var envID string
	database.DB.QueryRow(`SELECT env_id FROM table_alert_room_sites WHERE id=?`, id).Scan(&envID)
	if _, err := database.DB.Exec(`
		UPDATE table_alert_room_sites SET enabled=?, remark=? WHERE id=?`,
		enabled, req.Remark, id); err != nil {
		respondError(w, http.StatusInternalServerError, "保存失败: "+err.Error())
		return
	}
	taRefreshDictAfterMapChange(envID)
	respondJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

// HandleTADeleteRoomSiteMap DELETE /api/table-alert/room-site-map/{id}
func HandleTADeleteRoomSiteMap(w http.ResponseWriter, r *http.Request) {
	if !taRequirePerm(w, r, taPermSiteManage) {
		return
	}
	id := mux.Vars(r)["id"]
	var envID string
	database.DB.QueryRow(`SELECT env_id FROM table_alert_room_sites WHERE id=?`, id).Scan(&envID)
	if _, err := database.DB.Exec(`DELETE FROM table_alert_room_sites WHERE id=?`, id); err != nil {
		respondError(w, http.StatusInternalServerError, "删除失败: "+err.Error())
		return
	}
	taRefreshDictAfterMapChange(envID)
	respondJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
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
