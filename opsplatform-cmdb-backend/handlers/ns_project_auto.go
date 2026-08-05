package handlers

import (
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/logx"
)

// 命名空间 → 业务项目的自动归属。
//
//	## 为什么需要
//
//	成本页 92% 的开销显示"未分配"——不是算错了，是 k8s_ns_project 这张
//	映射表基本没人填。而没有归属的成本数字是**没法用来做决策**的：
//	看到一个总额，却不知道该找谁谈优化。
//
//	命名空间的命名其实很有规律（生产 46 个里一大片是 `g32-*`），
//	完全可以自动匹配出大部分，剩下的少数再人工处理。
//
//	## 三条不能破的规矩
//
//	1. **只匹配已存在的项目**。凭空造项目名会让项目列表变成垃圾场，
//	   而且那些名字没人认领，等于换了一种"未分配"。
//	2. **只填空的，绝不覆盖**人工设过的映射。人工配置永远优先——
//	   自动规则再准也只是猜，把人的决定改掉是最让人不敢用的行为。
//	3. **平台组件不自动归**。kube-system/istio-system 这些不属于任何业务项目，
//	   硬塞进去会污染成本口径（业务方看到自己名下多了一坨监控开销）。
//	   它们在预览里单独列出来，由人决定要不要建一个"平台"项目收着。

// nsAutoMatch 一条匹配结果。
type nsAutoMatch struct {
	Namespace string `json:"namespace"`
	Project   string `json:"project"`
	// Rule 命中哪条规则：exact / prefix / platform / none
	Rule string `json:"rule"`
	// Reason 人话解释，预览时让人能判断这条对不对
	Reason string `json:"reason"`
	// Current 当前已有的归属；非空表示这条**不会被动**
	Current string `json:"current,omitempty"`
}

// platformNamespaces 平台/系统命名空间——不属于任何业务项目。
//
//	前缀匹配的那几个（cattle-/gke-managed-/kube-）是发行版自带的，
//	名字固定；其余是我们自己部署的平台组件。
var platformNsExact = map[string]bool{
	"default": true, "kube-system": true, "kube-public": true, "kube-node-lease": true,
	"istio-system": true, "monitoring": true, "logging": true, "argocd": true, "argocd2": true,
	"falco": true, "local": true, "tmp": true, "devops": true, "cert-manager": true,
	"ingress-nginx": true, "velero": true, "kubesphere-system": true,
}

var platformNsPrefix = []string{"cattle-", "gke-managed-", "kube-", "gmp-", "kubesphere-", "mxcwpp-"}

func isPlatformNamespace(ns string) bool {
	if platformNsExact[ns] {
		return true
	}
	for _, p := range platformNsPrefix {
		if strings.HasPrefix(ns, p) {
			return true
		}
	}
	return false
}

// matchNsProject 给一个命名空间挑项目。projects 是已存在的项目名。
//
//	匹配顺序：精确 > 最长前缀。前缀必须以分隔符结尾（`g32-`），
//	否则 `g32` 会误命中 `g32x-foo` 这种毫不相干的命名空间。
//	长前缀优先：同时存在项目 `g32` 和 `g32-bi` 时，`g32-bi-etl` 该归后者。
func matchNsProject(ns string, projects []string) (project, rule, reason string) {
	lower := strings.ToLower(ns)

	for _, p := range projects {
		if strings.EqualFold(ns, p) {
			return p, "exact", "命名空间名和项目名完全一致"
		}
	}

	best, bestLen := "", 0
	for _, p := range projects {
		pre := strings.ToLower(p) + "-"
		if strings.HasPrefix(lower, pre) && len(pre) > bestLen {
			best, bestLen = p, len(pre)
		}
	}
	if best != "" {
		return best, "prefix", "命名空间以「" + best + "-」开头"
	}

	if isPlatformNamespace(ns) {
		return "", "platform", "平台/系统组件，不属于任何业务项目——硬归到业务项目会污染成本口径"
	}
	return "", "none", "没有同名或同前缀的项目，需要人工判断"
}

// AutoNsProjects POST /api/k8s/ns-projects/auto?cluster_id=&dry_run=1
//
//	dry_run=1（默认）只返回预览，不写库。**批量写映射会直接改变成本报表的
//	归属结果**，让人先看清楚再落是必须的——和批量续费同一个道理。
func (h *K8sResourceHandler) AutoNsProjects(c *gin.Context) {
	cid, ok := requireCluster(c, h.DB)
	if !ok {
		return
	}
	dry := c.Query("dry_run") != "0" // 默认预览

	var projects []string
	if rows, err := h.DB.Query(`SELECT name FROM projects ORDER BY name`); err == nil {
		for rows.Next() {
			var n string
			if rows.Scan(&n) == nil && strings.TrimSpace(n) != "" {
				projects = append(projects, n)
			}
		}
		rows.Close()
	}
	if len(projects) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"error": "还没有任何项目，无法自动归属",
			"hint":  "先到「基础配置 → 项目」建好项目，再回来自动匹配",
		})
		return
	}
	// 长的排前面，让最长前缀优先命中
	sort.Slice(projects, func(i, j int) bool { return len(projects[i]) > len(projects[j]) })

	rows, err := h.DB.Query(`SELECT n.name, COALESCE(m.project,'')
		FROM k8s_namespaces n LEFT JOIN k8s_ns_project m ON m.cluster_id=n.cluster_id AND m.namespace=n.name
		WHERE n.cluster_id=? ORDER BY n.name`, cid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	out := []nsAutoMatch{}
	var toWrite []nsAutoMatch
	stat := map[string]int{}
	for rows.Next() {
		var ns, cur string
		if rows.Scan(&ns, &cur) != nil {
			continue
		}
		m := nsAutoMatch{Namespace: ns, Current: cur}
		if cur != "" {
			// 已有归属 —— 一律不动。人工配置永远优先。
			m.Rule, m.Reason = "keep", "已有归属，自动匹配不会覆盖"
			stat["keep"]++
			out = append(out, m)
			continue
		}
		m.Project, m.Rule, m.Reason = matchNsProject(ns, projects)
		stat[m.Rule]++
		if m.Project != "" {
			toWrite = append(toWrite, m)
		}
		out = append(out, m)
	}

	if !dry {
		n := 0
		for _, m := range toWrite {
			// 条件写：只在仍然没有归属时插入，避免预览和执行之间被人改过
			if _, e := h.DB.Exec(`INSERT INTO k8s_ns_project (cluster_id,namespace,project)
				VALUES (?,?,?) ON DUPLICATE KEY UPDATE project=IF(project='' OR project IS NULL, VALUES(project), project)`,
				cid, m.Namespace, m.Project); e == nil {
				n++
			}
		}
		logx.Line("ns_project", "自动归属 cluster="+itoa(cid)+" 写入 "+itoa(n)+" 条")
		SetAuditTarget(c, "集群 "+itoa(cid)+" 自动归属 "+itoa(n)+" 个命名空间")
		c.JSON(http.StatusOK, gin.H{"applied": n, "items": out, "stat": stat,
			"msg": "已归属 " + itoa(n) + " 个命名空间；平台组件和无法判断的仍为空，需要人工处理"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"dry_run": true, "items": out, "stat": stat,
		"will_apply": len(toWrite), "projects": projects})
}
