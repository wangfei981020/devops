package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"ops-cmdb-backend/internal/httpx"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// DevOps 流水线（KubeSphere DevOps / 底层 Jenkins）只读查询。
//
// 排障链路：pipeline_runs 找出哪次构建失败 → pipeline_log 直接给出失败原因。
// 之所以要单独做成接口而不是让调用方自己拼 kapis 路径：
//  1. 两个能力分别在不同 API 版本上——运行记录在 v1alpha3 的 CRD，构建日志只在 v1alpha2，
//     且日志路径用的是 Jenkins 构建号(runs/50)而不是 CRD 名(pipelineruns/xxx-2xzsk)，
//     两者要靠 annotation 关联，不告诉调用方它基本不可能猜对。
//  2. 一次构建的日志实测 236KB/3200 行，整段塞进上下文会直接超限——必须先抽出错误段。
//
// Jenkins 的编译输出不进 pod stdout，Loki 也采不到，所以这是拿到构建失败原因的唯一通路。

const (
	// 运行记录在 v1alpha3 的 CRD 里。
	devopsRunsAPI = "/apis/devops.kubesphere.io/v1alpha3/namespaces/%s/pipelineruns"
	// 构建日志只有 v1alpha2 提供，且按 Jenkins 构建号寻址，末尾的斜杠不能省。
	devopsLogAPI = "/kapis/devops.kubesphere.io/v1alpha2/namespaces/%s/pipelines/%s/runs/%s/log/?start=0"
	// 阶段/步骤级状态，用来定位失败卡在哪一步。
	devopsNodeAPI = "/kapis/devops.kubesphere.io/v1alpha3/namespaces/%s/pipelineruns/%s/nodedetails"
)

func (h *ObsQueryHandler) RegisterDevOps(r *gin.RouterGroup) {
	r.GET("/devops/pipeline-runs", h.PipelineRuns) // cluster_id, namespace, pipeline?, only_failed?
	r.GET("/devops/projects", h.DevOpsProjects)
	r.GET("/devops/pipeline-log", h.PipelineLog) // cluster_id, namespace, pipeline, run, tail?, full?
}

// ksGet 打一次 ks-apiserver。
func (h *ObsQueryHandler) ksGet(cid int, path string) (int, string, error) {
	var env string
	_ = h.DB.QueryRow(`SELECT COALESCE(environment,'') FROM k8s_clusters WHERE id=?`, cid).Scan(&env)
	base, token, err := resolveEndpoint(h.DB, h.Cipher, "kubesphere", env, cid)
	if err != nil {
		return 0, "", err
	}
	return obsGet(strings.TrimRight(base, "/")+path, token, 30*time.Second)
}

// PipelineRuns 列某 DevOps 项目下的流水线运行记录（默认只看失败的）。
func (h *ObsQueryHandler) PipelineRuns(c *gin.Context) {
	cid, _ := strconv.Atoi(c.Query("cluster_id"))
	ns := c.Query("namespace")
	if cid == 0 || ns == "" {
		httpx.FailKeyWith(c, httpx.CodeBadRequest, "error.pipelineRunsRequired", nil,
			map[string]any{
				// 中文原句留给 MCP / 直接调 API 的人（他们读不到语言包）
				"error": "cluster_id/namespace 必填（namespace 是 DevOps 项目对应的命名空间，形如 test-test-devopsj2q22）",
			})
		return
	}
	// ⚠️ limit 不能开大。
	//
	//	每条 PipelineRun 都带着**完整的 Jenkinsfile**（spec.pipelineSpec，实测 ~5KB），
	//	而这一页只用得到 metadata.labels 和 status —— 那几 KB 全是白拉的。
	//	原来写的是 limit=500，响应约 2.5MB，超过 obsGet 的 1MB 上限被截断，
	//	最后报成「解析流水线运行记录失败」（真实原因是响应太大）。
	//
	//	KubeSphere 的 kapis 不支持字段裁剪，所以只能靠 limit 控制。
	//	100 条足够覆盖"最近失败了哪些"，而且响应还有 remainingItemCount，
	//	下面会把"还有多少条没看"如实报出来 —— 不能让人以为这就是全部。
	const runsLimit = 100
	code, body, err := h.ksGet(cid, fmt.Sprintf(devopsRunsAPI, ns)+fmt.Sprintf("?limit=%d", runsLimit))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	if code != 200 {
		// 🔴 不同状态码的成因和处置完全不同，不能共用一句话。
		// 曾经统一报「确认 namespace 是不是 DevOps 项目」，结果 403（令牌权限不足）
		// 被说成参数写错，用户去反复检查 namespace —— 而那个 namespace 是对的。
		// 把权限问题说成参数问题，比不给提示更糟：它把人指向确定错误的方向。
		c.JSON(http.StatusOK, gin.H{"ok": false, "status": code,
			"error": ksErrorHint(code, ns)})
		return
	}
	var resp struct {
		// Metadata.RemainingItemCount：这一页之外还有多少条。
		// ⚠️ 不接的话，limit 截断就变成静默的了 —— 而这一页的用途是
		// "最近哪些构建挂了"，看到的不是全部却以为是全部，会漏掉真问题
		Metadata struct {
			RemainingItemCount int `json:"remainingItemCount"`
		} `json:"metadata"`
		Items []struct {
			Metadata struct {
				Name        string            `json:"name"`
				Namespace   string            `json:"namespace"`
				Labels      map[string]string `json:"labels"`
				Annotations map[string]string `json:"annotations"`
			} `json:"metadata"`
			Status struct {
				Phase          string `json:"phase"`
				StartTime      string `json:"startTime"`
				CompletionTime string `json:"completionTime"`
			} `json:"status"`
		} `json:"items"`
	}
	if e := json.Unmarshal([]byte(body), &resp); e != nil {
		// ⚠️ 说清楚**收到的是什么**。只说"解析失败"，人无从判断是上游改了格式、
		// 返回了 HTML 错误页、还是响应被截断 —— 三者的处置完全不同。
		c.JSON(http.StatusOK, gin.H{"ok": false,
			"error": "解析流水线运行记录失败：" + e.Error(), "error_key": "error.parsePipelineRunsFailed",
			"got":      truncStr(body, 200),
			"length":   len(body),
			"hint_key": "error.pipelineRunsParseHint",
			"hint":     "若 got 看起来是半截 JSON，多半是响应过大被截断；若是 HTML，多半是认证跳转"})
		return
	}

	onlyFailed := c.Query("only_failed") != "0" // 默认只看失败的——排障场景没人关心成功的那几百条
	wantPipeline := c.Query("pipeline")
	out := []gin.H{}
	total, failed := 0, 0
	for _, it := range resp.Items {
		total++
		if it.Status.Phase == "Failed" {
			failed++
		}
		pl := it.Metadata.Labels["devops.kubesphere.io/pipeline"]
		if wantPipeline != "" && pl != wantPipeline {
			continue
		}
		if onlyFailed && it.Status.Phase != "Failed" {
			continue
		}
		out = append(out, gin.H{
			"pipeline":  pl,
			"run":       it.Metadata.Annotations["devops.kubesphere.io/jenkins-pipelinerun-id"], // 取日志要用这个号，不是 name
			"name":      it.Metadata.Name,
			"namespace": it.Metadata.Namespace,
			"phase":     it.Status.Phase,
			"start":     it.Status.StartTime,
			"end":       it.Status.CompletionTime,
			"creator":   it.Metadata.Annotations["devops.kubesphere.io/creator"],
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return fmt.Sprint(out[i]["start"]) > fmt.Sprint(out[j]["start"])
	})
	// 🔴 两处截断都必须说出来，否则「看到的就是全部」这个默认理解是错的。
	//
	//	  ① out[:50]                 本次筛出来的太多，只给前 50 条
	//	  ② metadata.remainingItemCount  这个命名空间下还有多少条压根没拉
	//
	//	这一页的用途是「最近哪些构建挂了」。少给了却不说，
	//	人会得出"就这几条失败"的结论然后收工 —— 而漏掉的那条可能正是要找的。
	matched := len(out)
	if len(out) > 50 {
		out = out[:50]
	}
	res := gin.H{"ok": true, "total": total, "failed": failed,
		"count": len(out), "items": out,
		// 界面上的人点行就能看日志，不需要（也执行不了）工具名；
		// 工具链另放 mcp_hint 给 AI（check-mcp-text-leak）
		"hint_key": "pipelines:clickRunForLog",
		"hint":     "点某次构建可以看它的日志；run 是 Jenkins 构建号",
		"mcp_hint": "用 pipeline + run 调 pipeline_log 看失败原因；run 是 Jenkins 构建号"}
	if matched > len(out) {
		res["truncated"] = fmt.Sprintf(
			"符合条件的共 %d 条，这里只返回按时间倒序的前 %d 条", matched, len(out))
	}
	if resp.Metadata.RemainingItemCount > 0 {
		// ⚠️ 这一条比上面那条更要紧：上面是"筛出来的没给全"，
		// 这一条是"**压根没拉全**" —— 没拉到的那部分连筛都没筛过
		res["not_all_fetched"] = fmt.Sprintf(
			"这个命名空间下还有 %d 条运行记录没有拉取（单次上限 %d 条）。"+
				"⚠️ 上面的 total/failed 只覆盖已拉取的这批，不是该命名空间的全量。"+
				"要查更早的构建，请指定 pipeline 参数缩小范围",
			resp.Metadata.RemainingItemCount, runsLimit)
	}
	c.JSON(http.StatusOK, res)
}

// PipelineLog 取一次构建的日志。默认不返回全文，只给失败定位所需的部分。
func (h *ObsQueryHandler) PipelineLog(c *gin.Context) {
	cid, _ := strconv.Atoi(c.Query("cluster_id"))
	ns, pl, run := c.Query("namespace"), c.Query("pipeline"), c.Query("run")
	if cid == 0 || ns == "" || pl == "" || run == "" {
		httpx.FailKeyWith(c, httpx.CodeBadRequest, "error.pipelineLogRequired", nil,
			map[string]any{
				"error":    "cluster_id/namespace/pipeline/run 必填（run 是 Jenkins 构建号）",
				"mcp_hint": "run 是 Jenkins 构建号，从 pipeline_runs 拿",
			})
		return
	}
	code, body, err := h.ksGet(cid, fmt.Sprintf(devopsLogAPI, url.PathEscape(ns), url.PathEscape(pl), url.PathEscape(run)))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	if code != 200 {
		c.JSON(http.StatusOK, gin.H{"ok": false, "status": code,
			"error_key": "error.pipelineLogFetchFailed",
			"error":     "取构建日志失败：确认 pipeline 名和 run（Jenkins 构建号）正确，且该次构建的日志未被 Jenkins 的保留策略清理"})
		return
	}

	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	out := gin.H{"ok": true, "namespace": ns, "pipeline": pl, "run": run,
		"total_lines": len(lines), "total_bytes": len(body)}

	if c.Query("full") == "1" {
		out["log"] = body
		c.JSON(http.StatusOK, out)
		return
	}

	tail := 60
	if t, e := strconv.Atoi(c.Query("tail")); e == nil && t > 0 && t <= 2000 {
		tail = t
	}
	errs := extractErrorLines(lines)
	out["errors"] = errs
	out["tail"] = strings.Join(lastN(lines, tail), "\n")
	if len(errs) == 0 {
		out["hint"] = "没匹配到错误关键字，日志尾部见 tail；要全文加 full=1"
	} else {
		out["hint"] = "errors 是从全文里抽出的报错行(含行号)；要看上下文用 full=1 取全文"
	}
	c.JSON(http.StatusOK, out)
}

// errLinePattern 构建失败时真正有信息量的行。
//
// 只收敛到明确表示失败的关键字——实测那份日志里含 "error" 的行有 660 行，
// 绝大多数是 errorCallback / failureErrorWithLog 这类栈帧，全抓回来等于没抽（真正的报错只有 6 行）。
//
// 刻意不加 (?i)：Jenkins/构建工具的失败关键字本来就是全大写(ERROR:/FAILURE/FAILED)，
// 一旦忽略大小写，栈帧里的 failureErrorWithLog 就会命中 FAILURE。
// 只有 exit status/code 这类写法不统一的才单独放宽。
// 全大写关键字之外，还要单独补构建工具的小写报错——它们才是「为什么失败」。
// 实测：一次 vite 构建失败，全大写那组只能抽出 "ERR_PNPM_RECURSIVE_RUN_FIRST_FAIL"、
// "Exit status 1"、"ERROR: script returned exit code 1"，看完只知道 vite build 挂了，
// 而真正的根因 "error during build:" + "[vite:vue] Unexpected token, expected \",\" (38:2)"
// 全是小写开头、一条都抓不到。
//
// 这些模式在三份真实构建日志上验过：零栈帧噪音。新增模式必须同样用真实日志复测——
// naive 地匹配 "error" 会抓回 660 行（几乎全是 errorCallback/failureErrorWithLog 栈帧），
// 正确收敛后应当只有个位数。
var buildToolErrPattern = regexp.MustCompile(
	`error during build|Unexpected token|SyntaxError|Cannot find module|Module not found|` +
		`Type error:|Parse error|Compilation failed|ENOENT: no such file`)

var errLinePattern = regexp.MustCompile(
	`(^|\s)(ERROR:|ERR_[A-Z][A-Z_]*|\bFAILURE\b|\bFAILED\b|npm ERR!|error TS\d+|fatal:|panic:|Traceback \(most recent call last\))` +
		`|(?i)\bexit (status|code) \d+`)

// ansiPattern 终端颜色码。构建日志里混着 \x1b[31m 之类的转义，
// 不清掉会让抽出来的行难读，也会干扰后续做字符串匹配。
var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// stackFramePattern 栈帧行。它们跟在真正的报错后面，本身不含根因信息，
// 收集上下文时要跳过，否则 6 行上下文会被栈帧占满。
var stackFramePattern = regexp.MustCompile(`^\s*at\s+|^\s*File "|^\s{4,}\.\.\.`)

func isErrLine(s string) bool {
	return errLinePattern.MatchString(s) || buildToolErrPattern.MatchString(s)
}

// fatalErrPattern 决定「构建为什么失败」的那几行。
//
// 它们几乎总在日志末尾：编译器先吐一大堆类型/lint 错误，最后才是构建工具宣布失败。
// 区分出来是因为按行号从前往后截断会把它们全部挤掉——实测某次构建：
// 抽到的 81 条全是 L333~L698 的 TS 类型错误，真正致命的 error during build 在 L3148，
// 一条都没进来（CMDB-007）。
var fatalErrPattern = regexp.MustCompile(
	`(?i)error during build|command failed|exit (status|code) [1-9]|` +
		`ELIFECYCLE|npm ERR!|ERR_PNPM|build failed|compilation failed|` +
		`finished: failure|script returned exit code|process exited with|` +
		`cannot find module|module not found|out of memory|killed`)

// maxErrLines 返回给调用方的报错行上限。一次构建动辄 3000+ 行、236KB，
// 整段返回会撑爆上下文——这是这个接口存在的主要理由。
const maxErrLines = 80

// extractErrorLines 抽出报错行并带上原始行号（方便让人回原文定位）。
//
// 命中后还要带几行上下文：像 "error during build:" 这种行只说明「构建失败了」，
// 具体是哪个文件哪一行在它**后面**几行。只返回命中行本身仍然定位不到问题。
//
// 超过上限时的取舍（CMDB-007 的修复）：
//  1. 致命行**全部保留**——它们才回答「为什么失败」，而且数量本来就少
//  2. 普通行**从后往前取**——同一个文件的类型错误重复几百条，越靠后越接近失败点
//  3. 丢了多少、丢的是哪一段，明确写出来，不让人以为这就是全部
func extractErrorLines(lines []string) []gin.H {
	type hit struct {
		item  gin.H
		fatal bool
	}
	hits := make([]hit, 0, 128)
	for i, l := range lines {
		s := strings.TrimSpace(ansiPattern.ReplaceAllString(l, ""))
		if s == "" || !isErrLine(s) {
			continue
		}
		item := gin.H{"line": i + 1, "text": truncate(s, 500)}
		if ctx := collectContext(lines, i); len(ctx) > 0 {
			item["context"] = ctx
		}
		f := fatalErrPattern.MatchString(s)
		if f {
			item["fatal"] = true
		}
		hits = append(hits, hit{item, f})
	}
	if len(hits) <= maxErrLines {
		out := make([]gin.H, 0, len(hits))
		for _, h := range hits {
			out = append(out, h.item)
		}
		return out
	}

	// 超限：先收致命行（全留），再用剩余配额从后往前收普通行
	kept := make([]gin.H, 0, maxErrLines)
	takenIdx := map[int]bool{}
	for i, h := range hits {
		if h.fatal {
			kept = append(kept, h.item)
			takenIdx[i] = true
		}
	}
	fatalCount := len(kept)
	quota := maxErrLines - fatalCount
	normalTaken := 0
	for i := len(hits) - 1; i >= 0 && normalTaken < quota; i-- {
		if takenIdx[i] {
			continue
		}
		kept = append(kept, hits[i].item)
		takenIdx[i] = true
		normalTaken++
	}
	// 按原始行号排回去，读起来才和日志一致
	sort.Slice(kept, func(a, b int) bool {
		la, _ := kept[a]["line"].(int)
		lb, _ := kept[b]["line"].(int)
		return la < lb
	})
	dropped := len(hits) - len(kept)
	if dropped > 0 {
		kept = append(kept, gin.H{"line": 0, "text": fmt.Sprintf(
			"（共 %d 条报错行，已保留全部 %d 条致命错误 + 最靠近失败点的 %d 条，省略中间 %d 条重复报错；用 full=1 看全文）",
			len(hits), fatalCount, normalTaken, dropped)})
	}
	return kept
}

// collectContext 取命中行之后最多 6 行非栈帧内容，遇到下一个报错行即停。
// 上限 6 行是权衡：够带出文件名、行号和代码片段，又不至于把整段日志搬回来。
func collectContext(lines []string, from int) []string {
	const maxCtx = 6
	ctx := []string{}
	for j := from + 1; j < len(lines) && len(ctx) < maxCtx; j++ {
		s := strings.TrimSpace(ansiPattern.ReplaceAllString(lines[j], ""))
		if s == "" || stackFramePattern.MatchString(s) {
			continue
		}
		if isErrLine(s) {
			break // 下一个报错自成一条，不重复收进上下文
		}
		ctx = append(ctx, truncate(s, 300))
	}
	return ctx
}

func lastN(lines []string, n int) []string {
	if len(lines) <= n {
		return lines
	}
	return lines[len(lines)-n:]
}

// ksErrorHint 把 KubeSphere 返回的状态码翻译成「该去查什么」。
//
// ⚠️ 每一档都要指向**不同**的排查方向。共用一句话的代价见上面调用处的注释。
func ksErrorHint(code int, ns string) string {
	switch code {
	case 401:
		return "KubeSphere 令牌无效或已过期（HTTP 401）。去「管理 → 观测端点」" +
			"更新 kubesphere 数据源的令牌"
	case 403:
		return "KubeSphere 令牌权限不足（HTTP 403），读不到该 DevOps 项目的流水线。" +
			"⚠️ 这不是 namespace 写错——能连上但没权限。" +
			"该令牌需要对 DevOps 项目有读权限（platform-regular 之类的只读角色不够，" +
			"要在对应企业空间/DevOps 项目里授予查看权限）。" +
			"注意 /kapis/version 是匿名可调的，它通了不代表权限够"
	case 404:
		return "找不到该 DevOps 项目（HTTP 404）：确认 namespace 是 DevOps 项目对应的" +
			"命名空间（形如 g66-test-devopsj2q22），不是普通业务命名空间。" +
			"⚠️ 也可能是 KubeSphere 4.x 的 kapis 路径与 3.x 不同导致的路径不存在"
	default:
		return fmt.Sprintf("取流水线运行记录失败（HTTP %d，namespace=%s）。"+
			"先用 kubesphere_fetch 调 /kapis/version 确认连通性", code, ns)
	}
}

// DevOpsProjects 列出 DevOps 项目命名空间。
//
// 🔴 为什么必须有：pipeline_runs 的 namespace 是**必填**，
// 但此前没有任何工具能告诉你有哪些 DevOps 项目 —— 只能把全量命名空间拉下来
// 按名字猜（`*-devops*`）。这与 health_detail 补上的是同一类缺口：
// **知道有问题，却拿不到对象名**，「发现」和「分析」之间没有桥。
//
// 数据来自已采集的命名空间表，不额外打 KubeSphere —— 采集本来就有这份数据，
// 再去问一次既慢又多一个失败点。
func (h *ObsQueryHandler) DevOpsProjects(c *gin.Context) {
	cid, _ := strconv.Atoi(c.Query("cluster_id"))
	if cid <= 0 {
		httpx.Required(c, "cluster_id")
		return
	}
	rows, err := h.DB.Query(`SELECT n.name, n.phase,
		(SELECT COUNT(*) FROM k8s_configmaps c
		  WHERE c.cluster_id=n.cluster_id AND c.namespace=n.name) AS cm
		FROM k8s_namespaces n
		WHERE n.cluster_id=? AND (n.name LIKE '%-devops%' OR n.name LIKE '%devops')
		ORDER BY n.name`, cid)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var name, phase string
		var cm int
		if rows.Scan(&name, &phase, &cm) == nil {
			out = append(out, gin.H{"namespace": name, "phase": phase, "configmaps": cm})
		}
	}
	res := gin.H{"ok": true, "count": len(out), "projects": out,
		"hint_key": "pipelines:pickProjectForRuns",
		"hint":     "选一个项目查看它的构建记录，再点具体某次构建看日志",
		"mcp_hint": "拿 namespace 去调 pipeline_runs 看失败的构建，再用 pipeline_log 取根因"}
	if len(out) == 0 {
		// ⚠️ 空要区分「这个集群没装 DevOps」和「命名空间还没采到」
		var nsTotal int
		_ = h.DB.QueryRow(`SELECT COUNT(*) FROM k8s_namespaces WHERE cluster_id=?`, cid).Scan(&nsTotal)
		if nsTotal == 0 {
			res["hint"] = "该集群的命名空间还没采集到，无法判断有没有 DevOps 项目 —— 先看 data_freshness"
		} else {
			res["hint"] = "该集群没有 DevOps 项目命名空间（已采到 " + strconv.Itoa(nsTotal) +
				" 个命名空间，其中没有 *-devops* 形态的）。可能是没装 KubeSphere DevOps"
		}
	}
	c.JSON(http.StatusOK, res)
}
