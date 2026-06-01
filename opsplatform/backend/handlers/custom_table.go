package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"opsplatform/database"
)

// v748: 兼容两种前端写法
//  - data: {"date":"..."}              ← object，正常
//  - data: "{\"date\":\"...\"}"        ← stringified（前端 JSON.stringify 多包了一层）
// 第二种存到 MySQL JSON 列会变成 JSON STRING 而不是 OBJECT，导致 JSON_EXTRACT/$.date 返回 NULL。
// 这里统一解一层，保证落库始终是 object 文本，避免数据脏化。
func normalizeJSONField(raw json.RawMessage, fallback string) string {
	if len(raw) == 0 {
		return fallback
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

// CustomTable 自定义表格
type CustomTable struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	CreatedBy   string `json:"created_by"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// CustomColumn 自定义列
type CustomColumn struct {
	ID           string          `json:"id"`
	TableID      string          `json:"table_id"`
	Name         string          `json:"name"`
	FieldKey     string          `json:"field_key"`
	FieldType    string          `json:"field_type"`
	Options      json.RawMessage `json:"options,omitempty"`
	Required     bool            `json:"required"`
	DefaultValue string          `json:"default_value"`
	SortOrder    int             `json:"sort_order"`
	CreatedAt    string          `json:"created_at"`
}

// CustomRow 自定义行数据
type CustomRow struct {
	ID             string          `json:"id"`
	TableID        string          `json:"table_id"`
	Data           json.RawMessage `json:"data"`
	Attachments    json.RawMessage `json:"attachments,omitempty"`
	SourceAPIKeyID *string         `json:"source_api_key_id"`
	CreatedBy      string          `json:"created_by"`
	CreatedAt      string          `json:"created_at"`
	UpdatedAt      string          `json:"updated_at"`
}

// HandleGetCustomTables 获取所有自定义表格
func HandleGetCustomTables(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`
		SELECT id, name, COALESCE(description, ''), COALESCE(icon, 'table'), COALESCE(created_by, ''), created_at, updated_at
		FROM custom_tables ORDER BY created_at DESC
	`)
	if err != nil {
		SafeError(w, "获取表格列表失败", http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	var tables []CustomTable
	for rows.Next() {
		var t CustomTable
		rows.Scan(&t.ID, &t.Name, &t.Description, &t.Icon, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt)
		tables = append(tables, t)
	}

	if tables == nil {
		tables = []CustomTable{}
	}
	respondJSON(w, http.StatusOK, tables)
}

// HandleCreateCustomTable 创建自定义表格
func HandleCreateCustomTable(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Icon        string `json:"icon"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "无效的请求", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "表格名称不能为空", http.StatusBadRequest)
		return
	}

	id := uuid.New().String()
	now := time.Now().Format("2006-01-02 15:04:05")
	createdBy := r.Header.Get("X-Operator")
	if req.Icon == "" {
		req.Icon = "table"
	}

	_, err := database.DB.Exec(`
		INSERT INTO custom_tables (id, name, description, icon, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, id, req.Name, req.Description, req.Icon, createdBy, now, now)

	if err != nil {
		SafeError(w, "创建表格失败", http.StatusInternalServerError, err)
		return
	}

	// 默认添加日期列
	dateColID := uuid.New().String()
	_, err = database.DB.Exec(`
		INSERT INTO custom_columns (id, table_id, name, field_key, field_type, required, sort_order, created_at)
		VALUES (?, ?, '日期', 'date', 'date', TRUE, 0, ?)
	`, dateColID, id, now)

	// 记录审计日志
	AddAuditLogFromRequest(r, "创建自定义表格", id, createdBy, "",
		fmt.Sprintf(`{"name":"%s","description":"%s"}`, req.Name, req.Description),
		fmt.Sprintf("创建自定义表格: %s", req.Name))

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"id":      id,
		"message": "表格创建成功",
	})
}

// HandleGetCustomTable 获取桌台维护记录列表（包含表结构、列定义、所有行）
// @Summary      获取桌台维护记录列表
// @Description  返回表元信息、列定义、所有行数据。响应里每行带 source_api_key_id 字段（标识是哪个 API Key 创建的）。
// @Tags         table_maintenance
// @Produce      json
// @Param        id   path      string  true  "table_id"
// @Success      200  {object}  GetCustomTableResponse
// @Failure      403  {object}  ErrorResponse "API Key 缺少 read 权限"
// @Security     ApiKeyAuth
// @Router       /custom-tables/{id} [get]
func HandleGetCustomTable(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	tableID := vars["id"]

	// 获取表格信息
	var table CustomTable
	var columnConfigStr *string
	err := database.DB.QueryRow(`
		SELECT id, name, COALESCE(description, ''), COALESCE(icon, 'table'), COALESCE(created_by, ''), created_at, updated_at, COALESCE(column_config, '')
		FROM custom_tables WHERE id = ?
	`, tableID).Scan(&table.ID, &table.Name, &table.Description, &table.Icon, &table.CreatedBy, &table.CreatedAt, &table.UpdatedAt, &columnConfigStr)
	if err != nil {
		log.Printf("[CustomTable] 查询表格 %s 失败: %v", tableID, err)
		http.Error(w, "表格不存在", http.StatusNotFound)
		return
	}

	var savedColumnConfig json.RawMessage
	if columnConfigStr != nil && *columnConfigStr != "" {
		savedColumnConfig = json.RawMessage(*columnConfigStr)
	}

	// 获取列配置
	colRows, err := database.DB.Query(`
		SELECT id, table_id, name, field_key, field_type, COALESCE(options, '[]'), required, COALESCE(default_value, ''), sort_order, created_at
		FROM custom_columns WHERE table_id = ? ORDER BY sort_order ASC
	`, tableID)
	if err != nil {
		SafeError(w, "获取列配置失败", http.StatusInternalServerError, err)
		return
	}
	defer colRows.Close()

	var columns []CustomColumn
	for colRows.Next() {
		var c CustomColumn
		var optionsStr string
		colRows.Scan(&c.ID, &c.TableID, &c.Name, &c.FieldKey, &c.FieldType, &optionsStr, &c.Required, &c.DefaultValue, &c.SortOrder, &c.CreatedAt)
		c.Options = json.RawMessage(optionsStr)
		columns = append(columns, c)
	}
	if columns == nil {
		columns = []CustomColumn{}
	}

	// 获取数据行
	dataRows, err := database.DB.Query(`
		SELECT id, table_id, COALESCE(data, '{}'), COALESCE(attachments, '[]'), source_api_key_id, COALESCE(created_by, ''), created_at, updated_at
		FROM custom_rows WHERE table_id = ? ORDER BY created_at DESC
	`, tableID)
	if err != nil {
		SafeError(w, "获取数据失败", http.StatusInternalServerError, err)
		return
	}
	defer dataRows.Close()

	var rows []CustomRow
	for dataRows.Next() {
		var row CustomRow
		var dataStr, attachStr string
		var srcKey sql.NullString
		dataRows.Scan(&row.ID, &row.TableID, &dataStr, &attachStr, &srcKey, &row.CreatedBy, &row.CreatedAt, &row.UpdatedAt)
		row.Data = json.RawMessage(dataStr)
		row.Attachments = json.RawMessage(attachStr)
		if srcKey.Valid {
			s := srcKey.String
			row.SourceAPIKeyID = &s
		}
		rows = append(rows, row)
	}
	if rows == nil {
		rows = []CustomRow{}
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"table":         table,
		"columns":       columns,
		"rows":          rows,
		"column_config": savedColumnConfig,
	})
}

// HandleSaveColumnConfig 保存列配置
func HandleSaveColumnConfig(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	tableID := vars["id"]

	var req struct {
		ColumnConfig json.RawMessage `json:"column_config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "无效的请求", http.StatusBadRequest)
		return
	}

	now := time.Now().Format("2006-01-02 15:04:05")
	_, err := database.DB.Exec(`
		UPDATE custom_tables SET column_config = ?, updated_at = ? WHERE id = ?
	`, string(req.ColumnConfig), now, tableID)

	if err != nil {
		SafeError(w, "保存列配置失败", http.StatusInternalServerError, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "列配置保存成功",
	})
}

// HandleUpdateCustomTable 更新表格信息
func HandleUpdateCustomTable(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	tableID := vars["id"]

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Icon        string `json:"icon"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "无效的请求", http.StatusBadRequest)
		return
	}

	now := time.Now().Format("2006-01-02 15:04:05")
	operator := r.Header.Get("X-Operator")
	_, err := database.DB.Exec(`
		UPDATE custom_tables SET name = ?, description = ?, icon = ?, updated_at = ? WHERE id = ?
	`, req.Name, req.Description, req.Icon, now, tableID)

	if err != nil {
		SafeError(w, "更新表格失败", http.StatusInternalServerError, err)
		return
	}

	// 记录审计日志
	AddAuditLogFromRequest(r, "更新自定义表格", tableID, operator, "",
		fmt.Sprintf(`{"name":"%s","description":"%s"}`, req.Name, req.Description),
		fmt.Sprintf("更新自定义表格: %s", req.Name))

	respondJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "更新成功"})
}

// HandleDeleteCustomTable 删除表格
func HandleDeleteCustomTable(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	tableID := vars["id"]
	operator := r.Header.Get("X-Operator")

	// 获取表格名称用于日志
	var tableName string
	database.DB.QueryRow("SELECT name FROM custom_tables WHERE id = ?", tableID).Scan(&tableName)

	// 删除相关数据
	database.DB.Exec("DELETE FROM custom_rows WHERE table_id = ?", tableID)
	database.DB.Exec("DELETE FROM custom_columns WHERE table_id = ?", tableID)
	database.DB.Exec("DELETE FROM custom_tables WHERE id = ?", tableID)

	// 记录审计日志
	AddAuditLogFromRequest(r, "删除自定义表格", tableID, operator, "",
		fmt.Sprintf(`{"name":"%s"}`, tableName),
		fmt.Sprintf("删除自定义表格: %s", tableName))

	respondJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "删除成功"})
}

// HandleAddCustomColumn 添加列
func HandleAddCustomColumn(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	tableID := vars["id"]

	var req struct {
		Name         string          `json:"name"`
		FieldKey     string          `json:"field_key"`
		FieldType    string          `json:"field_type"`
		Options      json.RawMessage `json:"options"`
		Required     bool            `json:"required"`
		DefaultValue string          `json:"default_value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "无效的请求", http.StatusBadRequest)
		return
	}

	if req.Name == "" || req.FieldKey == "" || req.FieldType == "" {
		http.Error(w, "列名、字段key和类型不能为空", http.StatusBadRequest)
		return
	}

	// 获取最大排序号
	var maxOrder int
	database.DB.QueryRow("SELECT COALESCE(MAX(sort_order), 0) FROM custom_columns WHERE table_id = ?", tableID).Scan(&maxOrder)

	id := uuid.New().String()
	now := time.Now().Format("2006-01-02 15:04:05")

	optionsStr := "[]"
	if req.Options != nil {
		optionsStr = string(req.Options)
	}

	_, err := database.DB.Exec(`
		INSERT INTO custom_columns (id, table_id, name, field_key, field_type, options, required, default_value, sort_order, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, tableID, req.Name, req.FieldKey, req.FieldType, optionsStr, req.Required, req.DefaultValue, maxOrder+1, now)

	if err != nil {
		SafeError(w, "添加列失败", http.StatusInternalServerError, err)
		return
	}

	// 更新表格时间
	database.DB.Exec("UPDATE custom_tables SET updated_at = ? WHERE id = ?", now, tableID)

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"id":      id,
		"message": "列添加成功",
	})
}

// HandleUpdateCustomColumn 更新列
func HandleUpdateCustomColumn(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	colID := vars["colId"]

	var req struct {
		Name         string          `json:"name"`
		FieldType    string          `json:"field_type"`
		Options      json.RawMessage `json:"options"`
		Required     bool            `json:"required"`
		DefaultValue string          `json:"default_value"`
		SortOrder    int             `json:"sort_order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "无效的请求", http.StatusBadRequest)
		return
	}

	optionsStr := "[]"
	if req.Options != nil {
		optionsStr = string(req.Options)
	}

	_, err := database.DB.Exec(`
		UPDATE custom_columns SET name = ?, field_type = ?, options = ?, required = ?, default_value = ?, sort_order = ?
		WHERE id = ?
	`, req.Name, req.FieldType, optionsStr, req.Required, req.DefaultValue, req.SortOrder, colID)

	if err != nil {
		SafeError(w, "更新列失败", http.StatusInternalServerError, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "更新成功"})
}

// HandleDeleteCustomColumn 删除列
func HandleDeleteCustomColumn(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	colID := vars["colId"]

	database.DB.Exec("DELETE FROM custom_columns WHERE id = ?", colID)

	respondJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "删除成功"})
}

// HandleAddCustomRow 添加行数据
// checkAPIKeyRowAccess 判断 JWT 用户是否有权限改/删 API Key 创建的记录
// op: "edit" | "delete"
// 返回: allowed=true 放行；false 时 reason 给前端展示
func checkAPIKeyRowAccess(r *http.Request, rowID, op string) (allowed bool, reason string) {
	var srcKey sql.NullString
	err := database.DB.QueryRow(`SELECT source_api_key_id FROM custom_rows WHERE id = ?`, rowID).Scan(&srcKey)
	if err != nil {
		// 查询失败或记录不存在，放行，让后续业务逻辑自然处理（404 等）
		return true, ""
	}
	if !srcKey.Valid {
		// 平台用户创建，沿用原 update/delete 权限码（不在此处校验）
		return true, ""
	}
	// API Key 创建的记录
	if r.Header.Get("X-API-Key") != "" {
		// 调用方是 API Key，已经过 scopes 校验
		return true, ""
	}
	// JWT 用户：必须有专属权限码
	permCode := "table_maintenance:edit_api_record"
	if op == "delete" {
		permCode = "table_maintenance:delete_api_record"
	}
	if isAdminOrHasPerm(r, permCode) {
		return true, ""
	}
	return false, "此记录由 API Key 创建，需要权限：" + permCode
}

// HandleAddCustomRow 创建桌台维护记录
// @Summary      创建桌台维护记录
// @Description  通过 API Key 创建一条新的维护记录。截图字段需先调 /storage/upload 拿到 path 再放入 data 中。
// @Tags         table_maintenance
// @Accept       json
// @Produce      json
// @Param        id   path      string             true  "table_id (固定值：09ccbe4d-fcce-44f2-b689-725894111e80)"
// @Param        body body      CreateRowRequest   true  "记录内容"
// @Success      200  {object}  CreateRowResponse
// @Failure      400  {object}  ErrorResponse "请求参数无效"
// @Failure      403  {object}  ErrorResponse "API Key 缺少 create 权限"
// @Security     ApiKeyAuth
// @Router       /custom-tables/{id}/rows [post]
func HandleAddCustomRow(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	tableID := vars["id"]

	var req struct {
		Data        json.RawMessage `json:"data"`
		Attachments json.RawMessage `json:"attachments"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "无效的请求", http.StatusBadRequest)
		return
	}

	id := uuid.New().String()
	now := time.Now().Format("2006-01-02 15:04:05")
	createdBy := r.Header.Get("X-Operator")

	// 若调用方是 API Key（中间件已塞 ctxAPIKeyID 到 context），落入 source_api_key_id
	var sourceAPIKeyID interface{}
	if v := r.Context().Value(ctxAPIKeyID); v != nil {
		if s, ok := v.(string); ok && s != "" {
			sourceAPIKeyID = s
		}
	}

	dataStr := normalizeJSONField(req.Data, "{}")
	attachStr := normalizeJSONField(req.Attachments, "[]")

	_, err := database.DB.Exec(`
		INSERT INTO custom_rows (id, table_id, data, attachments, source_api_key_id, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, id, tableID, dataStr, attachStr, sourceAPIKeyID, createdBy, now, now)

	if err != nil {
		SafeError(w, "添加数据失败", http.StatusInternalServerError, err)
		return
	}

	// 获取表格名称用于日志
	var tableName string
	database.DB.QueryRow("SELECT name FROM custom_tables WHERE id = ?", tableID).Scan(&tableName)

	// 记录审计日志
	AddAuditLogFromRequest(r, "添加记录", id, createdBy, "", dataStr,
		fmt.Sprintf("在表格 %s 中添加记录", tableName))

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"id":      id,
		"message": "数据添加成功",
	})
}

// HandleUpdateCustomRow 全量更新桌台维护记录
// @Summary      全量更新记录（PUT）
// @Description  PUT 会整体替换 data 字段：未传的子字段会被覆盖为空。如果需要保留原值，请用 PATCH。
// @Tags         table_maintenance
// @Accept       json
// @Produce      json
// @Param        id      path      string            true  "table_id"
// @Param        rowId   path      string            true  "记录ID"
// @Param        body    body      UpdateRowRequest  true  "完整记录内容"
// @Success      200     {object}  SuccessResponse
// @Failure      400     {object}  ErrorResponse "请求参数无效"
// @Failure      403     {object}  ErrorResponse "API Key 缺少 update 权限或行属于其他 API Key"
// @Security     ApiKeyAuth
// @Router       /custom-tables/{id}/rows/{rowId} [put]
func HandleUpdateCustomRow(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	rowID := vars["rowId"]

	if allowed, reason := checkAPIKeyRowAccess(r, rowID, "edit"); !allowed {
		sendError(w, reason, http.StatusForbidden)
		return
	}

	var req struct {
		Data        json.RawMessage `json:"data"`
		Attachments json.RawMessage `json:"attachments"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "无效的请求", http.StatusBadRequest)
		return
	}

	now := time.Now().Format("2006-01-02 15:04:05")

	dataStr := normalizeJSONField(req.Data, "{}")
	attachStr := normalizeJSONField(req.Attachments, "[]")

	operator := r.Header.Get("X-Operator")

	// 获取表格ID和名称用于日志
	var tableID, tableName string
	database.DB.QueryRow("SELECT table_id FROM custom_rows WHERE id = ?", rowID).Scan(&tableID)
	database.DB.QueryRow("SELECT name FROM custom_tables WHERE id = ?", tableID).Scan(&tableName)

	_, err := database.DB.Exec(`
		UPDATE custom_rows SET data = ?, attachments = ?, updated_at = ? WHERE id = ?
	`, dataStr, attachStr, now, rowID)

	if err != nil {
		SafeError(w, "更新数据失败", http.StatusInternalServerError, err)
		return
	}

	// 记录审计日志
	AddAuditLogFromRequest(r, "更新记录", rowID, operator, "", dataStr,
		fmt.Sprintf("在表格 %s 中更新记录", tableName))

	respondJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "更新成功"})
}

// HandlePatchCustomRow 部分更新桌台维护记录
// @Summary      部分更新记录（PATCH）
// @Description  PATCH 会浅合并 data 字段：未传的子字段保留原值，传了的子字段覆盖。**推荐二次编辑用这个**。
// @Tags         table_maintenance
// @Accept       json
// @Produce      json
// @Param        id      path      string            true  "table_id"
// @Param        rowId   path      string            true  "记录ID"
// @Param        body    body      PatchRowRequest   true  "需要更新的字段（只传要改的）"
// @Success      200     {object}  SuccessResponse
// @Failure      400     {object}  ErrorResponse
// @Failure      403     {object}  ErrorResponse
// @Security     ApiKeyAuth
// @Router       /custom-tables/{id}/rows/{rowId} [patch]
func HandlePatchCustomRow(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	rowID := vars["rowId"]

	if allowed, reason := checkAPIKeyRowAccess(r, rowID, "edit"); !allowed {
		sendError(w, reason, http.StatusForbidden)
		return
	}

	var req struct {
		Data        json.RawMessage `json:"data"`
		Attachments json.RawMessage `json:"attachments"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "无效的请求", http.StatusBadRequest)
		return
	}

	// 取现有 data/attachments
	var currentDataStr, currentAttachStr string
	var tableID string
	err := database.DB.QueryRow(
		`SELECT table_id, COALESCE(data, '{}'), COALESCE(attachments, '[]') FROM custom_rows WHERE id = ?`,
		rowID,
	).Scan(&tableID, &currentDataStr, &currentAttachStr)
	if err != nil {
		http.Error(w, "记录不存在", http.StatusNotFound)
		return
	}

	// v748: 解 data（兼容前端 stringify 双重编码）
	var patchData map[string]interface{}
	if len(req.Data) > 0 {
		normalized := normalizeJSONField(req.Data, "")
		if normalized != "" {
			if err := json.Unmarshal([]byte(normalized), &patchData); err != nil {
				http.Error(w, "data 字段格式错误", http.StatusBadRequest)
				return
			}
		}
	}

	// 合并 data（浅合并，传入的 key 覆盖原值）
	mergedDataStr := currentDataStr
	if len(patchData) > 0 {
		var current map[string]interface{}
		if err := json.Unmarshal([]byte(currentDataStr), &current); err != nil {
			current = map[string]interface{}{}
		}
		for k, v := range patchData {
			current[k] = v
		}
		merged, err := json.Marshal(current)
		if err != nil {
			SafeError(w, "合并数据失败", http.StatusInternalServerError, err)
			return
		}
		mergedDataStr = string(merged)
	}

	// attachments：若显式传入则替换，未传则保留原值
	attachStr := currentAttachStr
	if req.Attachments != nil {
		attachStr = normalizeJSONField(req.Attachments, "[]")
	}

	now := time.Now().Format("2006-01-02 15:04:05")
	operator := r.Header.Get("X-Operator")

	var tableName string
	database.DB.QueryRow("SELECT name FROM custom_tables WHERE id = ?", tableID).Scan(&tableName)

	_, err = database.DB.Exec(
		`UPDATE custom_rows SET data = ?, attachments = ?, updated_at = ? WHERE id = ?`,
		mergedDataStr, attachStr, now, rowID,
	)
	if err != nil {
		SafeError(w, "更新数据失败", http.StatusInternalServerError, err)
		return
	}

	AddAuditLogFromRequest(r, "部分更新记录", rowID, operator, currentDataStr, mergedDataStr,
		fmt.Sprintf("在表格 %s 中部分更新记录", tableName))

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "更新成功",
		"data":    json.RawMessage(mergedDataStr),
	})
}

// HandleDeleteCustomRow 删除桌台维护记录
// @Summary      删除记录
// @Tags         table_maintenance
// @Produce      json
// @Param        id      path      string  true  "table_id"
// @Param        rowId   path      string  true  "记录ID"
// @Success      200     {object}  SuccessResponse
// @Failure      403     {object}  ErrorResponse "API Key 缺少 delete 权限或行属于其他 API Key"
// @Security     ApiKeyAuth
// @Router       /custom-tables/{id}/rows/{rowId} [delete]
func HandleDeleteCustomRow(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	rowID := vars["rowId"]

	if allowed, reason := checkAPIKeyRowAccess(r, rowID, "delete"); !allowed {
		sendError(w, reason, http.StatusForbidden)
		return
	}

	operator := r.Header.Get("X-Operator")

	// 获取表格ID和名称用于日志
	var tableID, tableName string
	database.DB.QueryRow("SELECT table_id FROM custom_rows WHERE id = ?", rowID).Scan(&tableID)
	database.DB.QueryRow("SELECT name FROM custom_tables WHERE id = ?", tableID).Scan(&tableName)

	database.DB.Exec("DELETE FROM custom_rows WHERE id = ?", rowID)

	// 记录审计日志
	AddAuditLogFromRequest(r, "删除记录", rowID, operator, "", "",
		fmt.Sprintf("在表格 %s 中删除记录", tableName))

	respondJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "删除成功"})
}

// HandleGetCustomTableStats 获取表格统计数据
func HandleGetCustomTableStats(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	tableID := vars["id"]

	// 获取总记录数
	var totalRows int
	database.DB.QueryRow("SELECT COUNT(*) FROM custom_rows WHERE table_id = ?", tableID).Scan(&totalRows)

	// 获取今日新增
	var todayRows int
	database.DB.QueryRow("SELECT COUNT(*) FROM custom_rows WHERE table_id = ? AND DATE(created_at) = CURDATE()", tableID).Scan(&todayRows)

	// 获取本周新增
	var weekRows int
	database.DB.QueryRow("SELECT COUNT(*) FROM custom_rows WHERE table_id = ? AND created_at >= DATE_SUB(CURDATE(), INTERVAL WEEKDAY(CURDATE()) DAY)", tableID).Scan(&weekRows)

	// 获取本月新增
	var monthRows int
	database.DB.QueryRow("SELECT COUNT(*) FROM custom_rows WHERE table_id = ? AND YEAR(created_at) = YEAR(CURDATE()) AND MONTH(created_at) = MONTH(CURDATE())", tableID).Scan(&monthRows)

	// 获取列统计（针对select类型的列）
	colRows, _ := database.DB.Query(`
		SELECT id, name, field_key, field_type FROM custom_columns WHERE table_id = ? AND field_type IN ('select', 'multi_select')
	`, tableID)
	defer colRows.Close()

	columnStats := make(map[string]interface{})
	for colRows.Next() {
		var colID, name, fieldKey, fieldType string
		colRows.Scan(&colID, &name, &fieldKey, &fieldType)

		// 这里简化处理，实际需要解析JSON统计
		columnStats[fieldKey] = map[string]interface{}{
			"name": name,
			"type": fieldType,
		}
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"total_rows":   totalRows,
		"today_rows":   todayRows,
		"week_rows":    weekRows,
		"month_rows":   monthRows,
		"column_stats": columnStats,
	})
}

func init() {
	log.Println("自定义表格模块已加载")
}
