package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"opsplatform/database"
)

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
	ID          string          `json:"id"`
	TableID     string          `json:"table_id"`
	Data        json.RawMessage `json:"data"`
	Attachments json.RawMessage `json:"attachments,omitempty"`
	CreatedBy   string          `json:"created_by"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
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

// HandleGetCustomTable 获取单个表格详情（含列和数据）
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
		SELECT id, table_id, COALESCE(data, '{}'), COALESCE(attachments, '[]'), COALESCE(created_by, ''), created_at, updated_at
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
		dataRows.Scan(&row.ID, &row.TableID, &dataStr, &attachStr, &row.CreatedBy, &row.CreatedAt, &row.UpdatedAt)
		row.Data = json.RawMessage(dataStr)
		row.Attachments = json.RawMessage(attachStr)
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

	dataStr := "{}"
	if req.Data != nil {
		dataStr = string(req.Data)
	}
	attachStr := "[]"
	if req.Attachments != nil {
		attachStr = string(req.Attachments)
	}

	_, err := database.DB.Exec(`
		INSERT INTO custom_rows (id, table_id, data, attachments, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, id, tableID, dataStr, attachStr, createdBy, now, now)

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

// HandleUpdateCustomRow 更新行数据
func HandleUpdateCustomRow(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	rowID := vars["rowId"]

	var req struct {
		Data        json.RawMessage `json:"data"`
		Attachments json.RawMessage `json:"attachments"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "无效的请求", http.StatusBadRequest)
		return
	}

	now := time.Now().Format("2006-01-02 15:04:05")

	dataStr := "{}"
	if req.Data != nil {
		dataStr = string(req.Data)
	}
	attachStr := "[]"
	if req.Attachments != nil {
		attachStr = string(req.Attachments)
	}

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

// HandleDeleteCustomRow 删除行数据
func HandleDeleteCustomRow(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	rowID := vars["rowId"]
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
