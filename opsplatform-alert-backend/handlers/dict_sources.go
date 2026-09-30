package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"opsplatform-alert-backend/alert"
	"opsplatform-alert-backend/database"
)

// External dictionary sources: where this platform fetches the names behind the
// ids that appear in logs.
//
// Address and key are rows in a table rather than environment variables on
// purpose. Rotating a key or moving the service is an operational act somebody
// should be able to do from the UI at 3am; making it a Secret edit plus a pod
// restart guarantees it gets postponed.

// dictSourceRow is the shape sent to the UI. Note what is missing: the key
// itself. It goes in and never comes back — the form shows whether one is set,
// and an empty value on save means "leave it alone".
type dictSourceRow struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	BaseURL       string `json:"base_url"`
	HasAPIKey     bool   `json:"has_api_key"`
	Env           string `json:"env"`
	RefreshSec    int    `json:"refresh_sec"`
	Status        int    `json:"status"`
	Description   string `json:"description"`
	LastSyncAt    string `json:"last_sync_at"`
	LastSyncOK    bool   `json:"last_sync_ok"`
	LastSyncError string `json:"last_sync_error"`
	LastVersion   string `json:"last_version"`
	RoomCount     int    `json:"room_count"`
	SiteCount     int    `json:"site_count"`
}

func HandleListDictSources(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`
		SELECT id, name, base_url, COALESCE(api_key,''), COALESCE(env,''),
		       COALESCE(refresh_sec,600), status, COALESCE(description,''),
		       last_sync_at, last_sync_ok, COALESCE(last_sync_error,''),
		       COALESCE(last_version,''), room_count, site_count
		FROM dict_sources ORDER BY id`)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()

	list := []dictSourceRow{}
	for rows.Next() {
		var it dictSourceRow
		var key string
		var syncAt *time.Time
		if rows.Scan(&it.ID, &it.Name, &it.BaseURL, &key, &it.Env,
			&it.RefreshSec, &it.Status, &it.Description,
			&syncAt, &it.LastSyncOK, &it.LastSyncError,
			&it.LastVersion, &it.RoomCount, &it.SiteCount) != nil {
			continue
		}
		it.HasAPIKey = key != ""
		if syncAt != nil {
			it.LastSyncAt = syncAt.Format(time.RFC3339)
		}
		list = append(list, it)
	}
	jsonSuccess(w, list)
}

type dictSourceReq struct {
	Name        string `json:"name"`
	BaseURL     string `json:"base_url"`
	APIKey      string `json:"api_key"`
	Env         string `json:"env"`
	RefreshSec  int    `json:"refresh_sec"`
	Status      *int   `json:"status"`
	Description string `json:"description"`
}

func (q *dictSourceReq) normalize() {
	q.Name = strings.TrimSpace(q.Name)
	q.BaseURL = strings.TrimRight(strings.TrimSpace(q.BaseURL), "/")
	q.APIKey = strings.TrimSpace(q.APIKey)
	q.Env = strings.TrimSpace(q.Env)
	if q.RefreshSec <= 0 {
		q.RefreshSec = 600
	}
}

func (q *dictSourceReq) validate() string {
	if q.Name == "" {
		return "名称不能为空"
	}
	if q.BaseURL == "" {
		return "服务地址不能为空"
	}
	if !strings.HasPrefix(q.BaseURL, "http://") && !strings.HasPrefix(q.BaseURL, "https://") {
		return "服务地址要带 http:// 或 https://，同集群可写 http://opsplatform-backend:8080"
	}
	return ""
}

func HandleCreateDictSource(w http.ResponseWriter, r *http.Request) {
	var req dictSourceReq
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}
	req.normalize()
	if msg := req.validate(); msg != "" {
		jsonError(w, http.StatusBadRequest, msg)
		return
	}
	status := 1
	if req.Status != nil {
		status = *req.Status
	}
	res, err := database.DB.Exec(`INSERT INTO dict_sources
		(name, base_url, api_key, env, refresh_sec, status, description)
		VALUES (?,?,?,?,?,?,?)`,
		req.Name, req.BaseURL, req.APIKey, req.Env, req.RefreshSec, status, req.Description)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "保存失败（名称可能重复）")
		return
	}
	id, _ := res.LastInsertId()
	jsonSuccess(w, map[string]interface{}{"id": id})
}

func HandleUpdateDictSource(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])
	var req dictSourceReq
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}
	req.normalize()
	if msg := req.validate(); msg != "" {
		jsonError(w, http.StatusBadRequest, msg)
		return
	}
	status := 1
	if req.Status != nil {
		status = *req.Status
	}

	// An empty key means "keep the current one". The UI never receives the key
	// back, so a blank field is the normal state when editing anything else —
	// treating it as "clear the key" would silently break the source every time
	// somebody fixed a typo in the description.
	if req.APIKey == "" {
		_, err := database.DB.Exec(`UPDATE dict_sources
			SET name=?, base_url=?, env=?, refresh_sec=?, status=?, description=? WHERE id=?`,
			req.Name, req.BaseURL, req.Env, req.RefreshSec, status, req.Description, id)
		if err != nil {
			jsonError(w, http.StatusBadRequest, "保存失败（名称可能重复）")
			return
		}
	} else {
		_, err := database.DB.Exec(`UPDATE dict_sources
			SET name=?, base_url=?, api_key=?, env=?, refresh_sec=?, status=?, description=? WHERE id=?`,
			req.Name, req.BaseURL, req.APIKey, req.Env, req.RefreshSec, status, req.Description, id)
		if err != nil {
			jsonError(w, http.StatusBadRequest, "保存失败（名称可能重复）")
			return
		}
	}

	// The cached roster belongs to the old address/key. Dropping it forces the
	// next read to go out fresh, so a corrected address takes effect now rather
	// than after the refresh interval expires.
	alert.InvalidateDictCache(r.Context(), id)
	jsonSuccess(w, map[string]interface{}{"message": "已保存"})
}

func HandleDeleteDictSource(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])

	var used int
	database.DB.QueryRow(`SELECT COUNT(*) FROM alert_rules WHERE dict_source_id=?`, id).Scan(&used)
	if used > 0 {
		// Deleting a source out from under a rule would turn every one of its
		// alerts into raw ids with no way to tell why.
		jsonError(w, http.StatusBadRequest, "还有 "+strconv.Itoa(used)+" 条告警规则在用这个字典源，请先改掉再删除")
		return
	}

	if _, err := database.DB.Exec(`DELETE FROM dict_sources WHERE id=?`, id); err != nil {
		jsonError(w, http.StatusInternalServerError, "删除失败")
		return
	}
	alert.InvalidateDictCache(r.Context(), id)
	jsonSuccess(w, map[string]interface{}{"message": "已删除"})
}

// HandleTestDictSource probes the endpoint with whatever the form currently
// holds, without saving anything.
//
// It reports the counts rather than just "成功", because the three failures an
// operator actually hits look identical from a bare OK: right address but wrong
// environment name returns an empty roster, a stale key returns 403, and a
// typo'd host returns a dial error. Showing rooms/sites/in-service makes the
// first one obvious.
func HandleTestDictSource(w http.ResponseWriter, r *http.Request) {
	var req dictSourceReq
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}
	req.normalize()
	if msg := req.validate(); msg != "" {
		jsonError(w, http.StatusBadRequest, msg)
		return
	}

	// Editing an existing source without retyping the key must still be
	// testable, so fall back to the stored one.
	if req.APIKey == "" {
		if idStr := mux.Vars(r)["id"]; idStr != "" {
			id, _ := strconv.Atoi(idStr)
			database.DB.QueryRow(`SELECT COALESCE(api_key,'') FROM dict_sources WHERE id=?`, id).Scan(&req.APIKey)
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	info, err := alert.TestDictSource(ctx, &alert.DictSource{
		Name:    req.Name,
		BaseURL: req.BaseURL,
		APIKey:  req.APIKey,
		Env:     req.Env,
	})
	if err != nil {
		jsonError(w, http.StatusBadRequest, "连接失败: "+err.Error())
		return
	}
	jsonSuccess(w, info)
}

// HandleSyncDictSource forces a real fetch right now, cache and all.
//
// Distinct from 测试连接, which only probes with the form's current values and
// deliberately writes nothing: a probe should not be able to change the state
// of a saved source. This one is the saved source actually going out, so it
// updates the cache, the fingerprint and the last-sync record.
//
// It exists because without it the first real sync only happens when some rule
// runs, and until then the list shows "尚未同步" next to a source that was just
// configured correctly — which reads as a failure.
func HandleSyncDictSource(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	// Drop the cache first so this really goes out to the source. Otherwise a
	// manual sync inside the refresh window would return the cached copy and
	// report success without having talked to anything — the opposite of what
	// somebody clicking "立即同步" is asking for.
	alert.InvalidateDictCache(ctx, id)

	d, err := alert.GetDict(ctx, id)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "同步失败: "+err.Error())
		return
	}

	inService := 0
	for _, r := range d.Rooms {
		if r.InService {
			inService++
		}
	}
	watched := 0
	for _, s := range d.Sites {
		if s.Watched {
			watched++
		}
	}
	jsonSuccess(w, map[string]interface{}{
		"version":      d.Version,
		"collected_at": d.CollectedAt,
		"collect_ok":   d.CollectOK,
		"room_count":   len(d.Rooms),
		"site_count":   len(d.Sites),
		"in_service":   inService,
		"watched":      watched,
		"stale":        d.Stale,
		"stale_why":    d.StaleWhy,
	})
}
