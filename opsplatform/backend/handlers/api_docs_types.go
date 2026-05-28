package handlers

// 本文件定义供 OpenAPI/Swagger 文档使用的请求和响应类型。
// 这些类型不直接在 handler 里使用（handler 沿用 anonymous struct + json.RawMessage 解析），
// 仅供 swaggo/swag 工具扫描生成 openapi.json。

// ===== 通用 =====

// ErrorResponse 通用错误响应
type ErrorResponse struct {
	Error string `json:"error" example:"请求参数无效"`
}

// SuccessResponse 通用成功响应
type SuccessResponse struct {
	Success bool   `json:"success" example:"true"`
	Message string `json:"message,omitempty" example:"操作成功"`
}

// FileAttachment 文件附件（截图等）
type FileAttachment struct {
	Path string `json:"path" example:"uploads/xxx.png"`
	Name string `json:"name" example:"通知开始截图.png"`
}

// ===== 桌台维护记录业务字段 =====

// MaintenanceRecordData 桌台维护记录业务字段
// 注意：所有字段都是字符串/字符串数组，与现有数据库存储格式一致
type MaintenanceRecordData struct {
	Date                  string           `json:"date,omitempty" example:"2026-05-28"`
	StartTime             string           `json:"start_time,omitempty" example:"2026-05-28 18:00"`
	EndTime               string           `json:"end_time,omitempty" example:"2026-05-28 18:15"`
	StartDuration         string           `json:"start_duration,omitempty" example:"两分钟内" enums:"两分钟内,五分钟内,十分钟内,十分钟以上"`
	CloseDuration         string           `json:"close_duration,omitempty" example:"五分钟内" enums:"两分钟内,五分钟内,十分钟内,十分钟以上"`
	NotifyStartScreenshot []FileAttachment `json:"notify_start_screenshot,omitempty"`
	NotifyEndScreenshot   []FileAttachment `json:"notify_end_screenshot,omitempty"`
	AffectedProjects      []string         `json:"affected_projects,omitempty" example:"利升,未接入"`
	AffectedSites         []string         `json:"affected_sites,omitempty"`
	AffectedTables        []string         `json:"affected_tables,omitempty" example:"P001,P002"`
	GameTypes             []string         `json:"game_types,omitempty"`
	MaintenanceType       string           `json:"maintenance_type,omitempty" example:"紧急维护" enums:"紧急维护,临时维护,例行维护,无"`
	Operation             string           `json:"operation,omitempty" example:"维护" enums:"维护,取消,重算,重派彩,包桌T人,漏操作,漏截图"`
	Operator              string           `json:"operator,omitempty" example:"张三"`
	Inspector             string           `json:"inspector,omitempty" example:"李四"`
	QCStatus              string           `json:"qc_status,omitempty" example:"正常" enums:"正常,异常"`
	Reason                string           `json:"reason,omitempty" example:"系统更新维护"`
	AffectSettlement      string           `json:"affect_settlement,omitempty" example:"否" enums:"是,否"`
	AffectedRoundIDs      string           `json:"affected_round_ids,omitempty"`
	Remark                string           `json:"remark,omitempty" example:"已完成，运行正常"`
	TableStatus           string           `json:"table_status,omitempty"`
}

// ===== 自定义表格端点 =====

// CreateRowRequest 创建行的请求体
type CreateRowRequest struct {
	Data        MaintenanceRecordData `json:"data"`
	Attachments []FileAttachment      `json:"attachments,omitempty"`
}

// UpdateRowRequest 全量更新行的请求体（PUT，注意：不传字段会被覆盖为空）
type UpdateRowRequest struct {
	Data        MaintenanceRecordData `json:"data"`
	Attachments []FileAttachment      `json:"attachments,omitempty"`
}

// PatchRowRequest 部分更新行的请求体（PATCH，不传字段保留原值）
type PatchRowRequest struct {
	Data        map[string]interface{} `json:"data" swaggertype:"object"`
	Attachments []FileAttachment       `json:"attachments,omitempty"`
}

// CreateRowResponse 创建行的响应
type CreateRowResponse struct {
	Success bool   `json:"success" example:"true"`
	ID      string `json:"id" example:"269bb917-fcce-44f2-b689-725894111e80"`
	Message string `json:"message" example:"数据添加成功"`
}

// GetCustomTableResponse 获取表数据的响应
type GetCustomTableResponse struct {
	Table        map[string]interface{}   `json:"table" swaggertype:"object"`
	Columns      []map[string]interface{} `json:"columns" swaggertype:"array,object"`
	Rows         []map[string]interface{} `json:"rows" swaggertype:"array,object"`
	ColumnConfig map[string]interface{}   `json:"column_config,omitempty" swaggertype:"object"`
}

// ===== 存储端点 =====

// UploadResponse 文件上传响应
type UploadResponse struct {
	Path string `json:"path" example:"uploads/2026/05/xxx.png"`
	URL  string `json:"url,omitempty"`
}

// PresignedURLResponse 预签名 URL 响应
type PresignedURLResponse struct {
	URL       string `json:"url" example:"https://minio.../uploads/xxx.png?X-Amz-Signature=..."`
	ExpiresIn int    `json:"expires_in" example:"3600"`
}

// BatchPresignRequest 批量预签名请求
type BatchPresignRequest struct {
	Paths []string `json:"paths" example:"uploads/a.png,uploads/b.png"`
}

// BatchPresignResponse 批量预签名响应
type BatchPresignResponse struct {
	URLs map[string]string `json:"urls" swaggertype:"object"`
}

// ===== 桌台层级 =====

// HierarchyResponse 桌台层级查询响应
type HierarchyResponse struct {
	Projects  []string `json:"projects" example:"利升"`
	Sites     []string `json:"sites" example:"PT,欧洲厅,卡卡湾"`
	Tables    []string `json:"tables" example:"P001,P002,E001"`
	GameTypes []string `json:"gameTypes" example:"百家乐,龙虎,轮盘"`
}
