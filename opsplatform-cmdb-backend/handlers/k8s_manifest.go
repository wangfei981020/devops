package handlers

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"

	"opsplatform-cmdb-backend/logx"
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

// redactAny 递归脱敏，返回被替换的处数。
//
// 两种形态都要覆盖：
//   - 直接键值：{"password": "xxx"}
//   - env 风格的数组元素：{"name": "DB_PASSWORD", "value": "xxx"}——键叫 value，敏感信息在 name 里
func redactAny(v any) int {
	switch t := v.(type) {
	case map[string]any:
		n := 0
		if nm, ok := t["name"].(string); ok && looksSensitiveKey(nm) {
			if _, has := t["value"]; has {
				t["value"] = redactedMark
				n++
			}
		}
		for k, vv := range t {
			if s, ok := vv.(string); ok && s != "" && looksSensitiveKey(k) {
				t[k] = redactedMark
				n++
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

// scrubManifest 删掉纯噪声字段并脱敏，返回脱敏处数。
func scrubManifest(obj map[string]any) int {
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
	return redactAny(obj)
}

// Manifest 取单个对象的完整 YAML（只读 + 脱敏），补上"采集只落列、看不到 spec"的缺口。
func (h *K8sDiagHandler) Manifest(c *gin.Context) {
	cid, _ := strconv.Atoi(c.Query("cluster_id"))
	kindRaw, name := c.Query("kind"), c.Query("name")
	ns := c.Query("namespace")
	if cid == 0 || kindRaw == "" || name == "" {
		c.JSON(400, gin.H{"error": "cluster_id/kind/name 必填"})
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
		c.JSON(http.StatusForbidden, gin.H{
			"error": "拒绝返回 Secret 内容（CMDB 只读设计：Secret 内容永不经过本服务）",
			"hint":  "查 Secret 是否存在/键名，用 config_audit；或用 query_prometheus 查 kube_secret_info",
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
			c.JSON(400, gin.H{"error": "api_group 只支持 networking.istio.io 或 gateway.networking.k8s.io"})
			return
		}
	}
	if !ok {
		kinds := make([]string, 0, len(manifestKinds))
		for k := range manifestKinds {
			kinds = append(kinds, k)
		}
		sortStrings(kinds)
		c.JSON(400, gin.H{"error": "不支持的 kind: " + kindRaw, "supported": kinds})
		return
	}
	if !mk.clusterScoped && ns == "" {
		c.JSON(400, gin.H{"error": kindRaw + " 是命名空间级资源，namespace 必填"})
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

	redacted := scrubManifest(obj)
	out, err := yaml.Marshal(obj)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "序列化 YAML 失败: " + err.Error()})
		return
	}

	logx.J("k8s_diag", "manifest", map[string]any{
		"cluster_id": cid, "kind": kindRaw, "namespace": ns, "name": name,
		"gvr": usedGVR.String(), "redacted": redacted, "bytes": len(out),
	})
	WriteAudit(h.DB, c, "view_manifest:"+kind, ns+"/"+name)

	header := "# " + usedGVR.String() + " " + ns + "/" + name + "\n"
	if redacted > 0 {
		// 必须说出来：否则调用方会把 ***REDACTED*** 当成配置里真实写着的值，进而得出错误结论。
		header += "# 已脱敏 " + strconv.Itoa(redacted) + " 处敏感值（显示为 " + redactedMark + "），引用名(secretName 等)未改动\n"
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
