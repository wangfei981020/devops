package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"opsplatform/database"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// ============================================================================
// 桌台管理（新菜单）
//
// 支持任意外部 OpenAPI（不同项目字段不同 / 方法不同）：
//   - HTTP method: GET / POST
//   - data_path: 响应里桌台数组的嵌套路径，如 "data" / "data.data" / "data.list"
//   - field_map: 外部字段名 → 内部字段名 (platform_id/platform_name/...)
//   - status_map: 外部 status 原值 → 内部 enabled/disabled
//
// 不影响桌台层级配置 / 桌台维护记录现有逻辑。
// ============================================================================

// 内部规范化字段（field_map 的 key 必须是这些之一）
const (
	FldPlatformID     = "platform_id"
	FldPlatformName   = "platform_name"
	FldPlatformNameZh = "platform_name_zh"
	FldRoomID         = "room_id"
	FldGameType       = "game_type"
	FldGameTypeName   = "game_type_name" // 可选：外部直接给的游戏中文名
	FldRoomStatus     = "room_status"
)

// 默认配置（向后兼容老项目 A，platformId 那种）
var defaultFieldMap = map[string]string{
	FldPlatformID:     "platformId",
	FldPlatformName:   "platformName",
	FldPlatformNameZh: "platformNameZh",
	FldRoomID:         "roomId",
	FldGameType:       "gameType",
	FldGameTypeName:   "",
	FldRoomStatus:     "roomStatus",
}
var defaultStatusMap = map[string][]string{
	"enabled":  {"0", "2", "Enable", "enable"},
	"disabled": {"1", "Disable", "disable"},
}

// ========== 类型定义 ==========

type ExternalDataSource struct {
	ID             string            `json:"id"`
	Project        string            `json:"project"`
	URL            string            `json:"url"`
	Method         string            `json:"method"`
	RequestBody    string            `json:"request_body"`
	DataPath       string            `json:"data_path"`
	FieldMap       map[string]string `json:"field_map"`
	StatusMap      map[string][]string `json:"status_map"`
	Enabled        bool              `json:"enabled"`
	LastSyncedAt   *time.Time        `json:"last_synced_at"`
	LastSyncStatus string            `json:"last_sync_status"`
	LastSyncError  string            `json:"last_sync_error"`
	LastSyncCount  int               `json:"last_sync_count"`
	CreatedBy      string            `json:"created_by"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

type ExternalRoom struct {
	ID             string     `json:"id"`
	Project        string     `json:"project"`
	PlatformID     string     `json:"platform_id"`
	PlatformName   string     `json:"platform_name"`
	PlatformNameZh string     `json:"platform_name_zh"`
	RoomID         string     `json:"room_id"`
	GameType       string     `json:"game_type"`
	RoomStatus     int        `json:"room_status"` // 0=enabled, 1=disabled, 2=enabled（兼容已有）
	Status         string     `json:"status"`      // 派生：enabled / disabled
	SyncedAt       time.Time  `json:"synced_at"`
	DeletedAt      *time.Time `json:"deleted_at"`
}

type ExternalAlias struct {
	ID        string    `json:"id"`
	AliasType string    `json:"alias_type"`
	Code      string    `json:"code"`
	NameZh    string    `json:"name_zh"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// 规范化后的桌台条目（field_map 解析后的中间结果）
type normalizedRoom struct {
	PlatformID     string `json:"platform_id"`
	PlatformName   string `json:"platform_name"`
	PlatformNameZh string `json:"platform_name_zh"`
	RoomID         string `json:"room_id"`
	GameType       string `json:"game_type"`
	GameTypeName   string `json:"game_type_name"`
	Status         string `json:"status"` // enabled / disabled
	RawStatus      string `json:"raw_status"`
}

// ========== 字段提取 / 状态映射 ==========

// asString 把任意 interface{} 安全转成 string（数字也转）
func asString(v interface{}) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		if x == float64(int(x)) {
			return fmt.Sprintf("%d", int(x))
		}
		return fmt.Sprintf("%v", x)
	}
	return fmt.Sprintf("%v", v)
}

// extractByPath 按点分路径从 map 里取嵌套值，如 "data.data" / "data"
// 返回最深层的数组（[]interface{}）或错误
func extractByPath(root map[string]interface{}, path string) ([]interface{}, error) {
	if strings.TrimSpace(path) == "" {
		path = "data.data" // 默认
	}
	parts := strings.Split(path, ".")
	var cur interface{} = root
	for _, p := range parts {
		m, ok := cur.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("路径 '%s' 在 '%s' 处不是对象", path, p)
		}
		v, exists := m[p]
		if !exists {
			return nil, fmt.Errorf("路径 '%s' 在 '%s' 处不存在", path, p)
		}
		cur = v
	}
	arr, ok := cur.([]interface{})
	if !ok {
		return nil, fmt.Errorf("路径 '%s' 末端不是数组", path)
	}
	return arr, nil
}

// normalizeRoom 用 field_map 把一条外部 raw 转成 normalizedRoom
func normalizeRoom(raw map[string]interface{}, fieldMap map[string]string, statusMap map[string][]string) normalizedRoom {
	get := func(internalKey string) string {
		externalKey, ok := fieldMap[internalKey]
		if !ok || externalKey == "" {
			return ""
		}
		return asString(raw[externalKey])
	}
	rawStatus := get(FldRoomStatus)
	status := mapRawStatusToInternal(rawStatus, statusMap)
	return normalizedRoom{
		PlatformID:     get(FldPlatformID),
		PlatformName:   get(FldPlatformName),
		PlatformNameZh: get(FldPlatformNameZh),
		RoomID:         get(FldRoomID),
		GameType:       get(FldGameType),
		GameTypeName:   get(FldGameTypeName),
		Status:         status,
		RawStatus:      rawStatus,
	}
}

// mapRawStatusToInternal 用 status_map 把外部原值转成 enabled/disabled
// 未命中：默认 enabled（保守，不轻易关闭桌台）
func mapRawStatusToInternal(rawValue string, statusMap map[string][]string) string {
	if statusMap == nil {
		statusMap = defaultStatusMap
	}
	for internal, externals := range statusMap {
		for _, e := range externals {
			if strings.EqualFold(rawValue, e) {
				return internal
			}
		}
	}
	return "enabled"
}

// mapRoomStatusToInternal （旧函数，保留兼容老 ExternalRoom.Status 派生逻辑）
func mapRoomStatusToInternal(rs int) string {
	if rs == 1 {
		return "disabled"
	}
	return "enabled"
}

// 把内部 enabled/disabled 反向编码成 int 存表（用于 room_status 字段）
func statusToInt(s string) int {
	if s == "disabled" {
		return 1
	}
	return 0
}

// ========== HTTP 调用（通用 GET/POST + 任意 data_path） ==========

// callExternalAPI 调外部 API，返回解析后的数组（[]map[string]interface{}）
// 适配任意 method + 任意 data_path
func callExternalAPI(method, url, body, dataPath string) ([]map[string]interface{}, map[string]interface{}, error) {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = "POST"
	}

	var bodyReader io.Reader
	if method == "POST" {
		if body == "" {
			body = "{}"
		}
		bodyReader = bytes.NewReader([]byte(body))
	}

	httpReq, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, nil, fmt.Errorf("构造请求失败: %w", err)
	}
	if method == "POST" {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, nil, fmt.Errorf("调外部 API 失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("读响应失败: %w", err)
	}
	if resp.StatusCode != 200 {
		return nil, nil, fmt.Errorf("外部 API 返回 HTTP %d: %s", resp.StatusCode, truncate(string(respBody), 300))
	}

	var rootGeneric interface{}
	if err := json.Unmarshal(respBody, &rootGeneric); err != nil {
		return nil, nil, fmt.Errorf("响应不是合法 JSON: %w (raw=%s)", err, truncate(string(respBody), 300))
	}
	root, ok := rootGeneric.(map[string]interface{})
	if !ok {
		return nil, nil, fmt.Errorf("响应顶层不是 JSON 对象")
	}

	arr, err := extractByPath(root, dataPath)
	if err != nil {
		return nil, root, err
	}
	out := make([]map[string]interface{}, 0, len(arr))
	for _, item := range arr {
		if m, ok := item.(map[string]interface{}); ok {
			out = append(out, m)
		}
	}
	return out, root, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// ========== 数据源 CRUD ==========

// 解析 field_map/status_map JSON 字符串到 map
func parseFieldMap(s string) map[string]string {
	if s == "" {
		return cloneFieldMap(defaultFieldMap)
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(s), &m); err != nil || m == nil {
		return cloneFieldMap(defaultFieldMap)
	}
	return m
}

func parseStatusMap(s string) map[string][]string {
	if s == "" {
		return cloneStatusMap(defaultStatusMap)
	}
	var m map[string][]string
	if err := json.Unmarshal([]byte(s), &m); err != nil || m == nil {
		return cloneStatusMap(defaultStatusMap)
	}
	return m
}

func cloneFieldMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
func cloneStatusMap(m map[string][]string) map[string][]string {
	out := make(map[string][]string, len(m))
	for k, v := range m {
		cp := make([]string, len(v))
		copy(cp, v)
		out[k] = cp
	}
	return out
}

func scanDataSource(scanner interface {
	Scan(dest ...interface{}) error
}) (*ExternalDataSource, error) {
	var s ExternalDataSource
	var lastSyncedAt sql.NullTime
	var method, dataPath, fieldMapStr, statusMapStr string
	var enabled int
	if err := scanner.Scan(&s.ID, &s.Project, &s.URL, &method, &s.RequestBody, &dataPath,
		&fieldMapStr, &statusMapStr, &enabled, &lastSyncedAt,
		&s.LastSyncStatus, &s.LastSyncError, &s.LastSyncCount,
		&s.CreatedBy, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return nil, err
	}
	if method == "" {
		method = "POST"
	}
	if dataPath == "" {
		dataPath = "data.data"
	}
	s.Method = method
	s.DataPath = dataPath
	s.FieldMap = parseFieldMap(fieldMapStr)
	s.StatusMap = parseStatusMap(statusMapStr)
	s.Enabled = enabled == 1
	if lastSyncedAt.Valid {
		t := lastSyncedAt.Time
		s.LastSyncedAt = &t
	}
	return &s, nil
}

const selectDataSourceSQL = `SELECT id, project, url, COALESCE(method, 'POST'), request_body, COALESCE(data_path, 'data.data'),
       COALESCE(field_map, ''), COALESCE(status_map, ''),
       enabled, last_synced_at, COALESCE(last_sync_status, ''), COALESCE(last_sync_error, ''),
       COALESCE(last_sync_count, 0), created_by, created_at, updated_at
FROM external_data_sources`

func HandleListExtDataSources(w http.ResponseWriter, r *http.Request) {
	if !isAdminOrHasPerm(r, "menu:table_management",
		"table_management:source_create", "table_management:source_update", "table_management:source_delete", "table_management:sync") {
		sendError(w, "无权限", http.StatusForbidden)
		return
	}
	rows, err := database.DB.Query(selectDataSourceSQL + " ORDER BY project ASC")
	if err != nil {
		SafeError(w, "查询失败", http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()
	list := []ExternalDataSource{}
	for rows.Next() {
		s, err := scanDataSource(rows)
		if err != nil {
			continue
		}
		list = append(list, *s)
	}
	respondJSON(w, http.StatusOK, list)
}

type createExtSourceReq struct {
	Project     string              `json:"project"`
	URL         string              `json:"url"`
	Method      string              `json:"method"`
	RequestBody string              `json:"request_body"`
	DataPath    string              `json:"data_path"`
	FieldMap    map[string]string   `json:"field_map"`
	StatusMap   map[string][]string `json:"status_map"`
}

func validateCreateUpdate(method, body string) error {
	method = strings.ToUpper(method)
	if method != "GET" && method != "POST" {
		return fmt.Errorf("method 只支持 GET / POST")
	}
	if method == "POST" && body != "" {
		var tmp interface{}
		if err := json.Unmarshal([]byte(body), &tmp); err != nil {
			return fmt.Errorf("请求体不是合法 JSON")
		}
	}
	return nil
}

func HandleCreateExtDataSource(w http.ResponseWriter, r *http.Request) {
	if !isAdminOrHasPerm(r, "table_management:source_create") {
		sendError(w, "无权限", http.StatusForbidden)
		return
	}
	_, username, _ := GetUserFromContext(r)

	var req createExtSourceReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, "请求参数无效", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Project) == "" || strings.TrimSpace(req.URL) == "" {
		sendError(w, "项目和 URL 不能为空", http.StatusBadRequest)
		return
	}
	if req.Method == "" {
		req.Method = "POST"
	}
	if err := validateCreateUpdate(req.Method, req.RequestBody); err != nil {
		sendError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.DataPath == "" {
		req.DataPath = "data.data"
	}
	if req.FieldMap == nil {
		req.FieldMap = defaultFieldMap
	}
	if req.StatusMap == nil {
		req.StatusMap = defaultStatusMap
	}
	fieldMapJSON, _ := json.Marshal(req.FieldMap)
	statusMapJSON, _ := json.Marshal(req.StatusMap)

	id := uuid.New().String()
	_, err := database.DB.Exec(`
		INSERT INTO external_data_sources (id, project, url, method, request_body, data_path, field_map, status_map, enabled, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?)
	`, id, req.Project, req.URL, strings.ToUpper(req.Method), req.RequestBody, req.DataPath,
		string(fieldMapJSON), string(statusMapJSON), username)
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate") {
			sendError(w, "该项目已配置过数据源", http.StatusBadRequest)
			return
		}
		SafeError(w, "创建失败", http.StatusInternalServerError, err)
		return
	}
	log.Printf("[ExternalTables] user=%s created data source for project=%s method=%s", username, req.Project, req.Method)
	respondJSON(w, http.StatusOK, map[string]interface{}{"success": true, "id": id})
}

type updateExtSourceReq struct {
	URL         *string              `json:"url"`
	Method      *string              `json:"method"`
	RequestBody *string              `json:"request_body"`
	DataPath    *string              `json:"data_path"`
	FieldMap    *map[string]string   `json:"field_map"`
	StatusMap   *map[string][]string `json:"status_map"`
	Enabled     *bool                `json:"enabled"`
}

func HandleUpdateExtDataSource(w http.ResponseWriter, r *http.Request) {
	if !isAdminOrHasPerm(r, "table_management:source_update") {
		sendError(w, "无权限", http.StatusForbidden)
		return
	}
	id := mux.Vars(r)["id"]
	var req updateExtSourceReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, "请求参数无效", http.StatusBadRequest)
		return
	}

	sets := []string{}
	args := []interface{}{}
	if req.URL != nil {
		if strings.TrimSpace(*req.URL) == "" {
			sendError(w, "URL 不能为空", http.StatusBadRequest)
			return
		}
		sets = append(sets, "url = ?")
		args = append(args, *req.URL)
	}
	if req.Method != nil {
		m := strings.ToUpper(*req.Method)
		if m != "GET" && m != "POST" {
			sendError(w, "method 只支持 GET / POST", http.StatusBadRequest)
			return
		}
		sets = append(sets, "method = ?")
		args = append(args, m)
	}
	if req.RequestBody != nil {
		if *req.RequestBody != "" {
			var tmp interface{}
			if err := json.Unmarshal([]byte(*req.RequestBody), &tmp); err != nil {
				sendError(w, "请求体不是合法 JSON", http.StatusBadRequest)
				return
			}
		}
		sets = append(sets, "request_body = ?")
		args = append(args, *req.RequestBody)
	}
	if req.DataPath != nil {
		sets = append(sets, "data_path = ?")
		args = append(args, *req.DataPath)
	}
	if req.FieldMap != nil {
		b, _ := json.Marshal(*req.FieldMap)
		sets = append(sets, "field_map = ?")
		args = append(args, string(b))
	}
	if req.StatusMap != nil {
		b, _ := json.Marshal(*req.StatusMap)
		sets = append(sets, "status_map = ?")
		args = append(args, string(b))
	}
	if req.Enabled != nil {
		sets = append(sets, "enabled = ?")
		if *req.Enabled {
			args = append(args, 1)
		} else {
			args = append(args, 0)
		}
	}
	if len(sets) == 0 {
		sendError(w, "没有需要更新的字段", http.StatusBadRequest)
		return
	}
	args = append(args, id)
	if _, err := database.DB.Exec("UPDATE external_data_sources SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...); err != nil {
		SafeError(w, "更新失败", http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func HandleDeleteExtDataSource(w http.ResponseWriter, r *http.Request) {
	if !isAdminOrHasPerm(r, "table_management:source_delete") {
		sendError(w, "无权限", http.StatusForbidden)
		return
	}
	id := mux.Vars(r)["id"]

	var project string
	if err := database.DB.QueryRow(`SELECT project FROM external_data_sources WHERE id = ?`, id).Scan(&project); err != nil {
		sendError(w, "数据源不存在", http.StatusNotFound)
		return
	}
	tx, err := database.DB.Begin()
	if err != nil {
		SafeError(w, "事务失败", http.StatusInternalServerError, err)
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM external_rooms WHERE project = ?`, project); err != nil {
		SafeError(w, "清理桌台失败", http.StatusInternalServerError, err)
		return
	}
	if _, err := tx.Exec(`DELETE FROM external_data_sources WHERE id = ?`, id); err != nil {
		SafeError(w, "删除失败", http.StatusInternalServerError, err)
		return
	}
	tx.Commit()
	respondJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// ========== 测试连接（双面预览） ==========

type testConnectionReq struct {
	URL         string              `json:"url"`
	Method      string              `json:"method"`
	RequestBody string              `json:"request_body"`
	DataPath    string              `json:"data_path"`
	FieldMap    map[string]string   `json:"field_map"`
	StatusMap   map[string][]string `json:"status_map"`
}

func HandleTestConnection(w http.ResponseWriter, r *http.Request) {
	if !isAdminOrHasPerm(r, "table_management:source_create", "table_management:source_update") {
		sendError(w, "无权限", http.StatusForbidden)
		return
	}
	var req testConnectionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, "请求参数无效", http.StatusBadRequest)
		return
	}
	if req.URL == "" {
		sendError(w, "URL 不能为空", http.StatusBadRequest)
		return
	}
	if req.Method == "" {
		req.Method = "POST"
	}
	if req.DataPath == "" {
		req.DataPath = "data.data"
	}
	if req.FieldMap == nil || len(req.FieldMap) == 0 {
		req.FieldMap = defaultFieldMap
	}
	if req.StatusMap == nil || len(req.StatusMap) == 0 {
		req.StatusMap = defaultStatusMap
	}

	rawRooms, rootJSON, err := callExternalAPI(req.Method, req.URL, req.RequestBody, req.DataPath)
	if err != nil {
		// 失败时也尝试把原始 JSON 截出来给前端帮助调试
		preview := map[string]interface{}{}
		if rootJSON != nil {
			preview = rootJSON
		}
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"success":     false,
			"error":       err.Error(),
			"root_sample": preview,
		})
		return
	}

	// 取前 5 条 raw + parsed 双面预览
	limit := 5
	if len(rawRooms) < limit {
		limit = len(rawRooms)
	}
	raw := rawRooms[:limit]
	parsed := make([]normalizedRoom, 0, limit)
	for _, r := range raw {
		parsed = append(parsed, normalizeRoom(r, req.FieldMap, req.StatusMap))
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"total":   len(rawRooms),
		"raw":     raw,
		"parsed":  parsed,
	})
}

// ========== 同步 ==========

func HandleSyncExtDataSource(w http.ResponseWriter, r *http.Request) {
	if !isAdminOrHasPerm(r, "table_management:sync") {
		sendError(w, "无权限", http.StatusForbidden)
		return
	}
	id := mux.Vars(r)["id"]
	added, updated, deleted, err := SyncOneDataSource(id)
	if err != nil {
		respondJSON(w, http.StatusOK, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"success": true, "added": added, "updated": updated, "deleted": deleted,
	})
}

// SyncOneDataSource 用通用 schema 同步一个数据源
func SyncOneDataSource(id string) (added, updated, deleted int, err error) {
	src, e := loadDataSourceByID(id)
	if e != nil {
		return 0, 0, 0, e
	}
	if !src.Enabled {
		return 0, 0, 0, fmt.Errorf("数据源已禁用")
	}

	rawRooms, _, callErr := callExternalAPI(src.Method, src.URL, src.RequestBody, src.DataPath)
	if callErr != nil {
		database.DB.Exec(`UPDATE external_data_sources SET last_sync_status='failed', last_sync_error=? WHERE id=?`, callErr.Error(), id)
		return 0, 0, 0, callErr
	}

	existing := map[string]string{}
	rows, _ := database.DB.Query(`SELECT id, platform_id, room_id FROM external_rooms WHERE project = ?`, src.Project)
	for rows.Next() {
		var rid, pid, rmid string
		rows.Scan(&rid, &pid, &rmid)
		existing[pid+"|"+rmid] = rid
	}
	rows.Close()

	seen := map[string]bool{}
	now := time.Now().Format("2006-01-02 15:04:05")
	count := 0
	for _, raw := range rawRooms {
		n := normalizeRoom(raw, src.FieldMap, src.StatusMap)
		if n.RoomID == "" {
			continue
		}
		count++
		// 如果 platform_id 为空（如项目 B 的 gamePlatformCode 是 null），用 platform_name 兜底
		pid := n.PlatformID
		if pid == "" {
			pid = n.PlatformName
		}
		key := pid + "|" + n.RoomID
		seen[key] = true
		statusInt := statusToInt(n.Status)

		if existingID, ok := existing[key]; ok {
			_, e := database.DB.Exec(`
				UPDATE external_rooms SET platform_name=?, platform_name_zh=?, game_type=?, room_status=?, synced_at=?, deleted_at=NULL
				WHERE id=?`,
				n.PlatformName, n.PlatformNameZh, n.GameType, statusInt, now, existingID)
			if e == nil {
				updated++
			}
		} else {
			_, e := database.DB.Exec(`
				INSERT INTO external_rooms (id, project, platform_id, platform_name, platform_name_zh, room_id, game_type, room_status, synced_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				uuid.New().String(), src.Project, pid, n.PlatformName, n.PlatformNameZh, n.RoomID, n.GameType, statusInt, now)
			if e == nil {
				added++
			}
		}

		// 顺便把 game_type_name（外部直接给的中文）自动注册成 gameType 别名（不覆盖用户编辑的）
		if n.GameType != "" && n.GameTypeName != "" && n.GameType != n.GameTypeName {
			var existingAliasID, currentZh string
			err := database.DB.QueryRow(`SELECT id, name_zh FROM external_aliases WHERE alias_type='gameType' AND code=?`, n.GameType).Scan(&existingAliasID, &currentZh)
			if err == sql.ErrNoRows {
				database.DB.Exec(`INSERT INTO external_aliases (id, alias_type, code, name_zh) VALUES (?, 'gameType', ?, ?)`,
					uuid.New().String(), n.GameType, n.GameTypeName)
			}
			// 用户已编辑过的 → 不动
			_ = currentZh
		}
	}

	for key, existingID := range existing {
		if !seen[key] {
			_, e := database.DB.Exec(`UPDATE external_rooms SET deleted_at=? WHERE id=? AND deleted_at IS NULL`, now, existingID)
			if e == nil {
				deleted++
			}
		}
	}

	database.DB.Exec(`
		UPDATE external_data_sources SET last_synced_at=?, last_sync_status='success', last_sync_error='', last_sync_count=?
		WHERE id=?`,
		now, count, id)

	log.Printf("[ExternalTables] sync project=%s method=%s added=%d updated=%d deleted=%d total=%d",
		src.Project, src.Method, added, updated, deleted, count)
	return added, updated, deleted, nil
}

func loadDataSourceByID(id string) (*ExternalDataSource, error) {
	row := database.DB.QueryRow(selectDataSourceSQL+" WHERE id = ?", id)
	return scanDataSource(row)
}

// ========== 自动桌台清单 ==========

func HandleListExternalRooms(w http.ResponseWriter, r *http.Request) {
	if !isAdminOrHasPerm(r, "menu:table_management",
		"table_management:source_create", "table_management:source_update", "table_management:source_delete", "table_management:sync") {
		sendError(w, "无权限", http.StatusForbidden)
		return
	}
	q := "SELECT id, project, platform_id, platform_name, COALESCE(platform_name_zh, ''), room_id, game_type, room_status, synced_at, deleted_at FROM external_rooms WHERE 1=1"
	args := []interface{}{}
	if proj := r.URL.Query().Get("project"); proj != "" {
		q += " AND project = ?"
		args = append(args, proj)
	}
	if r.URL.Query().Get("showDeleted") != "true" {
		q += " AND deleted_at IS NULL"
	}
	q += " ORDER BY project, platform_id, room_id"
	rows, err := database.DB.Query(q, args...)
	if err != nil {
		SafeError(w, "查询失败", http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()
	list := []ExternalRoom{}
	for rows.Next() {
		var rm ExternalRoom
		var deletedAt sql.NullTime
		if err := rows.Scan(&rm.ID, &rm.Project, &rm.PlatformID, &rm.PlatformName, &rm.PlatformNameZh,
			&rm.RoomID, &rm.GameType, &rm.RoomStatus, &rm.SyncedAt, &deletedAt); err != nil {
			continue
		}
		rm.Status = mapRoomStatusToInternal(rm.RoomStatus)
		if deletedAt.Valid {
			t := deletedAt.Time
			rm.DeletedAt = &t
		}
		list = append(list, rm)
	}
	respondJSON(w, http.StatusOK, list)
}

// ========== 别名 CRUD ==========

func HandleListAliases(w http.ResponseWriter, r *http.Request) {
	if !isAdminOrHasPerm(r, "menu:table_management",
		"table_management:source_create", "table_management:source_update", "table_management:source_delete",
		"table_management:sync", "table_management:alias_update") {
		sendError(w, "无权限", http.StatusForbidden)
		return
	}
	aliasType := r.URL.Query().Get("type")
	q := "SELECT id, alias_type, code, name_zh, created_at, updated_at FROM external_aliases"
	args := []interface{}{}
	if aliasType != "" {
		q += " WHERE alias_type = ?"
		args = append(args, aliasType)
	}
	q += " ORDER BY alias_type, code"
	rows, err := database.DB.Query(q, args...)
	if err != nil {
		SafeError(w, "查询失败", http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()
	list := []ExternalAlias{}
	for rows.Next() {
		var a ExternalAlias
		if err := rows.Scan(&a.ID, &a.AliasType, &a.Code, &a.NameZh, &a.CreatedAt, &a.UpdatedAt); err != nil {
			continue
		}
		list = append(list, a)
	}
	respondJSON(w, http.StatusOK, list)
}

type upsertAliasReq struct {
	AliasType string `json:"alias_type"`
	Code      string `json:"code"`
	NameZh    string `json:"name_zh"`
}

func HandleUpsertAlias(w http.ResponseWriter, r *http.Request) {
	if !isAdminOrHasPerm(r, "table_management:alias_update") {
		sendError(w, "无权限", http.StatusForbidden)
		return
	}
	var req upsertAliasReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, "请求参数无效", http.StatusBadRequest)
		return
	}
	if req.AliasType != "platform" && req.AliasType != "gameType" {
		sendError(w, "alias_type 只支持 platform / gameType", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Code) == "" {
		sendError(w, "code 不能为空", http.StatusBadRequest)
		return
	}
	var existingID string
	err := database.DB.QueryRow(`SELECT id FROM external_aliases WHERE alias_type=? AND code=?`, req.AliasType, req.Code).Scan(&existingID)
	if err == sql.ErrNoRows {
		_, err = database.DB.Exec(`INSERT INTO external_aliases (id, alias_type, code, name_zh) VALUES (?, ?, ?, ?)`,
			uuid.New().String(), req.AliasType, req.Code, req.NameZh)
		if err != nil {
			SafeError(w, "保存失败", http.StatusInternalServerError, err)
			return
		}
	} else if err == nil {
		_, err = database.DB.Exec(`UPDATE external_aliases SET name_zh=? WHERE id=?`, req.NameZh, existingID)
		if err != nil {
			SafeError(w, "更新失败", http.StatusInternalServerError, err)
			return
		}
	} else {
		SafeError(w, "查询失败", http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// ========== 定时同步 ==========

func StartExternalTableScheduler() {
	log.Println("[ExternalTables] 同步调度器已启动，每天 03:00 执行")
	for {
		now := time.Now()
		next := time.Date(now.Year(), now.Month(), now.Day(), 3, 0, 0, 0, now.Location())
		if !next.After(now) {
			next = next.Add(24 * time.Hour)
		}
		log.Printf("[ExternalTables] 下次同步：%s (还有 %v)", next.Format("2006-01-02 15:04:05"), next.Sub(now))
		time.Sleep(next.Sub(now))

		rows, err := database.DB.Query(`SELECT id, project FROM external_data_sources WHERE enabled = 1`)
		if err != nil {
			log.Printf("[ExternalTables] 调度器查询失败: %v", err)
			continue
		}
		ids := []struct{ ID, Project string }{}
		for rows.Next() {
			var id, proj string
			rows.Scan(&id, &proj)
			ids = append(ids, struct{ ID, Project string }{id, proj})
		}
		rows.Close()
		for _, s := range ids {
			added, updated, deleted, err := SyncOneDataSource(s.ID)
			if err != nil {
				log.Printf("[ExternalTables] 调度器同步 %s 失败: %v", s.Project, err)
			} else {
				log.Printf("[ExternalTables] 调度器同步 %s 成功: +%d ~%d -%d", s.Project, added, updated, deleted)
			}
		}
	}
}
