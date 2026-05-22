package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"opsplatform-alert-backend/database"
	"opsplatform-alert-backend/es"
	lokiclient "opsplatform-alert-backend/loki"
	"opsplatform-alert-backend/models"
)

// ExploreRequest ES 查询探索请求
type ExploreRequest struct {
	ESConnectionID int    `json:"es_connection_id"`
	ESIndex        string `json:"es_index"`
	Keyword        string `json:"keyword"`
	FilterFields   string `json:"filter_fields"`
	QueryDSL       string `json:"query_dsl"`
	StartTime      string `json:"start_time"` // ISO 8601 or "2006-01-02 15:04:05"
	EndTime        string `json:"end_time"`
	TimeRange      string `json:"time_range"` // 快捷: 5m, 15m, 1h, 6h, 24h
	Size           int    `json:"size"`
}

func HandleESExplore(w http.ResponseWriter, r *http.Request) {
	var req ExploreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}

	if req.ESConnectionID == 0 {
		jsonError(w, http.StatusBadRequest, "请选择 ES 连接")
		return
	}

	// Get ES connection
	var conn models.ESConnection
	err := database.DB.QueryRow(`SELECT id, name, url, version, username, password, api_key, skip_tls_verify
		FROM es_connections WHERE id = ?`, req.ESConnectionID).Scan(
		&conn.ID, &conn.Name, &conn.URL, &conn.Version, &conn.Username, &conn.Password, &conn.APIKey, &conn.SkipTLSVerify)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "ES 连接不存在")
		return
	}

	client, err := es.NewClient(conn)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "ES 客户端创建失败: "+err.Error())
		return
	}

	size := req.Size
	if size <= 0 {
		size = 20
	}
	if size > 1000 {
		size = 1000
	}

	esIndex := req.ESIndex
	if esIndex == "" {
		esIndex = "*"
	}

	// Build query
	var query map[string]interface{}

	if req.QueryDSL != "" {
		// Custom DSL
		if err := json.Unmarshal([]byte(req.QueryDSL), &query); err != nil {
			jsonError(w, http.StatusBadRequest, "自定义DSL格式错误: "+err.Error())
			return
		}
		query["size"] = size
	} else {
		// Build from params
		must := []map[string]interface{}{}
		mustNot := []map[string]interface{}{}

		// Time range
		timeFilter := buildTimeFilter(req.StartTime, req.EndTime, req.TimeRange)
		if timeFilter != nil {
			must = append(must, timeFilter)
		}

		// Keyword
		if req.Keyword != "" {
			must = append(must, map[string]interface{}{
				"query_string": map[string]interface{}{
					"query":            req.Keyword,
					"default_operator": "AND",
				},
			})
		}

		// Filter fields
		if req.FilterFields != "" {
			var filters []models.FilterField
			if err := json.Unmarshal([]byte(req.FilterFields), &filters); err == nil {
				for _, f := range filters {
					op := f.Op
					if op == "" {
						op = "match"
					}
					switch op {
					case "term":
						must = append(must, map[string]interface{}{"term": map[string]interface{}{f.Field: f.Value}})
					case "not_term":
						mustNot = append(mustNot, map[string]interface{}{"term": map[string]interface{}{f.Field: f.Value}})
					case "wildcard":
						must = append(must, map[string]interface{}{"wildcard": map[string]interface{}{f.Field: f.Value}})
					case "exists":
						must = append(must, map[string]interface{}{"exists": map[string]interface{}{"field": f.Field}})
					default:
						must = append(must, map[string]interface{}{"match": map[string]interface{}{f.Field: f.Value}})
					}
				}
			}
		}

		boolQuery := map[string]interface{}{"must": must}
		if len(mustNot) > 0 {
			boolQuery["must_not"] = mustNot
		}
		query = map[string]interface{}{
			"size": size,
			"sort": []map[string]interface{}{
				{"@timestamp": map[string]interface{}{"order": "desc"}},
			},
			"query": map[string]interface{}{
				"bool": boolQuery,
			},
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	result, err := client.Search(ctx, esIndex, query)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "ES 查询失败: "+err.Error())
		return
	}

	queryJSON, _ := json.MarshalIndent(query, "", "  ")

	jsonSuccess(w, map[string]interface{}{
		"total":    result.Total,
		"count":    len(result.Hits),
		"hits":     result.Hits,
		"query":    string(queryJSON),
		"es_name":  conn.Name,
		"es_index": esIndex,
	})
}

// HandleESIndices lists available indices for an ES connection
func HandleESIndices(w http.ResponseWriter, r *http.Request) {
	connID, _ := strconv.Atoi(r.URL.Query().Get("es_connection_id"))
	if connID == 0 {
		jsonError(w, http.StatusBadRequest, "请指定 ES 连接")
		return
	}

	var conn models.ESConnection
	err := database.DB.QueryRow(`SELECT id, name, url, version, username, password, api_key, skip_tls_verify
		FROM es_connections WHERE id = ?`, connID).Scan(
		&conn.ID, &conn.Name, &conn.URL, &conn.Version, &conn.Username, &conn.Password, &conn.APIKey, &conn.SkipTLSVerify)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "ES 连接不存在")
		return
	}

	client, err := es.NewClient(conn)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "ES 客户端创建失败: "+err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	indices, err := client.ListIndices(ctx)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "获取索引列表失败: "+err.Error())
		return
	}

	jsonSuccess(w, indices)
}

// HandleLokiExplore queries Loki with LogQL
func HandleLokiExplore(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LokiConnectionID int    `json:"loki_connection_id"`
		LogQL            string `json:"logql"`
		TimeRange        string `json:"time_range"`
		StartTime        string `json:"start_time"`
		EndTime          string `json:"end_time"`
		Size             int    `json:"size"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}
	if req.LokiConnectionID == 0 {
		jsonError(w, http.StatusBadRequest, "请选择 Loki 连接")
		return
	}
	if req.LogQL == "" {
		jsonError(w, http.StatusBadRequest, "LogQL 查询不能为空")
		return
	}

	conn := getLokiConn(req.LokiConnectionID)
	if conn == nil {
		jsonError(w, http.StatusBadRequest, "Loki 连接不存在")
		return
	}

	client := lokiclient.NewClient(*conn)

	size := req.Size
	if size <= 0 {
		size = 50
	}
	if size > 1000 {
		size = 1000
	}

	// Parse time range
	now := time.Now()
	start := now.Add(-15 * time.Minute)
	end := now

	log.Printf("[LokiExplore] TimeRange=%s StartTime=%s EndTime=%s LogQL=%s", req.TimeRange, req.StartTime, req.EndTime, req.LogQL)

	if req.TimeRange != "" && req.TimeRange != "custom" {
		d := parseTimeRangeStr(req.TimeRange)
		start = now.Add(-d)
	} else if req.StartTime != "" {
		if t, err := time.ParseInLocation("2006-01-02T15:04", req.StartTime, time.Local); err == nil {
			start = t
		}
		if req.EndTime != "" {
			if t, err := time.ParseInLocation("2006-01-02T15:04", req.EndTime, time.Local); err == nil {
				end = t
			}
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	result, err := client.QueryRange(ctx, req.LogQL, start, end, size)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Loki 查询失败: "+err.Error())
		return
	}

	hits := result.ToHits()

	jsonSuccess(w, map[string]interface{}{
		"total":     result.Total,
		"count":     len(hits),
		"hits":      hits,
		"query":     req.LogQL,
		"loki_name": conn.Name,
	})
}

func buildTimeFilter(startTime, endTime, timeRange string) map[string]interface{} {
	rangeFilter := map[string]interface{}{}

	if timeRange != "" {
		// Quick range: 5m, 15m, 1h, 6h, 24h
		rangeFilter["gte"] = fmt.Sprintf("now-%s", timeRange)
		rangeFilter["lte"] = "now"
	} else if startTime != "" || endTime != "" {
		if startTime != "" {
			rangeFilter["gte"] = startTime
		}
		if endTime != "" {
			rangeFilter["lte"] = endTime
		}
	} else {
		// Default: last 15 minutes
		rangeFilter["gte"] = "now-15m"
		rangeFilter["lte"] = "now"
	}

	return map[string]interface{}{
		"range": map[string]interface{}{
			"@timestamp": rangeFilter,
		},
	}
}

// parseTimeRangeStr parses time range strings like "5m", "1h", "24h", "7d"
func parseTimeRangeStr(s string) time.Duration {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err == nil {
			return time.Duration(days) * 24 * time.Hour
		}
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 15 * time.Minute // fallback
	}
	return d
}
