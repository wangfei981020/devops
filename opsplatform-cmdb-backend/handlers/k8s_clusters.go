package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"opsplatform-cmdb-backend/crypto"
	"opsplatform-cmdb-backend/k8ssource"
	"opsplatform-cmdb-backend/logx"
)

// K8sClusterHandler 管理多集群纳管（只读）。凭据 AES 加密存，测连通只 list nodes。
type K8sClusterHandler struct {
	DB     *sql.DB
	Cipher *crypto.Cipher
	Pool   *k8ssource.Pool
}

func NewK8sClusterHandler(db *sql.DB, cipher *crypto.Cipher, pool *k8ssource.Pool) *K8sClusterHandler {
	return &K8sClusterHandler{DB: db, Cipher: cipher, Pool: pool}
}

func (h *K8sClusterHandler) Register(r *gin.RouterGroup) {
	r.GET("/k8s/clusters", h.List)
	r.POST("/k8s/clusters", h.Create)
	r.PUT("/k8s/clusters/:id", h.Update)
	r.DELETE("/k8s/clusters/:id", h.Delete)
	r.POST("/k8s/clusters/:id/test", h.Test)
}

type k8sClusterOut struct {
	ID             int    `json:"id"`
	Name           string `json:"name"`
	DisplayName    string `json:"display_name"`
	Environment    string `json:"environment"`
	Provider       string `json:"provider"`
	ProjectID      string `json:"project_id"`
	CloudAccountID int    `json:"cloud_account_id"` // 引用主机模块的云账号(GCP SA key 复用，不重复存)
	CloudAccount   string `json:"cloud_account"`    // 云账号名(展示)
	Location       string `json:"location"`
	Endpoint       string `json:"endpoint"`
	NodepoolLabel  string `json:"nodepool_label"` // 节点池标签 key（空=按角色/default 兜底）
	CostMode       string `json:"cost_mode"`      // cloud/idc/none，空=按 provider 自动推断
	HasKubeconfig  bool   `json:"has_kubeconfig"` // 不回传明文
	Enabled        int    `json:"enabled"`
}

func (h *K8sClusterHandler) List(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT k.id, k.name, k.display_name, k.environment, k.provider, k.project_id,
		k.cloud_account_id, COALESCE(a.name,''), k.location, k.endpoint, COALESCE(k.nodepool_label,''), COALESCE(k.cost_mode,''),
		CASE WHEN k.kubeconfig_enc IS NULL OR k.kubeconfig_enc='' THEN 0 ELSE 1 END,
		k.enabled
		FROM k8s_clusters k LEFT JOIN cloud_accounts a ON a.id=k.cloud_account_id
		ORDER BY k.environment, k.name`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []k8sClusterOut{}
	for rows.Next() {
		var r k8sClusterOut
		var hasKC int
		if err := rows.Scan(&r.ID, &r.Name, &r.DisplayName, &r.Environment, &r.Provider,
			&r.ProjectID, &r.CloudAccountID, &r.CloudAccount, &r.Location, &r.Endpoint, &r.NodepoolLabel, &r.CostMode, &hasKC, &r.Enabled); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		r.HasKubeconfig = hasKC == 1
		out = append(out, r)
	}
	c.JSON(http.StatusOK, out)
}

type k8sClusterIn struct {
	Name           string `json:"name"`
	DisplayName    string `json:"display_name"`
	Environment    string `json:"environment"`
	Provider       string `json:"provider"`
	ProjectID      string `json:"project_id"`
	CloudAccountID int    `json:"cloud_account_id"` // 引用主机模块云账号(GKE 用；GCP SA key 不在此重复配)
	Location       string `json:"location"`
	Endpoint       string `json:"endpoint"`
	NodepoolLabel  string `json:"nodepool_label"`
	CostMode       string `json:"cost_mode"`
	Kubeconfig     string `json:"kubeconfig"` // 空=保留原值(更新)/未配(创建)
	Enabled        *int   `json:"enabled"`
}

func (h *K8sClusterHandler) Create(c *gin.Context) {
	var in k8sClusterIn
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": "请求体解析失败"})
		return
	}
	if in.Name == "" {
		c.JSON(400, gin.H{"error": "集群名必填"})
		return
	}
	if in.Environment == "" {
		in.Environment = "DEV"
	}
	if in.Provider == "" {
		in.Provider = "gke"
	}
	kcEnc, err := h.encOrEmpty(in.Kubeconfig)
	if err != nil {
		c.JSON(500, gin.H{"error": "加密 kubeconfig 失败"})
		return
	}
	enabled := 1
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	res, err := h.DB.Exec(`INSERT INTO k8s_clusters
		(name, display_name, environment, provider, project_id, cloud_account_id, location, endpoint, nodepool_label, cost_mode, kubeconfig_enc, enabled)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		in.Name, in.DisplayName, in.Environment, in.Provider, in.ProjectID, in.CloudAccountID, in.Location, in.Endpoint, in.NodepoolLabel, in.CostMode, kcEnc, enabled)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	WriteAudit(h.DB, c, "create_k8s_cluster", in.Name)
	logx.J("k8s", "cluster_create", map[string]any{"id": id, "name": in.Name, "env": in.Environment, "provider": in.Provider})
	c.JSON(http.StatusOK, gin.H{"id": id})
}

func (h *K8sClusterHandler) Update(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var in k8sClusterIn
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": "请求体解析失败"})
		return
	}
	enabled := 1
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	// 凭据留空=保留原值；填了=加密覆盖
	if in.Kubeconfig != "" {
		enc, err := h.Cipher.Encrypt(in.Kubeconfig)
		if err != nil {
			c.JSON(500, gin.H{"error": "加密 kubeconfig 失败"})
			return
		}
		if _, err := h.DB.Exec(`UPDATE k8s_clusters SET kubeconfig_enc=? WHERE id=?`, enc, id); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
	}
	_, err := h.DB.Exec(`UPDATE k8s_clusters SET name=?, display_name=?, environment=?, provider=?,
		project_id=?, cloud_account_id=?, location=?, endpoint=?, nodepool_label=?, cost_mode=?, enabled=? WHERE id=?`,
		in.Name, in.DisplayName, in.Environment, in.Provider, in.ProjectID, in.CloudAccountID, in.Location, in.Endpoint, in.NodepoolLabel, in.CostMode, enabled, id)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	h.Pool.Invalidate(id) // 凭据/状态可能变，清连接缓存
	WriteAudit(h.DB, c, "update_k8s_cluster", in.Name)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *K8sClusterHandler) Delete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if _, err := h.DB.Exec(`DELETE FROM k8s_clusters WHERE id=?`, id); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	_, _ = h.DB.Exec(`DELETE FROM k8s_sync_state WHERE cluster_id=?`, id)
	h.Pool.Invalidate(id)
	WriteAudit(h.DB, c, "delete_k8s_cluster", c.Param("id"))
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Test 测连通：只 ServerVersion + 列 1 个节点，验证只读凭据可达 apiserver。
func (h *K8sClusterHandler) Test(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	h.Pool.Invalidate(id) // 用最新凭据
	cs, err := h.Pool.ClientFor(id)
	if err != nil {
		logx.J("k8s", "cluster_test_fail", map[string]any{"id": id, "err": err.Error()})
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ver, err := cs.Discovery().ServerVersion()
	if err != nil {
		logx.J("k8s", "cluster_test_fail", map[string]any{"id": id, "err": err.Error()})
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	nodes, err := cs.CoreV1().Nodes().List(ctx, metav1.ListOptions{Limit: 500})
	if err != nil {
		logx.J("k8s", "cluster_test_fail", map[string]any{"id": id, "version": ver.GitVersion, "err": err.Error()})
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error(), "version": ver.GitVersion})
		return
	}
	logx.J("k8s", "cluster_test_ok", map[string]any{"id": id, "version": ver.GitVersion, "nodes": len(nodes.Items)})
	c.JSON(http.StatusOK, gin.H{"ok": true, "version": ver.GitVersion, "nodes": len(nodes.Items)})
}

func (h *K8sClusterHandler) encOrEmpty(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	return h.Cipher.Encrypt(s)
}
