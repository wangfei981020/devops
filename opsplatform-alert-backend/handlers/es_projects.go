package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"opsplatform-alert-backend/database"
	"opsplatform-alert-backend/es"
	"opsplatform-alert-backend/models"
)

type esProjectReq struct {
	Code          string `json:"code"`
	DisplayName   string `json:"display_name"`
	MatchKeywords string `json:"match_keywords"`
	Enabled       int    `json:"enabled"`
	SortOrder     int    `json:"sort_order"`
}

func HandleListESProjects(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`SELECT id, code, display_name, match_keywords, enabled, sort_order, created_at, updated_at
		FROM es_projects ORDER BY sort_order, id`)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "查询失败: "+err.Error())
		return
	}
	defer rows.Close()

	list := []map[string]interface{}{}
	for rows.Next() {
		var id, enabled, sortOrder int
		var code, displayName, matchKeywords, createdAt, updatedAt string
		rows.Scan(&id, &code, &displayName, &matchKeywords, &enabled, &sortOrder, &createdAt, &updatedAt)
		list = append(list, map[string]interface{}{
			"id":             id,
			"code":           code,
			"display_name":   displayName,
			"match_keywords": matchKeywords,
			"enabled":        enabled,
			"sort_order":     sortOrder,
			"created_at":     createdAt,
			"updated_at":     updatedAt,
		})
	}
	jsonSuccess(w, list)
}

func HandleCreateESProject(w http.ResponseWriter, r *http.Request) {
	var req esProjectReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}
	req.Code = strings.TrimSpace(req.Code)
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	req.MatchKeywords = strings.TrimSpace(req.MatchKeywords)
	if req.Code == "" || req.DisplayName == "" || req.MatchKeywords == "" {
		jsonError(w, http.StatusBadRequest, "code/display_name/match_keywords 不能为空")
		return
	}

	result, err := database.DB.Exec(`INSERT INTO es_projects (code, display_name, match_keywords, enabled, sort_order)
		VALUES (?, ?, ?, ?, ?)`, req.Code, req.DisplayName, req.MatchKeywords, req.Enabled, req.SortOrder)
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate") {
			jsonError(w, http.StatusBadRequest, "code 已存在: "+req.Code)
			return
		}
		jsonError(w, http.StatusInternalServerError, "创建失败: "+err.Error())
		return
	}
	id, _ := result.LastInsertId()
	SaveAuditLog(r, "create_es_project", "es_project", req.Code, fmt.Sprintf("创建ES项目 ID=%d", id))
	jsonSuccess(w, map[string]interface{}{"id": id})
}

func HandleUpdateESProject(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])
	var req esProjectReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}

	_, err := database.DB.Exec(`UPDATE es_projects SET code=?, display_name=?, match_keywords=?, enabled=?, sort_order=? WHERE id=?`,
		req.Code, req.DisplayName, req.MatchKeywords, req.Enabled, req.SortOrder, id)
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate") {
			jsonError(w, http.StatusBadRequest, "code 已存在: "+req.Code)
			return
		}
		jsonError(w, http.StatusInternalServerError, "更新失败: "+err.Error())
		return
	}
	SaveAuditLog(r, "update_es_project", "es_project", req.Code, fmt.Sprintf("更新ES项目 ID=%d", id))
	jsonSuccess(w, nil)
}

func HandleDeleteESProject(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])
	_, err := database.DB.Exec("DELETE FROM es_projects WHERE id = ?", id)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "删除失败: "+err.Error())
		return
	}
	SaveAuditLog(r, "delete_es_project", "es_project", fmt.Sprintf("ID=%d", id), "删除ES项目")
	jsonSuccess(w, nil)
}

var projectTokenRE = regexp.MustCompile(`^(g\d+|ls)$`)

var envTokenSet = map[string]bool{"prod": true, "uat": true}

type discoverCandidate struct {
	Code          string   `json:"code"`
	DisplayName   string   `json:"display_name"`
	MatchKeywords string   `json:"match_keywords"`
	SampleIndices []string `json:"sample_indices"`
	Existing      bool     `json:"existing"`
}

func HandleDiscoverESProjects(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ESConnectionID int `json:"es_connection_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}
	if req.ESConnectionID == 0 {
		jsonError(w, http.StatusBadRequest, "请选择 ES 连接")
		return
	}

	indices, err := listIndicesByConnID(r.Context(), req.ESConnectionID)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	existing := map[string]bool{}
	rows, _ := database.DB.Query("SELECT code FROM es_projects")
	if rows != nil {
		for rows.Next() {
			var c string
			rows.Scan(&c)
			existing[c] = true
		}
		rows.Close()
	}

	candMap := map[string]*discoverCandidate{}
	for _, idx := range indices {
		tokens := splitIndexTokens(idx)
		projTokens := []string{}
		envTokens := []string{}
		for _, t := range tokens {
			if envTokenSet[t] && !containsStr(envTokens, t) {
				envTokens = append(envTokens, t)
			} else if projectTokenRE.MatchString(t) && !containsStr(projTokens, t) {
				projTokens = append(projTokens, t)
			}
		}
		for _, p := range projTokens {
			for _, e := range envTokens {
				code := p + "-" + e
				c, ok := candMap[code]
				if !ok {
					c = &discoverCandidate{
						Code:          code,
						DisplayName:   strings.ToUpper(p) + "-" + strings.ToUpper(e),
						MatchKeywords: p + "," + e,
						SampleIndices: []string{},
						Existing:      existing[code],
					}
					candMap[code] = c
				}
				if len(c.SampleIndices) < 3 {
					c.SampleIndices = append(c.SampleIndices, idx)
				}
			}
		}
	}

	out := make([]discoverCandidate, 0, len(candMap))
	for _, c := range candMap {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Existing != out[j].Existing {
			return !out[i].Existing
		}
		return out[i].Code < out[j].Code
	})
	jsonSuccess(w, map[string]interface{}{"candidates": out})
}

func splitIndexTokens(s string) []string {
	s = strings.TrimSuffix(s, "*")
	out := []string{}
	for _, part := range strings.FieldsFunc(s, func(r rune) bool {
		return r == '-' || r == '_' || r == '.'
	}) {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func containsStr(arr []string, s string) bool {
	for _, x := range arr {
		if x == s {
			return true
		}
	}
	return false
}

func listIndicesByConnID(ctx context.Context, connID int) ([]string, error) {
	var conn models.ESConnection
	err := database.DB.QueryRow(`SELECT id, name, url, version, username, password, api_key, skip_tls_verify
		FROM es_connections WHERE id = ?`, connID).Scan(
		&conn.ID, &conn.Name, &conn.URL, &conn.Version, &conn.Username, &conn.Password, &conn.APIKey, &conn.SkipTLSVerify)
	if err != nil {
		return nil, fmt.Errorf("ES 连接不存在")
	}
	client, err := es.NewClient(conn)
	if err != nil {
		return nil, fmt.Errorf("ES 客户端创建失败: %s", err.Error())
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return client.ListIndices(cctx)
}
