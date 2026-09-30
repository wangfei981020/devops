package alert

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"opsplatform-alert-backend/database"
)

// External dictionaries turn the bare ids in a log line into something a person
// can read.
//
// A log line gives us `gameRoomId:46001` and `siteId: 1281834004026244608`, but
// an alert saying "46001 has had nobody join for 15 minutes" tells the on-call
// engineer nothing. The names live in the ops platform, which polls the business
// API every minute and is where people already curate them. Keeping a second
// copy here would drift from it the first time somebody renames a site, so we
// fetch instead — and use the fingerprint the other side publishes to avoid
// fetching the whole roster when nothing has changed.
//
// What this deliberately does NOT do is fail an alert when the dictionary is
// unreachable. An alert with a raw id in it is worth far more than no alert, so
// every failure path falls back to the last good copy and says so.

const (
	// dictCachePrefix namespaces the Redis keys. The cache is shared across
	// replicas: whichever one refreshes first saves the others the round trip.
	dictCachePrefix = "alert:dict:"

	// dictCacheTTL outlives any sane refresh interval on purpose. The cache is
	// the fallback when the source is down, so expiring it on a schedule would
	// throw away the very thing we need during an outage. It is refreshed by
	// version checks, not by expiry.
	dictCacheTTL = 7 * 24 * time.Hour

	// dictHTTPTimeout keeps a hung dictionary from stalling a rule's cycle.
	// The version probe is a few dozen bytes and the roster is a few hundred
	// rows, so anything slower than this is a sick source, not a big payload.
	dictHTTPTimeout = 8 * time.Second
)

// DictRoom is one room as the ops platform knows it. room_id is what appears in
// logs; the rest is for display and for deciding whether to watch it at all.
type DictRoom struct {
	RoomID    string `json:"room_id"`
	RoomNo    string `json:"room_no"`
	TableNo   string `json:"table_no"`
	InService bool   `json:"in_service"`
	Status    string `json:"status"`
	// Maintaining / MaintainSites 来自中台的 gameRoomMaintainList。
	//
	// 它给的是「这张桌台在哪些站点维护中」，粒度和心跳告警的 (站点 × 桌台) 维度
	// 正好一致 —— 所以抑制能精确到「A 站点维护、B 站点照常」，不必整张桌台一刀切。
	Maintaining   bool     `json:"maintaining"`
	MaintainSites []string `json:"maintain_site_ids"`
}

// DictPair 是一个「站点 × 桌台」组合，心跳告警的监控范围就是它们。
//
// 这层关系只能由运维平台维护：中台的桌台接口给不了它（桌台对象上只有维护时才非空
// 的 gameRoomMaintainList，siteStatus 恒为 null）。也不能从日志反推——日志只能证明
// 「出现过的组合存在」，证明不了「没出现的组合不存在」，而一张整周没有日志的在用
// 桌台恰恰是最该告警的那个。
type DictPair struct {
	RoomID string `json:"room_id"`
	SiteID string `json:"site_id"`
	Source string `json:"source"`
	Hits   int64  `json:"hits"`
}

// DictWindow 是一条例行维护窗口的定义，按原样带过来在本地判定。
//
// 运维平台不替我们算「现在是否处于窗口内」：字典带缓存，算好的布尔值会在缓存里
// 停留到下次刷新，窗口边界就会偏出去十几分钟。定义本身极少变动，正好走缓存。
type DictWindow struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	RepeatType string `json:"repeat_type"` // daily / weekly / monthly / once
	Weekdays   string `json:"weekdays"`    // 1~7 逗号分隔，1=周一
	MonthDays  string `json:"month_days"`
	OnceDate   string `json:"once_date"`
	StartTime  string `json:"start_time"` // HH:MM
	EndTime    string `json:"end_time"`   // HH:MM，小于 start 表示跨零点
	TableNos   string `json:"table_nos"`  // 逗号分隔的房间号/桌台号，* = 全部
	Action     string `json:"action"`     // suppress=窗口内不告警 / annotate=照常告警但标注
}

// DictSite is one site. Watched mirrors the star in the ops platform's site
// list — the platform already curates which sites matter, so we follow it
// rather than maintaining a second opinion here.
type DictSite struct {
	SiteID   string `json:"site_id"`
	SiteName string `json:"site_name"`
	Watched  bool   `json:"watched"`
}

// dictPayload is the wire shape of GET /api/table-alert/dict.
type dictPayload struct {
	Env         string       `json:"env"`
	Version     string       `json:"version"`
	CollectedAt string       `json:"collected_at"`
	CollectOK   bool         `json:"collect_ok"`
	RoomCount   int          `json:"room_count"`
	SiteCount   int          `json:"site_count"`
	Rooms       []DictRoom   `json:"rooms"`
	Sites       []DictSite   `json:"sites"`
	Pairs       []DictPair   `json:"pairs"`
	Windows     []DictWindow `json:"maint_windows"`
	// TZName / TZOffsetSec 是运维平台那边的时区。维护窗口里的 02:00 是那边的墙上
	// 时间，判定却发生在这边 —— 按本地时区去解释可能整整偏出八小时，而且不报错，
	// 只表现成抑制窗口错位。
	TZName      string `json:"tz_name"`
	TZOffsetSec int    `json:"tz_offset_sec"`
}

// dictVersionPayload is the wire shape of GET .../dict/version — deliberately
// tiny, because it is fetched far more often than the roster itself.
type dictVersionPayload struct {
	Env         string `json:"env"`
	Version     string `json:"version"`
	CollectedAt string `json:"collected_at"`
	CollectOK   bool   `json:"collect_ok"`
}

// Dict is a usable snapshot: maps rather than slices, because every caller
// looks rooms up by id.
type Dict struct {
	SourceID    int                 `json:"source_id"`
	Env         string              `json:"env"`
	Version     string              `json:"version"`
	CollectedAt string              `json:"collected_at"`
	CollectOK   bool                `json:"collect_ok"`
	Rooms       map[string]DictRoom `json:"rooms"`
	Sites       map[string]DictSite `json:"sites"`
	Pairs       []DictPair          `json:"pairs"`
	Windows     []DictWindow        `json:"maint_windows"`
	TZName      string              `json:"tz_name"`
	TZOffsetSec int                 `json:"tz_offset_sec"`
	SyncedAt    time.Time           `json:"synced_at"`

	// Stale means this snapshot came from cache after a failed refresh. Callers
	// must still alert — they just say so in the message rather than pretending
	// the names are current.
	Stale     bool      `json:"-"`
	StaleWhy  string    `json:"-"`
	CheckedAt time.Time `json:"checked_at"`
}

// RoomLabel renders a room for humans: the room number, falling back to the raw
// id when the dictionary has never heard of it.
//
// The fallback matters. A room that appears in logs but not in the dictionary is
// exactly the case somebody needs to see — printing nothing, or dropping the
// entry, would hide a newly opened room that nobody has registered yet.
func (d *Dict) RoomLabel(roomID string) string {
	if d != nil {
		if r, ok := d.Rooms[roomID]; ok && r.RoomNo != "" {
			return r.RoomNo
		}
	}
	return "(未知房间 " + roomID + ")"
}

// SiteLabel is RoomLabel for sites. An unnamed site is normal — the ops platform
// discovers site ids automatically and names get filled in later — so an empty
// name falls back to the id rather than rendering blank.
func (d *Dict) SiteLabel(siteID string) string {
	if d != nil {
		if s, ok := d.Sites[siteID]; ok && s.SiteName != "" {
			return s.SiteName
		}
	}
	return "(未命名站点 " + siteID + ")"
}

// DictSource is the configured endpoint. Address and key live in the database,
// not in the environment: rotating a key or moving the service should not need
// a Secret edit and a pod restart.
type DictSource struct {
	ID         int
	Name       string
	BaseURL    string
	APIKey     string
	Env        string
	RefreshSec int
}

var dictClient = &http.Client{Timeout: dictHTTPTimeout}

// dictLocks serialises refreshes per source so a burst of rules starting
// together does not all probe the same endpoint at once.
var dictLocks sync.Map

func dictLockFor(sourceID int) *sync.Mutex {
	v, _ := dictLocks.LoadOrStore(sourceID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// LoadDictSource reads one enabled source.
func LoadDictSource(id int) (*DictSource, error) {
	var s DictSource
	err := database.DB.QueryRow(`
		SELECT id, name, base_url, COALESCE(api_key,''), COALESCE(env,''), COALESCE(refresh_sec,600)
		FROM dict_sources WHERE id=? AND status=1`, id).
		Scan(&s.ID, &s.Name, &s.BaseURL, &s.APIKey, &s.Env, &s.RefreshSec)
	if err != nil {
		return nil, err
	}
	if s.RefreshSec <= 0 {
		s.RefreshSec = 600
	}
	return &s, nil
}

func (s *DictSource) url(path string) string {
	base := strings.TrimRight(s.BaseURL, "/")
	u := base + path
	if s.Env != "" {
		u += "?env=" + s.Env
	}
	return u
}

func (s *DictSource) get(ctx context.Context, path string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url(path), nil)
	if err != nil {
		return err
	}
	if s.APIKey != "" {
		req.Header.Set("X-API-Key", s.APIKey)
	}
	resp, err := dictClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode != http.StatusOK {
		// The body carries the actual reason ("API Key 缺少权限: table_alert:read"),
		// which is the whole difference between a misconfigured key and a down
		// service. Truncated because an HTML error page would swamp the log.
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateForLog(string(body), 200))
	}
	return json.Unmarshal(body, out)
}

func truncateForLog(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func dictCacheKey(sourceID int) string {
	return fmt.Sprintf("%s%d", dictCachePrefix, sourceID)
}

func loadDictCache(ctx context.Context, sourceID int) *Dict {
	raw, err := database.RDB.Get(ctx, dictCacheKey(sourceID)).Result()
	if err != nil || raw == "" {
		return nil
	}
	var d Dict
	if json.Unmarshal([]byte(raw), &d) != nil {
		return nil
	}
	if d.Rooms == nil {
		d.Rooms = map[string]DictRoom{}
	}
	if d.Sites == nil {
		d.Sites = map[string]DictSite{}
	}
	return &d
}

func saveDictCache(ctx context.Context, d *Dict) {
	b, err := json.Marshal(d)
	if err != nil {
		return
	}
	database.RDB.Set(ctx, dictCacheKey(d.SourceID), b, dictCacheTTL)
}

// GetDict returns a usable dictionary, refreshing only when it has to.
//
// The sequence is deliberate:
//
//  1. Inside the refresh interval, the cached copy is returned untouched — no
//     HTTP at all. A rule running every 5 minutes against a 10-minute interval
//     therefore talks to the source every other cycle at most.
//  2. Past the interval, probe the version. Same fingerprint means the roster
//     has not changed and the cached copy is still exactly right; only the
//     check timestamp moves.
//  3. Different fingerprint, or nothing cached, fetches the full roster.
//
// Every failure returns the cached copy marked Stale rather than an error,
// because the caller's alternative is to not alert at all.
func GetDict(ctx context.Context, sourceID int) (*Dict, error) {
	src, err := LoadDictSource(sourceID)
	if err != nil {
		return nil, fmt.Errorf("字典源不可用: %w", err)
	}

	mu := dictLockFor(sourceID)
	mu.Lock()
	defer mu.Unlock()

	cached := loadDictCache(ctx, sourceID)
	if cached != nil && time.Since(cached.CheckedAt) < time.Duration(src.RefreshSec)*time.Second {
		return cached, nil
	}

	stale := func(why string) (*Dict, error) {
		if cached == nil {
			return nil, fmt.Errorf("字典源 %s 拉取失败且无缓存: %s", src.Name, why)
		}
		c := *cached
		c.Stale = true
		c.StaleWhy = why
		log.Printf("[Dict] source=%d(%s) 刷新失败，继续用 %s 的缓存: %s",
			src.ID, src.Name, c.SyncedAt.Format(time.RFC3339), why)
		return &c, nil
	}

	if cached != nil && cached.Version != "" {
		var v dictVersionPayload
		if err := src.get(ctx, "/api/table-alert/dict/version", &v); err != nil {
			recordDictSync(src.ID, false, err.Error(), nil)
			return stale(err.Error())
		}
		if v.Version != "" && v.Version == cached.Version {
			c := *cached
			c.CheckedAt = time.Now()
			c.CollectOK = v.CollectOK
			c.CollectedAt = v.CollectedAt
			saveDictCache(ctx, &c)
			recordDictSync(src.ID, true, "", &c)
			return &c, nil
		}
	}

	var p dictPayload
	if err := src.get(ctx, "/api/table-alert/dict", &p); err != nil {
		recordDictSync(src.ID, false, err.Error(), nil)
		return stale(err.Error())
	}

	d := &Dict{
		SourceID:    src.ID,
		Env:         p.Env,
		Version:     p.Version,
		CollectedAt: p.CollectedAt,
		CollectOK:   p.CollectOK,
		Rooms:       make(map[string]DictRoom, len(p.Rooms)),
		Sites:       make(map[string]DictSite, len(p.Sites)),
		SyncedAt:    time.Now(),
		CheckedAt:   time.Now(),
	}
	for _, r := range p.Rooms {
		d.Rooms[r.RoomID] = r
	}
	for _, s := range p.Sites {
		d.Sites[s.SiteID] = s
	}
	d.Pairs = p.Pairs
	d.Windows = p.Windows
	d.TZName = p.TZName
	d.TZOffsetSec = p.TZOffsetSec

	// An empty roster from a source that says its own collection failed is not
	// a real "everything went away" — writing it over a good cache would wipe
	// the monitoring scope silently. Keep the old copy and flag it.
	if len(d.Rooms) == 0 && !d.CollectOK && cached != nil && len(cached.Rooms) > 0 {
		return stale("对方返回空名单且自报采集失败，保留上一次的名单")
	}

	saveDictCache(ctx, d)
	recordDictSync(src.ID, true, "", d)
	log.Printf("[Dict] source=%d(%s) 已同步 version=%s rooms=%d sites=%d pairs=%d windows=%d tz=%s",
		src.ID, src.Name, d.Version, len(d.Rooms), len(d.Sites), len(d.Pairs), len(d.Windows), d.TZName)
	return d, nil
}

// recordDictSync keeps the source's last-sync state visible in the UI. A
// dictionary that has been failing for a day is otherwise invisible: alerts
// keep firing with stale names and nobody has a reason to look.
func recordDictSync(sourceID int, ok bool, errMsg string, d *Dict) {
	if ok && d != nil {
		database.DB.Exec(`UPDATE dict_sources
			SET last_sync_at=?, last_sync_ok=1, last_sync_error='',
			    last_version=?, room_count=?, site_count=?
			WHERE id=?`,
			time.Now().UTC(), d.Version, len(d.Rooms), len(d.Sites), sourceID)
		return
	}
	database.DB.Exec(`UPDATE dict_sources SET last_sync_ok=0, last_sync_error=? WHERE id=?`,
		truncateForLog(errMsg, 480), sourceID)
}

// TestDictSource probes a source without touching the cache, for the "测试连接"
// button. It returns what the operator needs to tell apart the three failures
// they will actually hit: wrong address, wrong key, wrong environment name.
func TestDictSource(ctx context.Context, s *DictSource) (map[string]interface{}, error) {
	var p dictPayload
	if err := s.get(ctx, "/api/table-alert/dict", &p); err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"env":          p.Env,
		"version":      p.Version,
		"collected_at": p.CollectedAt,
		"collect_ok":   p.CollectOK,
		"room_count":   len(p.Rooms),
		"site_count":   len(p.Sites),
		"in_service":   countInService(p.Rooms),
		"watched":      countWatched(p.Sites),
	}, nil
}

func countInService(rooms []DictRoom) int {
	n := 0
	for _, r := range rooms {
		if r.InService {
			n++
		}
	}
	return n
}

func countWatched(sites []DictSite) int {
	n := 0
	for _, s := range sites {
		if s.Watched {
			n++
		}
	}
	return n
}

// InvalidateDictCache drops the cached roster for one source.
//
// Called when the address or key changes: the cache belongs to the old
// endpoint, and keeping it would make a corrected address appear not to work
// until the refresh interval happened to expire.
func InvalidateDictCache(ctx context.Context, sourceID int) {
	database.RDB.Del(ctx, dictCacheKey(sourceID))
}
