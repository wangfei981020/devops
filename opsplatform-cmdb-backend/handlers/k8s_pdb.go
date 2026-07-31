// PodDisruptionBudget 只读查询。
//
// 单独成文件而不是并进 k8s_resources.go：PDB 在 CMDB 里的用途很集中——
// 只服务于「这个节点能不能被 drain 走 / 这次升级会不会卡住」，
// 和通用资源列表的关注点不同，放一起会让两边都变模糊。
package handlers

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

type K8sPDBHandler struct {
	DB *sql.DB
}

func NewK8sPDBHandler(db *sql.DB) *K8sPDBHandler {
	return &K8sPDBHandler{DB: db}
}

func (h *K8sPDBHandler) Register(r *gin.RouterGroup) {
	r.GET("/k8s/pdbs", h.List)
}

type pdbOut struct {
	Namespace          string `json:"namespace"`
	Name               string `json:"name"`
	MinAvailable       string `json:"min_available"`
	MaxUnavailable     string `json:"max_unavailable"`
	Selector           string `json:"selector"`
	CurrentHealthy     int    `json:"current_healthy"`
	DesiredHealthy     int    `json:"desired_healthy"`
	ExpectedPods       int    `json:"expected_pods"`
	DisruptionsAllowed int    `json:"disruptions_allowed"`
	Blocking           bool   `json:"blocking"`  // 余量为 0：此刻驱逐任何一个 Pod 都会被拒
	RiskNote           string `json:"risk_note"` // 为什么卡住 / 卡住会怎样，直接给结论不让人自己推
}

// List 列 PDB。blocking=1 只看余量为 0 的（升级前最该看的那批）。
//
// 采集缺失与「没有 PDB」必须分得开：前者是「不知道会不会卡」，后者是「确定不会卡」。
// 所以返回体里带 collected 字段，前端据此显示「未知」还是「无风险」。
func (h *K8sPDBHandler) List(c *gin.Context) {
	cid, _ := strconv.Atoi(c.Query("cluster_id"))
	if cid == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cluster_id 必填"})
		return
	}

	q := `SELECT namespace,name,min_available,max_unavailable,selector,
	             current_healthy,desired_healthy,expected_pods,disruptions_allowed
	        FROM k8s_pdbs WHERE cluster_id=?`
	args := []any{cid}
	if ns := c.Query("namespace"); ns != "" {
		q += ` AND namespace=?`
		args = append(args, ns)
	}
	if c.Query("blocking") == "1" {
		q += ` AND disruptions_allowed=0`
	}
	q += ` ORDER BY disruptions_allowed ASC, namespace, name`

	rows, err := h.DB.Query(q, args...)
	if err != nil {
		// 查询失败绝不能返回空列表——空列表会被读成「没有阻塞风险」，
		// 而实际是「不知道有没有」。这正是 CMDB-013 那类失效模式。
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询 PDB 失败: " + err.Error()})
		return
	}
	defer rows.Close()

	out := []pdbOut{}
	blocking := 0
	for rows.Next() {
		var p pdbOut
		if rows.Scan(&p.Namespace, &p.Name, &p.MinAvailable, &p.MaxUnavailable, &p.Selector,
			&p.CurrentHealthy, &p.DesiredHealthy, &p.ExpectedPods, &p.DisruptionsAllowed) != nil {
			continue
		}
		p.Blocking = p.DisruptionsAllowed == 0
		p.RiskNote = pdbRiskNote(p)
		if p.Blocking {
			blocking++
		}
		out = append(out, p)
	}

	c.JSON(http.StatusOK, gin.H{
		"items":     out,
		"total":     len(out),
		"blocking":  blocking,
		"collected": pdbCollected(h.DB, cid),
	})
}

// pdbCollected 该集群到底采没采到 PDB。
// 用 k8s_sync_state 而不是「表里有没有行」来判断——集群本来就一个 PDB 都没配时，
// 两者都是 0 行，但前者是「采过，确实没有」，后者可能是「压根没采」。
func pdbCollected(db *sql.DB, cid int) bool {
	return resourceCollected(db, cid, "pdbs")
}

// resourceCollected 某类资源是否成功采集过。
//
// 用 k8s_sync_state 而不是「表里有没有行」：集群本来就没有这类资源时两者都是 0 行，
// 但「采过，确实没有」能拿来下结论，「压根没采」不能。这个区分在升级预案里尤其要紧——
// 把「没采到」当成「没有风险」正是最危险的那种误报。
func resourceCollected(db *sql.DB, cid int, resource string) bool {
	var ok int
	err := db.QueryRow(`SELECT ok FROM k8s_sync_state WHERE cluster_id=? AND resource=?`,
		cid, resource).Scan(&ok)
	return err == nil && ok == 1
}

// pdbRiskNote 把数字翻译成结论。
//
// 光给 disruptionsAllowed=0 没用——看的人还要自己回想 PDB 语义才知道意味着什么。
// 升级窗口是深夜排的，这时候最不该让人做推理。
func pdbRiskNote(p pdbOut) string {
	if p.DisruptionsAllowed > 0 {
		return ""
	}
	switch {
	case p.ExpectedPods == 0:
		return "选中 0 个 Pod：PDB 的 selector 可能没匹配上任何工作负载（配错了或工作负载已删）。不会阻塞 drain，但这个 PDB 也没在保护任何东西"
	case p.CurrentHealthy < p.DesiredHealthy:
		return "健康副本不足（" + strconv.Itoa(p.CurrentHealthy) + "/" + strconv.Itoa(p.DesiredHealthy) +
			"）：有副本正在重启或起不来。drain 会被一直拒绝，直到副本恢复。升级前必须先把副本修好"
	default:
		return "余量为 0（健康 " + strconv.Itoa(p.CurrentHealthy) + "，最低要求 " + strconv.Itoa(p.DesiredHealthy) +
			"）：副本数恰好卡在下限，驱逐任何一个都会破坏约束。节点 drain 会被拒到超时才强杀，" +
			"单节点可能因此多花一小时。升级前应临时放宽此 PDB 或先扩容副本"
	}
}
