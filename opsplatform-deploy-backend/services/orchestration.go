package services

import (
	"bytes"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// 服务编排：从"参照模板"脚手架一个新模块的纯逻辑（不碰 git/helm，便于单测）。
// 落地三件事：① Chart.yaml 的 name 改成模块名 ② values.yaml 变量替换
// ③ 往 {chartBasePath}-apps/values.yaml 的 argocdApplications 追加一条。

// SetChartName 把 Chart.yaml 的 name 改成 moduleName（容器名 {{ .Chart.Name }} 依赖它）。
// 用 yaml.v3 Node API 保留注释/格式。
func SetChartName(content []byte, moduleName string) ([]byte, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		return nil, fmt.Errorf("parse Chart.yaml: %w", err)
	}
	if len(root.Content) == 0 {
		return nil, fmt.Errorf("empty Chart.yaml")
	}
	nameNode, err := findMappingKey(root.Content[0], "name")
	if err != nil {
		return nil, fmt.Errorf("Chart.yaml name: %w", err)
	}
	nameNode.Value = moduleName
	nameNode.Tag = "!!str"
	return encodeYAML(&root)
}

// AppsEntry 是 -apps/values.yaml 里 argocdApplications.<模块名> 的内容。
type AppsEntry struct {
	Name             string // = 模块名
	Namespace        string
	Disable          bool // true=app-of-apps 不生成 Application（安全预演）
	DisableAutomated bool
}

// AppendAppsEntry 往 argocdApplications 追加一条 <moduleName> 映射，保留其余条目/注释/顺序。
// 若已存在同名 key 返回错误（不覆盖别人）。
func AppendAppsEntry(appsContent []byte, e AppsEntry) ([]byte, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(appsContent, &root); err != nil {
		return nil, fmt.Errorf("parse -apps values.yaml: %w", err)
	}
	if len(root.Content) == 0 {
		return nil, fmt.Errorf("empty -apps values.yaml")
	}
	appsNode, err := findMappingKey(root.Content[0], "argocdApplications")
	if err != nil {
		return nil, fmt.Errorf("argocdApplications: %w", err)
	}
	if appsNode.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("argocdApplications is not a mapping")
	}
	// 查重：不覆盖已有条目
	for i := 0; i+1 < len(appsNode.Content); i += 2 {
		if appsNode.Content[i].Value == e.Name {
			return nil, fmt.Errorf("模块 %q 已在 -apps 中登记，请勿重复新增", e.Name)
		}
	}

	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: e.Name}
	valNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	appendBool(valNode, "disableAutomated", e.DisableAutomated)
	appendBool(valNode, "disable", e.Disable)
	appendStr(valNode, "name", e.Name)
	appendBool(valNode, "helm", true)
	appendStr(valNode, "namespace", e.Namespace)
	appsNode.Content = append(appsNode.Content, keyNode, valNode)

	return encodeYAML(&root)
}

// ApplyEnvIngress 预填时：把 ingressGateway.name 设为目标环境配的网关名、host 清空(域名默认留空)。
// 只在 values 里存在 ingressGateway 时生效（后端无 ingress 则原样返回）。gateway 为空则不改 name。
func ApplyEnvIngress(content []byte, gatewayName string) ([]byte, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		return content, nil // 解析不了就原样返回，不阻断预填
	}
	if len(root.Content) == 0 {
		return content, nil
	}
	ig, err := findMappingKey(root.Content[0], "ingressGateway")
	if err != nil || ig.Kind != yaml.MappingNode {
		return content, nil // 没有 ingressGateway，原样
	}
	if gatewayName != "" {
		if n, e := findMappingKey(root.Content[0], "ingressGateway", "name"); e == nil {
			n.Value = gatewayName
			n.Tag = "!!str"
		}
	}
	// 域名默认留空：host 置为空序列
	if h, e := findMappingKey(root.Content[0], "ingressGateway", "host"); e == nil {
		h.Kind = yaml.SequenceNode
		h.Tag = "!!seq"
		h.Content = nil
		h.Style = yaml.FlowStyle
	}
	return encodeYAML(&root)
}

// SetIngressHost 把 ingressGateway.host 设成 [host]（预填自动带出访问域名用）。
// 只在存在 ingressGateway.host 时生效；host 为空则清空（保持原"域名默认空"行为）。
func SetIngressHost(content []byte, host string) []byte {
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil || len(root.Content) == 0 {
		return content
	}
	h, err := findMappingKey(root.Content[0], "ingressGateway", "host")
	if err != nil {
		return content // 没有 ingressGateway.host（非前端/无 ingress 模板）→ 原样
	}
	h.Kind = yaml.SequenceNode
	h.Tag = "!!seq"
	h.Style = yaml.FlowStyle
	if host == "" {
		h.Content = nil
	} else {
		h.Content = []*yaml.Node{{Kind: yaml.ScalarNode, Tag: "!!str", Value: host}}
	}
	if out, e := encodeYAML(&root); e == nil {
		return out
	}
	return content
}

// EnsureGlobalLabels 兜底：values 里没有 global.labels 就补一个 global: { labels: {} }。
// 因为服务的 helper(如 web.labels)会读 .Values.global.labels，而部署时不注入 global，
// 缺这行就 nil pointer。已有则原样不动。global 段插在最前(跟工作正常的服务一致)。
func EnsureGlobalLabels(content []byte) []byte {
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil || len(root.Content) == 0 {
		return content
	}
	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return content
	}
	if _, err := findMappingKey(doc, "global", "labels"); err == nil {
		return content // 已有 global.labels
	}
	labelsKey := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "labels"}
	labelsVal := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Style: yaml.FlowStyle}
	if g, err := findMappingKey(doc, "global"); err == nil && g.Kind == yaml.MappingNode {
		g.Content = append(g.Content, labelsKey, labelsVal) // global 存在但缺 labels
	} else {
		globalKey := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "global"}
		globalVal := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		globalVal.Content = append(globalVal.Content, labelsKey, labelsVal)
		doc.Content = append([]*yaml.Node{globalKey, globalVal}, doc.Content...) // 插最前
	}
	if out, err := encodeYAML(&root); err == nil {
		return out
	}
	return content
}

// SetImageRepository 把 values 里 image.repository 设成推导出的仓库地址（预填时用）。
// 只在存在 image.repository 时生效；解析失败原样返回，不阻断预填。repo 为空则不改。
func SetImageRepository(content []byte, repo string) []byte {
	if repo == "" {
		return content
	}
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil || len(root.Content) == 0 {
		return content
	}
	n, err := findMappingKey(root.Content[0], "image", "repository")
	if err != nil {
		return content // 没有 image.repository，原样
	}
	n.Value = repo
	n.Tag = "!!str"
	if out, e := encodeYAML(&root); e == nil {
		return out
	}
	return content
}

// SetImageTag 把 values 里 image.tag 设成指定值（预填自动带最新 tag 用；tag 为 "" 则清空）。
// 只在存在 image.tag 时生效；强制字符串，避免 1.28 被当浮点。
func SetImageTag(content []byte, tag string) []byte {
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil || len(root.Content) == 0 {
		return content
	}
	n, err := findMappingKey(root.Content[0], "image", "tag")
	if err != nil {
		return content
	}
	n.Value = tag
	n.Tag = "!!str"
	n.Style = yaml.DoubleQuotedStyle // 让空/纯数字 tag 也稳定序列化
	if out, e := encodeYAML(&root); e == nil {
		return out
	}
	return content
}

// GetImageRepoTag 从 values 读 image.repository / image.tag。
func GetImageRepoTag(content []byte) (repo, tag string) {
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil || len(root.Content) == 0 {
		return "", ""
	}
	if n, e := findMappingKey(root.Content[0], "image", "repository"); e == nil {
		repo = strings.TrimSpace(n.Value)
	}
	if n, e := findMappingKey(root.Content[0], "image", "tag"); e == nil {
		tag = strings.TrimSpace(n.Value)
	}
	return
}

// HarborDomain 从全局 harbor_url 取出纯域名（去掉 https:// 前缀和尾部 /）。
// 例：https://harbor.slileisure.com/ → harbor.slileisure.com
func HarborDomain(harborURL string) string {
	s := strings.TrimSpace(harborURL)
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	return strings.TrimRight(s, "/")
}

// DeriveImageRepository 从模块名推导镜像仓库路径。
// 约定：模块名带项目前缀 "<project>-"，镜像路径去掉前缀。
// 例：harborBase=harbor.slileisure.com, project=g32, module=g32-baccarat-settle-backend
//     → harbor.slileisure.com/g32/baccarat-settle-backend
func DeriveImageRepository(harborBase, project, moduleName string) string {
	svc := strings.TrimPrefix(moduleName, project+"-")
	return fmt.Sprintf("%s/%s/%s", strings.TrimRight(harborBase, "/"), project, svc)
}

func appendStr(m *yaml.Node, key, val string) {
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: val})
}

func appendBool(m *yaml.Node, key string, val bool) {
	v := "false"
	if val {
		v = "true"
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: v})
}

func encodeYAML(root *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return nil, err
	}
	_ = enc.Close()
	return buf.Bytes(), nil
}
