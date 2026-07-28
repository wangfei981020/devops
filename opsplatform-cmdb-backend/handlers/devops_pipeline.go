package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
	r.GET("/devops/pipeline-log", h.PipelineLog)   // cluster_id, namespace, pipeline, run, tail?, full?
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
		c.JSON(400, gin.H{"error": "cluster_id/namespace 必填（namespace 是 DevOps 项目对应的命名空间，形如 g66-test-devopsj2q22）"})
		return
	}
	code, body, err := h.ksGet(cid, fmt.Sprintf(devopsRunsAPI, ns)+"?limit=500")
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	if code != 200 {
		c.JSON(http.StatusOK, gin.H{"ok": false, "status": code,
			"error": "取流水线运行记录失败，确认 namespace 是 DevOps 项目的命名空间（带 -devops 后缀）"})
		return
	}
	var resp struct {
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
	if json.Unmarshal([]byte(body), &resp) != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "解析流水线运行记录失败"})
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
	if len(out) > 50 {
		out = out[:50]
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "total": total, "failed": failed,
		"count": len(out), "items": out,
		"hint": "用 pipeline + run 调 pipeline_log 看失败原因；run 是 Jenkins 构建号"})
}

// PipelineLog 取一次构建的日志。默认不返回全文，只给失败定位所需的部分。
func (h *ObsQueryHandler) PipelineLog(c *gin.Context) {
	cid, _ := strconv.Atoi(c.Query("cluster_id"))
	ns, pl, run := c.Query("namespace"), c.Query("pipeline"), c.Query("run")
	if cid == 0 || ns == "" || pl == "" || run == "" {
		c.JSON(400, gin.H{"error": "cluster_id/namespace/pipeline/run 必填（run 是 Jenkins 构建号，从 pipeline_runs 拿）"})
		return
	}
	code, body, err := h.ksGet(cid, fmt.Sprintf(devopsLogAPI, url.PathEscape(ns), url.PathEscape(pl), url.PathEscape(run)))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	if code != 200 {
		c.JSON(http.StatusOK, gin.H{"ok": false, "status": code,
			"error": "取构建日志失败：确认 pipeline 名和 run（Jenkins 构建号）正确，且该次构建的日志未被 Jenkins 的保留策略清理"})
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

// extractErrorLines 抽出报错行并带上原始行号（方便让人回原文定位）。
// 一次构建动辄 3000+ 行、236KB，整段返回会直接撑爆上下文——这是这个接口存在的主要理由。
//
// 命中后还要带几行上下文：像 "error during build:" 这种行只说明「构建失败了」，
// 具体是哪个文件哪一行在它**后面**几行。只返回命中行本身仍然定位不到问题。
func extractErrorLines(lines []string) []gin.H {
	out := []gin.H{}
	for i, l := range lines {
		s := strings.TrimSpace(ansiPattern.ReplaceAllString(l, ""))
		if s == "" || !isErrLine(s) {
			continue
		}
		item := gin.H{"line": i + 1, "text": truncate(s, 500)}
		if ctx := collectContext(lines, i); len(ctx) > 0 {
			item["context"] = ctx
		}
		out = append(out, item)
		if len(out) >= 80 { // 够定位就行，再多是噪音
			out = append(out, gin.H{"line": 0, "text": "（报错行过多，仅列前 80 条，用 full=1 看全文）"})
			break
		}
	}
	return out
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
