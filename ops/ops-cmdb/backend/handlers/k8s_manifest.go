package handlers

import (
	"context"
	"fmt"
	"net/http"
	"ops-cmdb-backend/internal/httpx"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"

	"ops-cmdb-backend/logx"
)

// 为什么需要 get_manifest：
//
// CMDB 的采集只落"列"（镜像/副本/request/limit/重启次数），不落 spec。结果是诊断能报出
// "Unhealthy"，却答不出探针配的是哪个路径、超时多少、initialDelay 够不够；能报出
// "FailedPreStopHook"，却看不到 preStop 写了什么。这类问题占真实排障的一大半，
// 而在不登录服务器的前提下，之前完全没有替代手段——这个工具就是 `kubectl get -o yaml` 的只读等价物。
//
// 安全边界：Secret 一律拒绝（内容永不经过本进程）；其余对象返回前统一脱敏，
// 并记审计。脱敏只动"值"，不动引用名（secretName/secretKeyRef 要留着才能排障）。

const redactedMark = "***REDACTED***"

// manifestKind 描述一种可取的资源。
type manifestKind struct {
	gvr           schema.GroupVersionResource
	clusterScoped bool
	// fallback 用于同一资源存在多个 API 版本的情况（Istio 从 v1beta1 迁到 v1，两版都可能在用）。
	fallback []schema.GroupVersionResource
}

// manifestKinds 是支持的资源表，key 为小写 kind。
//
// 为什么用固定表而不是 discovery + RESTMapper：RESTMapper 每次要拉全量 API 资源列表
// （GKE 上几百个 CRD，一次几百毫秒），而排障真正要看的类型就这些。表里少什么加什么，
// 比动态发现更快也更可控。Gateway 这种两套 API 同名的，用 api_group 参数区分。
var manifestKinds = map[string]manifestKind{
	// core
	"pod":                   {gvr: schema.GroupVersionResource{Version: "v1", Resource: "pods"}},
	"service":               {gvr: schema.GroupVersionResource{Version: "v1", Resource: "services"}},
	"configmap":             {gvr: schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}},
	"endpoints":             {gvr: schema.GroupVersionResource{Version: "v1", Resource: "endpoints"}},
	"persistentvolumeclaim": {gvr: schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}},
	"pvc":                   {gvr: schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}},
	"serviceaccount":        {gvr: schema.GroupVersionResource{Version: "v1", Resource: "serviceaccounts"}},
	"resourcequota":         {gvr: schema.GroupVersionResource{Version: "v1", Resource: "resourcequotas"}},
	"limitrange":            {gvr: schema.GroupVersionResource{Version: "v1", Resource: "limitranges"}},
	"node":                  {gvr: schema.GroupVersionResource{Version: "v1", Resource: "nodes"}, clusterScoped: true},
	"namespace":             {gvr: schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}, clusterScoped: true},
	"persistentvolume":      {gvr: schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumes"}, clusterScoped: true},
	"pv":                    {gvr: schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumes"}, clusterScoped: true},
	// apps / batch / autoscaling
	"deployment":  {gvr: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}},
	"statefulset": {gvr: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}},
	"daemonset":   {gvr: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}},
	"replicaset":  {gvr: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "replicasets"}},
	"job":         {gvr: schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}},
	"cronjob":     {gvr: schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "cronjobs"}},
	"hpa":         {gvr: schema.GroupVersionResource{Group: "autoscaling", Version: "v2", Resource: "horizontalpodautoscalers"}},
	"horizontalpodautoscaler": {
		gvr:      schema.GroupVersionResource{Group: "autoscaling", Version: "v2", Resource: "horizontalpodautoscalers"},
		fallback: []schema.GroupVersionResource{{Group: "autoscaling", Version: "v1", Resource: "horizontalpodautoscalers"}},
	},
	// networking / policy
	"ingress":       {gvr: schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}},
	"networkpolicy": {gvr: schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies"}},
	"poddisruptionbudget": {
		gvr: schema.GroupVersionResource{Group: "policy", Version: "v1", Resource: "poddisruptionbudgets"},
	},
	"pdb":          {gvr: schema.GroupVersionResource{Group: "policy", Version: "v1", Resource: "poddisruptionbudgets"}},
	"storageclass": {gvr: schema.GroupVersionResource{Group: "storage.k8s.io", Version: "v1", Resource: "storageclasses"}, clusterScoped: true},
	// Istio：UAT 的主力入口，144 个 VS 全靠它
	"virtualservice": {
		gvr:      schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1", Resource: "virtualservices"},
		fallback: []schema.GroupVersionResource{{Group: "networking.istio.io", Version: "v1beta1", Resource: "virtualservices"}},
	},
	"destinationrule": {
		gvr:      schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1", Resource: "destinationrules"},
		fallback: []schema.GroupVersionResource{{Group: "networking.istio.io", Version: "v1beta1", Resource: "destinationrules"}},
	},
	"serviceentry": {
		gvr:      schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1", Resource: "serviceentries"},
		fallback: []schema.GroupVersionResource{{Group: "networking.istio.io", Version: "v1beta1", Resource: "serviceentries"}},
	},
	"sidecar": {
		gvr:      schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1", Resource: "sidecars"},
		fallback: []schema.GroupVersionResource{{Group: "networking.istio.io", Version: "v1beta1", Resource: "sidecars"}},
	},
	"envoyfilter": {gvr: schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1alpha3", Resource: "envoyfilters"}},
	"peerauthentication": {
		gvr:      schema.GroupVersionResource{Group: "security.istio.io", Version: "v1", Resource: "peerauthentications"},
		fallback: []schema.GroupVersionResource{{Group: "security.istio.io", Version: "v1beta1", Resource: "peerauthentications"}},
	},
	"authorizationpolicy": {
		gvr:      schema.GroupVersionResource{Group: "security.istio.io", Version: "v1", Resource: "authorizationpolicies"},
		fallback: []schema.GroupVersionResource{{Group: "security.istio.io", Version: "v1beta1", Resource: "authorizationpolicies"}},
	},
	// Gateway API
	"httproute": {gvr: schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes"}},
	// cert-manager：istio-system 的 PresentError 刷了 7288 次，卡在哪一步只能看 Challenge
	"certificate":        {gvr: schema.GroupVersionResource{Group: "cert-manager.io", Version: "v1", Resource: "certificates"}},
	"certificaterequest": {gvr: schema.GroupVersionResource{Group: "cert-manager.io", Version: "v1", Resource: "certificaterequests"}},
	"issuer":             {gvr: schema.GroupVersionResource{Group: "cert-manager.io", Version: "v1", Resource: "issuers"}},
	"clusterissuer":      {gvr: schema.GroupVersionResource{Group: "cert-manager.io", Version: "v1", Resource: "clusterissuers"}, clusterScoped: true},
	"order":              {gvr: schema.GroupVersionResource{Group: "acme.cert-manager.io", Version: "v1", Resource: "orders"}},
	"challenge":          {gvr: schema.GroupVersionResource{Group: "acme.cert-manager.io", Version: "v1", Resource: "challenges"}},
	// ArgoCD
	"application": {gvr: schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "applications"}},
}

// gatewayKinds：Gateway 在 Istio 和 Gateway API 里同名，靠 api_group 选。默认 Istio（UAT 用的是它）。
var gatewayKinds = map[string]manifestKind{
	"networking.istio.io": {
		gvr:      schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1", Resource: "gateways"},
		fallback: []schema.GroupVersionResource{{Group: "networking.istio.io", Version: "v1beta1", Resource: "gateways"}},
	},
	"gateway.networking.k8s.io": {
		gvr: schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gateways"},
	},
}

// sensitiveValueKeys 命中即把该键的字符串值替换掉。
// 只列"键名本身就代表一个凭据值"的词；引用型键（secretName/secretKeyRef/...）由 isReferenceKey 排除。
var sensitiveValueKeys = []string{
	"password", "passwd", "token", "apikey", "credential", "privatekey",
	"secret", "accesskey", "dsn", "keystore", "passphrase",
}

// keyNormalizer 去掉键名里的分隔符。
//
// 必须归一化：敏感词表里写的是 apikey/accesskey 这种连写，而真实环境变量几乎都是
// API_KEY / ACCESS-KEY 这种带分隔符的形式。不归一化就会漏脱——单测里 API_KEY_VALUE
// 就是这么漏出来的，那可是密码明文进 AI 上下文。
var keyNormalizer = strings.NewReplacer("_", "", "-", "", ".", "")

// isReferenceKey 判断这个键装的是"指向凭据的引用"而不是凭据本身。
// 这些必须原样保留：排障时要靠 secretName 去查到底引用了哪个 Secret，脱掉就断链了。
// 入参已由 looksSensitiveKey 归一化（小写、去分隔符）。
func isReferenceKey(lk string) bool {
	return strings.Contains(lk, "name") || strings.Contains(lk, "ref") || strings.Contains(lk, "path")
}

func looksSensitiveKey(k string) bool {
	lk := keyNormalizer.Replace(strings.ToLower(k))
	if isReferenceKey(lk) {
		return false
	}
	for _, h := range sensitiveValueKeys {
		if strings.Contains(lk, h) {
			return true
		}
	}
	return false
}

// cmdFlagSensitive 命令行里表示"下一个参数是口令"的旗标。
//
// 🔴 只匹配**完整旗标**，不做包含匹配：`--passive` 里含 "pass" 但它不是口令。
var cmdFlagSensitive = map[string]bool{
	"--pass": true, "--password": true, "-p": true, "--passwd": true,
	"--token": true, "--api-key": true, "--apikey": true, "--secret": true,
	"-a":     true, // redis-cli -a <password>
	"--auth": true, "--credential": true, "--credentials": true,
}

// cmdInlineSensitive 形如 `--password=xxx` / `PGPASSWORD=xxx` 的单元素写法。
var cmdInlineSensitive = regexp.MustCompile(
	`(?i)^(-{0,2}[a-z0-9_-]*(?:pass(?:wd|word)?|token|secret|api[_-]?key|auth)[a-z0-9_-]*)=(.+)$`)

// cmdInlineInShell 一整条 shell 命令塞在**单个字符串**里时，口令的形状。
//
// 🔴 `sh -c "redis-cli -a S3cr3t ping"` —— 旗标和口令在同一个元素里，
//
//	按数组下标找"下一个元素"的判据在这里完全失效。
//	⚠️ 这个盲区是补完数组形态之后才发现的：第一版单测里恰好有这么一行，
//	但断言没覆盖到它，于是测试全绿而口令还在输出里。
//	**测试用例里出现过的形态，必须真的被断言到。**
var cmdInlineInShell = regexp.MustCompile(
	`(?i)(\s-{1,2}(?:a|p|pass|passwd|password|token|secret|auth|api[_-]?key)[= ]\s*)([^\s'"]+)`)

// redactCmdArgs 脱敏**命令行数组**里的口令。
//
// 🔴 这一形态与键值对完全不同：密码没有键名，它只是数组里旗标**后面的那一个元素**。
//
//	livenessProbe:
//	  exec:
//	    command: [redis-cli, --user, cmdb, --pass, <明文口令>, ping]
//
//	`redactAny` 按"键名像不像敏感词"判断，而这里根本没有键 ——
//	于是探针里的密码原样返回（OPSCMDB-048，UAT devops/cmdb-redis 上实测撞见）。
//
// ⚠️ 只脱**紧随其后**的那一个元素。多脱一个会把 `ping` 这类正常参数也盖掉，
//
//	让人看不出这条探针到底在做什么 —— 那会把一个安全修复变成一个可用性问题。
func redactCmdArgs(arr []any) int {
	n := 0
	for i := 0; i < len(arr); i++ {
		s, ok := arr[i].(string)
		if !ok {
			continue
		}
		// ① `--password=xxx` 这种单元素写法
		if m := cmdInlineSensitive.FindStringSubmatch(s); m != nil {
			arr[i] = m[1] + "=" + redactedMark
			n++
			continue
		}
		// ② `--pass <口令>` 这种两元素写法：脱掉下一个
		if cmdFlagSensitive[strings.ToLower(s)] && i+1 < len(arr) {
			if _, ok := arr[i+1].(string); ok {
				arr[i+1] = redactedMark
				n++
				i++ // 跳过已脱敏的那个，避免它自己再被当成旗标
				continue
			}
		}
		// ③ 整条命令塞在一个字符串里（`sh -c "redis-cli -a xxx ping"`）。
		//	⚠️ 这一形态没有"下一个元素"可言，前两条判据都不成立
		if repl := cmdInlineInShell.ReplaceAllString(s, "${1}"+redactedMark); repl != s {
			arr[i] = repl
			n++
		}
	}
	return n
}

// ── 按**值的形态**脱敏（OPSCMDB-050）──
//
// 🔴 键名判据挡不住这两种：
//
//	DATABASE_URL:     postgres://user:<口令>@host:5432/db     ← 键名里没有 password
//	LARK_WEBHOOK_URL: https://…/bot/v2/hook/<token>           ← 键名里没有 secret
//
// 而它们的危害和 Secret 一样：拿到连接串就能连库，拿到 webhook 就能冒充告警。
// `get_manifest` 只需要**读 Deployment 的权限**就能调 —— 那是所有排障角色的默认权限，
// 等于把"能读 Secret"的门槛降到了"能读 Deployment"。

// connStringUserinfo URL 里的 `scheme://user:pass@host` 段。
//
// ⚠️ 只打码**口令那一段**，保留 scheme / user / host ——
//
//	把整个 URL 打掉的话，排障时连"连的是哪台库、用的哪个账号"都看不到了，
//	而那两项恰恰是最常要核对的（本轮正是靠 DSN 认出连错了库）。
var connStringUserinfo = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://[^:/@\s]+:)([^@/\s]+)(@)`)

// webhookToken webhook / API 路径里的长随机段。
//
// ⚠️ 判据是**路径关键字 + 足够长的随机串**，两个条件都要 ——
//
//	只按长度会把镜像 digest、UID、commit sha 全打掉（那些是排障必需的）。
var webhookToken = regexp.MustCompile(`((?i:/(?:hook|webhook|bot/v2/hook|services|token)/))([A-Za-z0-9_\-]{16,})`)

// suspiciousValue 疑似凭据但**判不准**的值：长随机串、Base64、私钥块开头。
//
// 🔴 它不参与脱敏，只参与**计数**。
//
//	档案 OPSCMDB-050 的第 2 条要求：提示里写「已脱敏 N 处」时，
//	若同时存在未能判定的可疑值，必须另起一行说「另有 M 处疑似凭据未处理」。
//	只报打了几处，读的人会以为已经干净了 —— 那是把不确定渲染成了确定。
var suspiciousValue = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----|^[A-Za-z0-9+/]{40,}={0,2}$|^[A-Za-z0-9_\-]{32,}$`)

// redactValueForms 按值的形态脱敏一个字符串。返回新值与是否改动。
func redactValueForms(s string) (string, bool) {
	out := connStringUserinfo.ReplaceAllString(s, "${1}"+redactedMark+"${3}")
	out = webhookToken.ReplaceAllString(out, "${1}"+redactedMark)
	return out, out != s
}

// redactAny 递归脱敏，返回被替换的处数。
//
// 三种形态都要覆盖：
//   - 直接键值：{"password": "xxx"}
//   - env 风格的数组元素：{"name": "DB_PASSWORD", "value": "xxx"}——键叫 value，敏感信息在 name 里
//   - 🔴 **命令行数组**：{"command": ["redis-cli", "--pass", "<明文>"]}——**没有键名**，
//     口令只是旗标后面的那个元素。前两种判据在这里全都不成立（OPSCMDB-048）。
func redactAny(v any) int {
	switch t := v.(type) {
	case map[string]any:
		n := 0
		if nm, ok := t["name"].(string); ok && looksSensitiveKey(nm) {
			if _, has := t["value"]; has {
				t["value"] = redactedMark
				n++
			}
		} else if val, ok := t["value"].(string); ok && val != "" {
			// env 项的 name 不敏感时，仍要看 value 的形态 ——
			// DATABASE_URL / LARK_WEBHOOK_URL 正是这一类
			if repl, changed := redactValueForms(val); changed {
				t["value"] = repl
				n++
			}
		}
		for k, vv := range t {
			if s, ok := vv.(string); ok && s != "" {
				if looksSensitiveKey(k) {
					t[k] = redactedMark
					n++
					continue
				}
				// 键名不敏感，再看**值长什么样**（连接串 / webhook token）
				if repl, changed := redactValueForms(s); changed {
					t[k] = repl
					n++
					continue
				}
			}
			// command / args 是**命令行数组**，口令在旗标后面而不是某个键下面。
			// ⚠️ 必须在递归之前处理：redactAny 对字符串数组无能为力（没有键可判）
			if arr, ok := vv.([]any); ok && (k == "command" || k == "args") {
				n += redactCmdArgs(arr)
				continue
			}
			n += redactAny(vv)
		}
		return n
	case []any:
		n := 0
		for _, e := range t {
			n += redactAny(e)
		}
		return n
	}
	return 0
}

// scrubManifest 删掉纯噪声字段并脱敏。
//
// 返回 (已脱敏处数, **判不准的可疑值**处数)。
//
// 🔴 第二个返回值是这次修复的重点：只报"打了 N 处"会让人以为已经干净了，
//
//	而实际可能还有判不出来的（长随机串、Base64、私钥块）。
//	不确定的状态不能渲染成确定的状态 —— 那是全站三态纪律的一部分。
func scrubManifest(obj map[string]any) (int, int) {
	if md, ok := obj["metadata"].(map[string]any); ok {
		// managedFields 动辄几百行且对排障零价值；last-applied-configuration 是整份 spec 的副本，
		// 既翻倍体积又可能把 env 明文再抄一遍。
		delete(md, "managedFields")
		if ann, ok := md["annotations"].(map[string]any); ok {
			delete(ann, "kubectl.kubernetes.io/last-applied-configuration")
			if len(ann) == 0 {
				delete(md, "annotations")
			}
		}
	}
	n := redactAny(obj)
	return n, countSuspicious(obj)
}

// countSuspicious 数一遍**没被脱敏但看着像凭据**的值。只计数，不改动。
//
// ⚠️ 刻意不去脱敏它们：长随机串里混着镜像 digest、UID、commit sha ——
//
//	打掉的话排障时最常要核对的几项就没了。
//	所以这里的答案是"我不确定"，而不是"我处理了"或"没有问题"。
func countSuspicious(v any) int {
	switch t := v.(type) {
	case map[string]any:
		n := 0
		for _, vv := range t {
			if s, ok := vv.(string); ok {
				if s != redactedMark && suspiciousValue.MatchString(s) {
					n++
				}
				continue
			}
			n += countSuspicious(vv)
		}
		return n
	case []any:
		n := 0
		for _, e := range t {
			n += countSuspicious(e)
		}
		return n
	}
	return 0
}

// Manifest 取单个对象的完整 YAML（只读 + 脱敏），补上"采集只落列、看不到 spec"的缺口。
func (h *K8sDiagHandler) Manifest(c *gin.Context) {
	cid, _ := strconv.Atoi(c.Query("cluster_id"))
	kindRaw, name := c.Query("kind"), c.Query("name")
	ns := c.Query("namespace")
	if cid == 0 || kindRaw == "" || name == "" {
		httpx.Required(c, "cluster_id/kind/name")
		return
	}
	kind := strings.ToLower(strings.TrimSpace(kindRaw))
	kind = strings.TrimSuffix(kind, "s") // 容忍 pods/deployments 这类复数写法
	if _, ok := manifestKinds[kind]; !ok {
		if _, ok2 := manifestKinds[kind+"s"]; ok2 {
			kind += "s"
		}
	}

	// Secret 的内容永不经过本进程：CMDB 全程只碰 PartialObjectMetadata（见 Pool.MetadataFor 的说明），
	// 这里开个口子就把那个保证破坏了。
	if kind == "secret" {
		// ⚠️ 界面上的人执行不了 config_audit / query_prometheus ——
		//	工具链只进 mcp_hint，hint 给的是他在网页里真能做的下一步
		//	（check-mcp-text-leak）
		c.JSON(http.StatusForbidden, gin.H{
			"error_key": "error.secretContentRefused",
			"error":     "拒绝返回 Secret 内容（CMDB 只读设计：Secret 内容永不经过本服务）",
			"hint_key":  "error.secretContentRefusedHint",
			"hint":      "去「配置合规」页可以确认这个 Secret 是否存在、有哪些键名，但不会显示值",
			"mcp_hint":  "查 Secret 是否存在/键名，用 config_audit；或用 query_prometheus 查 kube_secret_info",
		})
		return
	}

	mk, ok := manifestKinds[kind]
	if kind == "gateway" {
		grp := c.Query("api_group")
		if grp == "" {
			grp = "networking.istio.io" // UAT 用的是 Istio Gateway
		}
		mk, ok = gatewayKinds[grp]
		if !ok {
			httpx.Invalid(c, "api_group", "networking.istio.io|gateway.networking.k8s.io")
			return
		}
	}
	if !ok {
		kinds := make([]string, 0, len(manifestKinds))
		for k := range manifestKinds {
			kinds = append(kinds, k)
		}
		sortStrings(kinds)
		// supported 放 params 不放 extra：文案要**直接列出**支持的类型。
		// 写"见下"而下面什么都没有，比不说更糟（前端不渲染 extra）。
		httpx.FailKey(c, httpx.CodeBadRequest, "error.unsupportedKind", nil,
			map[string]any{"kind": kindRaw, "supported": kinds})
		return
	}
	if !mk.clusterScoped && ns == "" {
		httpx.FailKey(c, httpx.CodeBadRequest, "error.namespaceRequiredForKind", nil, map[string]any{"kind": kindRaw})
		return
	}

	dc, err := h.Pool.DynamicFor(cid)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()

	lookupNS := ns
	if mk.clusterScoped {
		lookupNS = ""
	}
	tries := append([]schema.GroupVersionResource{mk.gvr}, mk.fallback...)
	var obj map[string]any
	var lastErr error
	var usedGVR schema.GroupVersionResource
	for _, gvr := range tries {
		u, e := dc.Resource(gvr).Namespace(lookupNS).Get(ctx, name, metav1.GetOptions{})
		if e == nil {
			obj, usedGVR = u.Object, gvr
			break
		}
		lastErr = e
		// 只有"这个 API 版本不存在"才值得换版本重试；NotFound 说明版本对、对象不在，换版本没意义。
		if !apierrors.IsNotFound(e) && !strings.Contains(e.Error(), "could not find the requested resource") {
			break
		}
		if apierrors.IsNotFound(e) && len(tries) == 1 {
			break
		}
	}
	if obj == nil {
		msg := "取对象失败"
		if lastErr != nil {
			msg = lastErr.Error()
		}
		logx.J("k8s_diag", "manifest_failed", map[string]any{
			"cluster_id": cid, "kind": kindRaw, "namespace": ns, "name": name, "err": msg,
		})
		status := http.StatusBadGateway
		if lastErr != nil && apierrors.IsNotFound(lastErr) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": msg})
		return
	}

	redacted, suspicious := scrubManifest(obj)
	out, err := yaml.Marshal(obj)
	if err != nil {
		httpx.Fail(c, httpx.CodeInternal, fmt.Errorf("序列化 YAML 失败: %w", err), nil)
		return
	}

	logx.J("k8s_diag", "manifest", map[string]any{
		"cluster_id": cid, "kind": kindRaw, "namespace": ns, "name": name,
		"gvr": usedGVR.String(), "redacted": redacted, "suspicious": suspicious, "bytes": len(out),
	})
	WriteAudit(h.DB, c, "view_manifest:"+kind, ns+"/"+name)

	header := "# " + usedGVR.String() + " " + ns + "/" + name + "\n"
	if redacted > 0 {
		// 必须说出来：否则调用方会把 ***REDACTED*** 当成配置里真实写着的值，进而得出错误结论。
		header += "# 已脱敏 " + strconv.Itoa(redacted) + " 处敏感值（显示为 " + redactedMark + "），引用名(secretName 等)未改动\n"
	}
	// 🔴 判不准的必须单独说。
	//	只写"已脱敏 N 处"给的是"处理干净了"的暗示 —— 而这次实测是打了 4 处、漏了 2 处，
	//	提示却只说打了 4 处（OPSCMDB-050）。
	if suspicious > 0 {
		header += "# ⚠️ 另有 " + strconv.Itoa(suspicious) + " 处值看着像凭据但**未能判定**（长随机串 / Base64 / 私钥块），" +
			"没有脱敏也没有确认安全——自行核对\n"
	}
	header += "# managedFields / last-applied-configuration 已移除\n\n"
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(header+string(out)))
}

// sortStrings 就地升序（避免为一处排序引入 sort 之外的依赖，且此处 sort 已在包内使用）。
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
