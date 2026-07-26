package handlers

import (
	"database/sql"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/crypto"
)

// ObsHandler 管理外部数据源接入（Prometheus/Loki/KubeSphere），只读查询用。
type ObsHandler struct {
	DB     *sql.DB
	Cipher *crypto.Cipher
}

func NewObsHandler(db *sql.DB, cipher *crypto.Cipher) *ObsHandler {
	return &ObsHandler{DB: db, Cipher: cipher}
}

func (h *ObsHandler) Register(r *gin.RouterGroup) {
	r.GET("/obs-endpoints", h.List)
	r.POST("/obs-endpoints", h.Create)
	r.PUT("/obs-endpoints/:id", h.Update)
	r.DELETE("/obs-endpoints/:id", h.Delete)
	r.POST("/obs-endpoints/:id/test", h.Test)
}

type obsOut struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	URL       string `json:"url"`
	Env       string `json:"env"`
	ClusterID int    `json:"cluster_id"`
	HasToken  bool   `json:"has_token"`
	Enabled   int    `json:"enabled"`
}

func (h *ObsHandler) List(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT id,name,type,url,env,cluster_id,
		CASE WHEN token_enc IS NULL OR token_enc='' THEN 0 ELSE 1 END, enabled FROM obs_endpoints ORDER BY type,env,name`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []obsOut{}
	for rows.Next() {
		var o obsOut
		var hasTok int
		if rows.Scan(&o.ID, &o.Name, &o.Type, &o.URL, &o.Env, &o.ClusterID, &hasTok, &o.Enabled) != nil {
			continue
		}
		o.HasToken = hasTok == 1
		out = append(out, o)
	}
	c.JSON(http.StatusOK, out)
}

type obsIn struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	URL       string `json:"url"`
	Env       string `json:"env"`
	ClusterID int    `json:"cluster_id"`
	Token     string `json:"token"` // 空=保留(更新)/不配(创建)
	Enabled   *int   `json:"enabled"`
}

func (h *ObsHandler) Create(c *gin.Context) {
	var in obsIn
	if err := c.ShouldBindJSON(&in); err != nil || in.Name == "" || in.Type == "" || in.URL == "" {
		c.JSON(400, gin.H{"error": "name/type/url 必填"})
		return
	}
	tokEnc := ""
	if in.Token != "" {
		e, err := h.Cipher.Encrypt(in.Token)
		if err != nil {
			c.JSON(500, gin.H{"error": "加密失败"})
			return
		}
		tokEnc = e
	}
	enabled := 1
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	res, err := h.DB.Exec(`INSERT INTO obs_endpoints (name,type,url,env,cluster_id,token_enc,enabled) VALUES (?,?,?,?,?,?,?)`,
		in.Name, in.Type, in.URL, in.Env, in.ClusterID, tokEnc, enabled)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	WriteAudit(h.DB, c, "create_obs_endpoint", in.Name)
	c.JSON(http.StatusOK, gin.H{"id": id})
}

func (h *ObsHandler) Update(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var in obsIn
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": "请求体解析失败"})
		return
	}
	enabled := 1
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	if in.Token != "" {
		e, err := h.Cipher.Encrypt(in.Token)
		if err != nil {
			c.JSON(500, gin.H{"error": "加密失败"})
			return
		}
		if _, err := h.DB.Exec(`UPDATE obs_endpoints SET token_enc=? WHERE id=?`, e, id); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
	}
	if _, err := h.DB.Exec(`UPDATE obs_endpoints SET name=?,type=?,url=?,env=?,cluster_id=?,enabled=? WHERE id=?`,
		in.Name, in.Type, in.URL, in.Env, in.ClusterID, enabled, id); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	WriteAudit(h.DB, c, "update_obs_endpoint", in.Name)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *ObsHandler) Delete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if _, err := h.DB.Exec(`DELETE FROM obs_endpoints WHERE id=?`, id); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	WriteAudit(h.DB, c, "delete_obs_endpoint", c.Param("id"))
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Test 测连通：GET url（带 token），返回状态码。
func (h *ObsHandler) Test(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	url, token, err := resolveEndpointByID(h.DB, h.Cipher, id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	code, body, err := obsGet(url, token, 10*time.Second)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	snippet := body
	if len(snippet) > 200 {
		snippet = snippet[:200]
	}
	c.JSON(http.StatusOK, gin.H{"ok": code >= 200 && code < 400, "status": code, "body": snippet})
}

// ---- 供 usage/loki/kubesphere 复用 ----

// resolveEndpoint 按 type + env + cluster 选最匹配的启用端点（集群>环境>通用）。
func resolveEndpoint(db *sql.DB, cipher *crypto.Cipher, typ, env string, clusterID int) (url, token string, err error) {
	rows, e := db.Query(`SELECT url, COALESCE(token_enc,''), env, cluster_id FROM obs_endpoints WHERE type=? AND enabled=1`, typ)
	if e != nil {
		return "", "", e
	}
	defer rows.Close()
	bestScore := -1
	var bestURL, bestEnc string
	for rows.Next() {
		var u, enc, e2 string
		var cid int
		if rows.Scan(&u, &enc, &e2, &cid) != nil {
			continue
		}
		score := 0
		if cid != 0 {
			if cid == clusterID {
				score += 2
			} else {
				continue // 指定了别的集群，不匹配
			}
		}
		if e2 != "" {
			if e2 == env {
				score += 1
			} else {
				continue
			}
		}
		if score > bestScore {
			bestScore, bestURL, bestEnc = score, u, enc
		}
	}
	if bestURL == "" {
		return "", "", errNoEndpoint(typ)
	}
	if bestEnc != "" {
		token, _ = cipher.Decrypt(bestEnc)
	}
	return bestURL, token, nil
}

func resolveEndpointByID(db *sql.DB, cipher *crypto.Cipher, id int) (url, token string, err error) {
	var enc sql.NullString
	e := db.QueryRow(`SELECT url, token_enc FROM obs_endpoints WHERE id=?`, id).Scan(&url, &enc)
	if e == sql.ErrNoRows {
		return "", "", errNoEndpoint("id")
	}
	if e != nil {
		return "", "", e
	}
	if enc.Valid && enc.String != "" {
		token, _ = cipher.Decrypt(enc.String)
	}
	return url, token, nil
}

type obsErr string

func (e obsErr) Error() string { return string(e) }
func errNoEndpoint(t string) error {
	return obsErr("未配置可用的 " + t + " 数据源（去 系统管理→数据源接入 配置）")
}

// obsGet 只读 GET（带可选 Bearer token）。
func obsGet(url, token string, timeout time.Duration) (int, string, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, "", err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	cli := &http.Client{Timeout: timeout}
	resp, err := cli.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, string(b), nil
}
