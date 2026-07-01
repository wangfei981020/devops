// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package handlers

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/video-manager/backend/internal/models"
	"github.com/video-manager/backend/pkg/response"
)

// streamPathImportHeaderKey maps a CSV header cell to a logical field name.
func streamPathImportHeaderKey(raw string) string {
	h := strings.TrimSpace(strings.TrimPrefix(raw, "\ufeff"))
	if h == "" {
		return ""
	}
	switch h {
	case "桌台号", "table_id", "Table_ID", "TABLE_ID":
		return "table_id"
	case "路径", "full_path", "Full_Path", "FULL_PATH":
		return "full_path"
	case "流区域", "视频流区域", "stream", "stream_name", "Stream Name", "STREAM_NAME":
		return "stream_name"
	case "stream_id", "视频流区域ID", "StreamID", "STREAM_ID":
		return "stream_id"
	case "编号", "id", "ID":
		return "_skip"
	}
	lower := strings.ToLower(h)
	switch lower {
	case "table_id":
		return "table_id"
	case "full_path":
		return "full_path"
	case "stream_id":
		return "stream_id"
	case "stream_name", "stream":
		return "stream_name"
	}
	return ""
}

func parseStreamPathImportCSV(r io.Reader) ([]models.StreamPathImportItem, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("读取 CSV 失败: %w", err)
	}
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		data = data[3:]
	}

	cr := csv.NewReader(bytes.NewReader(data))
	cr.LazyQuotes = true
	cr.TrimLeadingSpace = true

	records, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("CSV 格式无效: %w", err)
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("CSV 至少需要表头与一行数据")
	}

	header := records[0]
	idxTable, idxPath, idxStreamName, idxStreamID := -1, -1, -1, -1
	for i, cell := range header {
		key := streamPathImportHeaderKey(cell)
		switch key {
		case "table_id":
			idxTable = i
		case "full_path":
			idxPath = i
		case "stream_name":
			idxStreamName = i
		case "stream_id":
			idxStreamID = i
		}
	}

	if idxTable < 0 || idxPath < 0 {
		return nil, fmt.Errorf("CSV 缺少必填列：桌台号（table_id）、路径（full_path）")
	}
	if idxStreamID < 0 && idxStreamName < 0 {
		return nil, fmt.Errorf("CSV 需包含 流区域（或 stream_name）或 stream_id 列之一")
	}

	cell := func(row []string, idx int) string {
		if idx < 0 || idx >= len(row) {
			return ""
		}
		return row[idx]
	}

	var items []models.StreamPathImportItem
	for i := 1; i < len(records); i++ {
		row := records[i]
		tableID := strings.TrimSpace(cell(row, idxTable))
		fullPath := strings.TrimSpace(cell(row, idxPath))
		streamName := strings.TrimSpace(cell(row, idxStreamName))
		streamIDStr := strings.TrimSpace(cell(row, idxStreamID))

		if tableID == "" && fullPath == "" && streamName == "" && streamIDStr == "" {
			continue
		}

		lineNo := i + 1
		var streamID int64
		if streamIDStr != "" {
			id, perr := strconv.ParseInt(streamIDStr, 10, 64)
			if perr != nil || id < 1 {
				return nil, fmt.Errorf("第 %d 行 stream_id 无效: %q", lineNo, streamIDStr)
			}
			streamID = id
		}

		items = append(items, models.StreamPathImportItem{
			Line:       lineNo,
			TableID:    tableID,
			FullPath:   fullPath,
			StreamID:   streamID,
			StreamName: streamName,
		})
	}

	if len(items) == 0 {
		return nil, fmt.Errorf("没有可导入的数据行")
	}
	return items, nil
}

// Import handles POST /api/stream-paths/import (multipart field "file": CSV).
// Upsert rule: existing table_id updates stream_id and full_path; otherwise creates a row.
//
// @Summary Import stream paths from CSV
// @Description CSV columns: 桌台号(table_id), 路径(full_path), 流区域 or stream_id. UTF-8 with optional BOM.
// @Tags stream-paths
// @Accept multipart/form-data
// @Produce json
// @Security BearerAuth
// @Param file formData file true "CSV file"
// @Success 200 {object} response.Response{data=models.StreamPathImportResult}
// @Failure 400 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/stream-paths/import [post]
func (h *StreamPathHandler) Import(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		response.BadRequest(c, "请上传 CSV 文件（表单字段名 file）")
		return
	}

	src, err := file.Open()
	if err != nil {
		response.InternalServerError(c, "无法读取上传文件")
		return
	}
	defer src.Close()

	items, err := parseStreamPathImportCSV(src)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	result, err := h.service.ImportStreamPaths(c.Request.Context(), items)
	if err != nil {
		response.InternalServerError(c, "导入处理失败")
		return
	}
	response.Success(c, result)
}
