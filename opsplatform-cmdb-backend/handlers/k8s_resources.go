package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/k8ssource"
	"opsplatform-cmdb-backend/logx"
)

// K8sResourceHandler 负责集群资源同步 + 只读列表查询。
type K8sResourceHandler struct {
	DB   *sql.DB
	Pool *k8ssource.Pool
}

func NewK8sResourceHandler(db *sql.DB, pool *k8ssource.Pool) *K8sResourceHandler {
	return &K8sResourceHandler{DB: db, Pool: pool}
}

func (h *K8sResourceHandler) Register(r *gin.RouterGroup) {
	r.POST("/k8s/clusters/:id/sync", h.Sync)
	r.GET("/k8s/node-pools", h.NodePools)
	r.GET("/k8s/nodes", h.Nodes)
	r.GET("/k8s/namespaces", h.Namespaces)
	r.GET("/k8s/workloads", h.Workloads)
	r.GET("/k8s/pods", h.Pods)
	r.GET("/k8s/services", h.Services)
	r.GET("/k8s/ingresses", h.Ingresses)
	r.GET("/k8s/gateways", h.Gateways)
	r.GET("/k8s/httproutes", h.HTTPRoutes)
	r.GET("/k8s/virtualservices", h.VirtualServices)
	r.GET("/k8s/pvcs", h.PVCs)
	r.GET("/k8s/hpas", h.HPAs)
	r.GET("/k8s/changes", h.Changes)
	r.GET("/k8s/ns-projects", h.NsProjects)   // 命名空间→项目 映射(含未映射的命名空间)
	r.POST("/k8s/ns-projects", h.SetNsProject) // upsert 单条映射
}

// Sync 手动全量采集一个集群（阶段2 手动触发；周期同步阶段3 接调度器）。
func (h *K8sResourceHandler) Sync(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var poolLabel string
	if err := h.DB.QueryRow(`SELECT COALESCE(nodepool_label,'') FROM k8s_clusters WHERE id=?`, id).Scan(&poolLabel); err != nil {
		c.JSON(404, gin.H{"error": "集群不存在"})
		return
	}
	cs, err := h.Pool.ClientFor(id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	dc, err := h.Pool.DynamicFor(id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
	defer cancel()
	results := k8ssource.SyncCluster(ctx, h.DB, cs, dc, id, poolLabel)
	summary := gin.H{}
	failed := 0
	for _, r := range results {
		if r.Err != nil {
			failed++
			summary[r.Resource] = "err: " + r.Err.Error()
		} else {
			summary[r.Resource] = r.Count
		}
	}
	WriteAudit(h.DB, c, "sync_k8s_cluster", c.Param("id"))
	logx.J("k8s", "cluster_sync", map[string]any{"cluster_id": id, "failed": failed, "summary": summary})
	c.JSON(http.StatusOK, gin.H{"ok": failed == 0, "summary": summary})
}

// ---- 只读列表（通用扫描）----

func (h *K8sResourceHandler) NodePools(c *gin.Context) {
	h.list(c, `SELECT id,cluster_id,name,machine_type,node_count,version FROM k8s_node_pools`,
		[]filter{{"cluster_id", "cluster_id", true}}, "cluster_id,name", []string{"name"})
}

func (h *K8sResourceHandler) Nodes(c *gin.Context) {
	h.list(c, `SELECT id,cluster_id,name,pool,internal_ip,roles,machine_type,cpu_cap,mem_cap,os_image,kubelet_version,ready_status,last_heartbeat,conditions,COALESCE(conditions_json,'') AS conditions_json,pod_count,stuck FROM k8s_nodes`,
		[]filter{{"cluster_id", "cluster_id", true}, {"pool", "pool", false}}, "cluster_id,pool,name", []string{"name", "internal_ip", "pool"})
}

func (h *K8sResourceHandler) Namespaces(c *gin.Context) {
	h.list(c, `SELECT id,cluster_id,name,phase FROM k8s_namespaces`,
		[]filter{{"cluster_id", "cluster_id", true}}, "cluster_id,name", []string{"name"})
}

func (h *K8sResourceHandler) Workloads(c *gin.Context) {
	h.list(c, `SELECT id,cluster_id,namespace,kind,name,replicas_desired,replicas_ready,image,image_tag,status FROM k8s_workloads`,
		[]filter{{"cluster_id", "cluster_id", true}, {"namespace", "namespace", false}, {"kind", "kind", false}}, "namespace,name", []string{"name", "image", "namespace"})
}

func (h *K8sResourceHandler) Pods(c *gin.Context) {
	h.list(c, `SELECT id,cluster_id,namespace,name,node_name,workload,phase,cpu_req_m,mem_req_mi,cpu_lim_m,mem_lim_mi,restarts,pod_ip,start_time FROM k8s_pods`,
		[]filter{{"cluster_id", "cluster_id", true}, {"namespace", "namespace", false}, {"node_name", "node", false}}, "namespace,name", []string{"name", "pod_ip", "node_name", "workload"})
}

func (h *K8sResourceHandler) Services(c *gin.Context) {
	h.list(c, `SELECT id,cluster_id,namespace,name,type,cluster_ip,ports FROM k8s_services`,
		[]filter{{"cluster_id", "cluster_id", true}, {"namespace", "namespace", false}}, "namespace,name", []string{"name", "cluster_ip"})
}

func (h *K8sResourceHandler) Ingresses(c *gin.Context) {
	h.list(c, `SELECT id,cluster_id,namespace,name,hosts,tls,svc_names FROM k8s_ingresses`,
		[]filter{{"cluster_id", "cluster_id", true}, {"namespace", "namespace", false}}, "namespace,name", []string{"name", "hosts"})
}

func (h *K8sResourceHandler) Gateways(c *gin.Context) {
	h.list(c, `SELECT id,cluster_id,namespace,name,gateway_class,listeners,addresses FROM k8s_gateways`,
		[]filter{{"cluster_id", "cluster_id", true}, {"namespace", "namespace", false}}, "namespace,name", []string{"name", "gateway_class"})
}

func (h *K8sResourceHandler) HTTPRoutes(c *gin.Context) {
	h.list(c, `SELECT id,cluster_id,namespace,name,hostnames,parents,backends FROM k8s_httproutes`,
		[]filter{{"cluster_id", "cluster_id", true}, {"namespace", "namespace", false}}, "namespace,name", []string{"name", "hostnames"})
}

func (h *K8sResourceHandler) VirtualServices(c *gin.Context) {
	h.list(c, `SELECT id,cluster_id,namespace,name,hosts,gateways,backends FROM k8s_virtualservices`,
		[]filter{{"cluster_id", "cluster_id", true}, {"namespace", "namespace", false}}, "namespace,name", []string{"name", "hosts"})
}

func (h *K8sResourceHandler) PVCs(c *gin.Context) {
	h.list(c, `SELECT id,cluster_id,namespace,name,status,capacity,storage_class,volume_name FROM k8s_pvcs`,
		[]filter{{"cluster_id", "cluster_id", true}, {"namespace", "namespace", false}}, "namespace,name", []string{"name", "storage_class"})
}

func (h *K8sResourceHandler) HPAs(c *gin.Context) {
	h.list(c, `SELECT id,cluster_id,namespace,name,target_kind,target_name,min_replicas,max_replicas,current_replicas FROM k8s_hpas`,
		[]filter{{"cluster_id", "cluster_id", true}, {"namespace", "namespace", false}}, "namespace,name", []string{"name", "target_name"})
}

// NsProjects 列出某集群所有命名空间 + 已配的项目归属（未配的 project 为空，前端提醒）。
func (h *K8sResourceHandler) NsProjects(c *gin.Context) {
	cid := c.Query("cluster_id")
	if cid == "" {
		c.JSON(400, gin.H{"error": "cluster_id 必填"})
		return
	}
	rows, err := h.DB.Query(`SELECT n.name, COALESCE(m.project,'') AS project
		FROM k8s_namespaces n LEFT JOIN k8s_ns_project m ON m.cluster_id=n.cluster_id AND m.namespace=n.name
		WHERE n.cluster_id=? ORDER BY n.name`, cid)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out, err := scanRows(rows)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, out)
}

// SetNsProject upsert 一条命名空间→项目映射（project 传空=清除归属）。
func (h *K8sResourceHandler) SetNsProject(c *gin.Context) {
	var in struct {
		ClusterID int    `json:"cluster_id"`
		Namespace string `json:"namespace"`
		Project   string `json:"project"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.ClusterID == 0 || in.Namespace == "" {
		c.JSON(400, gin.H{"error": "cluster_id/namespace 必填"})
		return
	}
	_, err := h.DB.Exec(`INSERT INTO k8s_ns_project (cluster_id,namespace,project) VALUES (?,?,?)
		ON DUPLICATE KEY UPDATE project=VALUES(project)`, in.ClusterID, in.Namespace, in.Project)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	WriteAudit(h.DB, c, "set_k8s_ns_project", in.Namespace+"→"+in.Project)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *K8sResourceHandler) Changes(c *gin.Context) {
	h.list(c, `SELECT id,cluster_id,namespace,kind,name,field,old_value,new_value,source,changed_at FROM k8s_changes`,
		[]filter{{"cluster_id", "cluster_id", true}, {"namespace", "namespace", false}, {"kind", "kind", false}, {"name", "name", false}},
		"changed_at DESC", []string{"name", "new_value"})
}

type filter struct {
	col   string // 数据库列
	param string // query 参数名
	isInt bool
}

// list 通用列表：按 filters 拼 WHERE + q 关键词模糊(searchCols) + orderBy，扫描为 []map。
func (h *K8sResourceHandler) list(c *gin.Context, base string, filters []filter, orderBy string, searchCols []string) {
	where := []string{}
	args := []any{}
	for _, f := range filters {
		v := c.Query(f.param)
		if v == "" {
			continue
		}
		if f.isInt {
			iv, err := strconv.Atoi(v)
			if err != nil {
				continue
			}
			where = append(where, f.col+"=?")
			args = append(args, iv)
		} else {
			where = append(where, f.col+"=?")
			args = append(args, v)
		}
	}
	if q := strings.TrimSpace(c.Query("q")); q != "" && len(searchCols) > 0 {
		ors := []string{}
		for _, col := range searchCols {
			ors = append(ors, col+" LIKE ?")
			args = append(args, "%"+q+"%")
		}
		where = append(where, "("+strings.Join(ors, " OR ")+")")
	}
	sqlStr := base
	if len(where) > 0 {
		sqlStr += " WHERE " + strings.Join(where, " AND ")
	}
	if orderBy != "" {
		sqlStr += " ORDER BY " + orderBy
	}
	rows, err := h.DB.Query(sqlStr, args...)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out, err := scanRows(rows)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, out)
}

// scanRows 把结果集扫描为 []map[string]any，自动处理 NULL 与时间格式。
func scanRows(rows *sql.Rows) ([]map[string]any, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		m := map[string]any{}
		for i, col := range cols {
			m[col] = normVal(vals[i])
		}
		out = append(out, m)
	}
	return out, nil
}

func normVal(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case []byte:
		return string(t)
	case time.Time:
		return t.Format("2006-01-02 15:04:05")
	default:
		return t
	}
}
