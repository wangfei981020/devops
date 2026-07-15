package services

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// 后端服务的 secret 不在服务 chart 里，而是集中在独立的 z-kv-secrets chart 的 values.yaml：
//   secrets:      段 → 公共/普通 secret（redis/nacos/rocketmq/encyrpt-salt/uid-tidb…），任意 key-value
//   tidbSecrets:  段 → 每个后端服务一条专属 TiDB 库（database + extraStringData，自动拼 tidbCommon）
// 新增后端模块时：服务 extraEnvVars 引用的 name 已在 z-kv → 复用；未在 → 专属，需填内容并追加到 z-kv。
// z-kv-secrets 可跨环境共用（如 g33 复用 g32 的），路径按「项目参数」配的 zkv_secrets_path。

// KV 有序键值对（extraStringData 要保序：TIDB_PWDSALT、TIDB_PWDCRYPT、额外字段）。
type KV struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// TidbSecretEntry 一条待追加的专属 TiDB secret。
type TidbSecretEntry struct {
	Name      string // <模块名>-tidb-secret
	Namespace string // 走「项目参数」的 namespace（跟服务同 ns，解决共用 z-kv 跨 ns）
	Database  string // 用户填
	Extra     []KV   // TIDB_PWDSALT / TIDB_PWDCRYPT（复用环境公共）+ 可选 COMMON_TOKEN/CONFIG_NAME…
}

// PlainSecretEntry 一条待追加的普通 Opaque secret（写 z-kv 的 secrets: 段，固定 stringData 明文；
// 加密交给集群侧 etcd/sealed-secrets 等，界面只填明文）。
type PlainSecretEntry struct {
	Name      string
	Namespace string
	Type      string // 默认 Opaque
	KVs       []KV
}

// ZkvSecretNames 列出 z-kv-secrets values 里已定义的所有 secret 名（secrets: + tidbSecrets: 两段）。
// 新增后端时用来判定引用的 secret「已存在（复用）」还是「待新建（专属，需填内容）」。
func ZkvSecretNames(content []byte) []string {
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil || len(root.Content) == 0 {
		return nil
	}
	doc := root.Content[0]
	var names []string
	for _, seg := range []string{"secrets", "tidbSecrets"} {
		seq, err := findMappingKey(doc, seg)
		if err != nil || seq.Kind != yaml.SequenceNode {
			continue
		}
		for _, item := range seq.Content {
			if item.Kind != yaml.MappingNode {
				continue
			}
			if n, e := findMappingKey(item, "name"); e == nil {
				if v := strings.TrimSpace(n.Value); v != "" {
					names = append(names, v)
				}
			}
		}
	}
	return names
}

// ZkvTidbDefaults 从 z-kv-secrets 现有 tidbSecrets 里取第一条的 TIDB_PWDSALT/TIDB_PWDCRYPT，
// 作为新增专属 tidb secret 的默认值（这俩是环境公共，三个服务都一样）。取不到返回空。
func ZkvTidbDefaults(content []byte) (salt, crypt string) {
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil || len(root.Content) == 0 {
		return "", ""
	}
	doc := root.Content[0]
	seq, err := findMappingKey(doc, "tidbSecrets")
	if err != nil || seq.Kind != yaml.SequenceNode {
		return "", ""
	}
	for _, item := range seq.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		if salt == "" {
			if n, e := findMappingKey(item, "extraStringData", "TIDB_PWDSALT"); e == nil {
				salt = strings.TrimSpace(n.Value)
			}
		}
		if crypt == "" {
			if n, e := findMappingKey(item, "extraStringData", "TIDB_PWDCRYPT"); e == nil {
				crypt = strings.TrimSpace(n.Value)
			}
		}
		if salt != "" && crypt != "" {
			break
		}
	}
	return
}

// ExtraEnvVarNames 读后端服务 values.yaml 里 extraEnvVars 引用的 secret 名列表。
func ExtraEnvVarNames(content []byte) []string {
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil || len(root.Content) == 0 {
		return nil
	}
	seq, err := findMappingKey(root.Content[0], "extraEnvVars")
	if err != nil || seq.Kind != yaml.SequenceNode {
		return nil
	}
	var names []string
	for _, item := range seq.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		if n, e := findMappingKey(item, "name"); e == nil {
			if v := strings.TrimSpace(n.Value); v != "" {
				names = append(names, v)
			}
		}
	}
	return names
}

// RenameSecretRefs 把服务 values 的 extraEnvVars 里引用的 secret 名的「项目前缀」换成目标项目前缀。
// 跨项目复用模板时用：模板样板来自 g32，secret 名带 g32-；换到 g50 环境要变 g50-nacos-secret。
//   knownProjects: 所有已登记项目名集合（project_env 去 -env 后缀）。
//   规则：secret 名第一段(第一个 '-' 前)命中 knownProjects 且 != dstProj → 换成 dstProj；
//        无项目前缀的(encyrpt-salt)、已是目标前缀的，不动。用 yaml.Node 精准改每个 name，不误伤别处。
func RenameSecretRefs(content []byte, knownProjects map[string]bool, dstProj string) []byte {
	if dstProj == "" {
		return content
	}
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil || len(root.Content) == 0 {
		return content
	}
	seq, err := findMappingKey(root.Content[0], "extraEnvVars")
	if err != nil || seq.Kind != yaml.SequenceNode {
		return content
	}
	changed := false
	for _, item := range seq.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		n, e := findMappingKey(item, "name")
		if e != nil {
			continue
		}
		name := strings.TrimSpace(n.Value)
		i := strings.Index(name, "-")
		if i <= 0 {
			continue
		}
		if prefix := name[:i]; prefix != dstProj && knownProjects[prefix] {
			n.Value = dstProj + name[i:]
			changed = true
		}
	}
	if !changed {
		return content
	}
	if out, e := encodeYAML(&root); e == nil {
		return out
	}
	return content
}

// RenameZkvSecretNames 初始化 z-kv-secrets 时用：把 secrets:/tidbSecrets: 两段里每条的 name 项目前缀
// 换成目标项目前缀（key/value 不动）。规则同 RenameSecretRefs：第一段命中 knownProjects 且 != dstProj → 换。
func RenameZkvSecretNames(content []byte, knownProjects map[string]bool, dstProj string) []byte {
	if dstProj == "" {
		return content
	}
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil || len(root.Content) == 0 {
		return content
	}
	doc := root.Content[0]
	changed := false
	for _, seg := range []string{"secrets", "tidbSecrets"} {
		seq, err := findMappingKey(doc, seg)
		if err != nil || seq.Kind != yaml.SequenceNode {
			continue
		}
		for _, item := range seq.Content {
			if item.Kind != yaml.MappingNode {
				continue
			}
			n, e := findMappingKey(item, "name")
			if e != nil {
				continue
			}
			name := strings.TrimSpace(n.Value)
			i := strings.Index(name, "-")
			if i <= 0 {
				continue
			}
			if prefix := name[:i]; prefix != dstProj && knownProjects[prefix] {
				n.Value = dstProj + name[i:]
				changed = true
			}
		}
	}
	if !changed {
		return content
	}
	if out, e := encodeYAML(&root); e == nil {
		return out
	}
	return content
}

// AppendTidbSecret 往 z-kv-secrets values 的 tidbSecrets: 段追加一条（保序/保注释，同名查重不覆盖）。
func AppendTidbSecret(content []byte, e TidbSecretEntry) ([]byte, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		return nil, fmt.Errorf("parse z-kv-secrets values.yaml: %w", err)
	}
	if len(root.Content) == 0 {
		return nil, fmt.Errorf("empty z-kv-secrets values.yaml")
	}
	seq, err := findMappingKey(root.Content[0], "tidbSecrets")
	if err != nil || seq.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("z-kv-secrets 里没有 tidbSecrets 段（或不是列表）")
	}
	for _, item := range seq.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		if n, e2 := findMappingKey(item, "name"); e2 == nil && n.Value == e.Name {
			return nil, fmt.Errorf("secret %q 已在 z-kv-secrets 中登记，请勿重复新增", e.Name)
		}
	}
	m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	appendStr(m, "name", e.Name)
	if e.Namespace != "" {
		appendStr(m, "namespace", e.Namespace)
	}
	appendStr(m, "database", e.Database)
	if len(e.Extra) > 0 {
		extraVal := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for _, kv := range e.Extra {
			if strings.TrimSpace(kv.Key) == "" {
				continue
			}
			appendStr(extraVal, kv.Key, kv.Value)
		}
		m.Content = append(m.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "extraStringData"}, extraVal)
	}
	seq.Content = append(seq.Content, m)
	return encodeYAML(&root)
}

// scalarStr 造一个字符串标量节点。
func scalarStr(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
}

// ensureSeqSegment 找 doc 下名为 seg 的序列节点；没有就建一个空序列并挂上。
func ensureSeqSegment(doc *yaml.Node, seg string) *yaml.Node {
	if n, err := findMappingKey(doc, seg); err == nil && n.Kind == yaml.SequenceNode {
		return n
	} else if err == nil {
		// 存在但不是序列（如 null / 空）→ 覆盖成空序列
		n.Kind = yaml.SequenceNode
		n.Tag = "!!seq"
		n.Content = nil
		return n
	}
	tseq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	doc.Content = append(doc.Content, scalarStr(seg), tseq)
	return tseq
}

// AppendPlainSecret 往 z-kv-secrets 的 secrets: 段追加一条普通 secret（段不存在则新建；同名查重）。
func AppendPlainSecret(content []byte, e PlainSecretEntry) ([]byte, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		return nil, fmt.Errorf("parse z-kv-secrets values.yaml: %w", err)
	}
	if len(root.Content) == 0 {
		return nil, fmt.Errorf("empty z-kv-secrets values.yaml")
	}
	seq := ensureSeqSegment(root.Content[0], "secrets")
	for _, item := range seq.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		if n, e2 := findMappingKey(item, "name"); e2 == nil && n.Value == e.Name {
			return nil, fmt.Errorf("secret %q 已在 z-kv-secrets 中登记，请勿重复新增", e.Name)
		}
	}
	typ := strings.TrimSpace(e.Type)
	if typ == "" {
		typ = "Opaque"
	}
	m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	appendStr(m, "name", e.Name)
	if e.Namespace != "" {
		appendStr(m, "namespace", e.Namespace)
	}
	appendStr(m, "type", typ)
	dataVal := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, kv := range e.KVs {
		if strings.TrimSpace(kv.Key) == "" {
			continue
		}
		appendStr(dataVal, kv.Key, kv.Value)
	}
	m.Content = append(m.Content, scalarStr("stringData"), dataVal)
	seq.Content = append(seq.Content, m)
	return encodeYAML(&root)
}

// AppendSecretsFromYAML 用于「YAML 模式」：把用户编辑的片段（含 tidbSecrets:/secrets: 列表）
// 直接把每个列表项节点并入 z-kv-secrets 对应段（保留原样格式，同名查重）。
func AppendSecretsFromYAML(zkvContent, fragment []byte) ([]byte, error) {
	var frag yaml.Node
	if err := yaml.Unmarshal(fragment, &frag); err != nil {
		return nil, fmt.Errorf("解析密钥 YAML 失败: %w", err)
	}
	if len(frag.Content) == 0 {
		return zkvContent, nil
	}
	fdoc := frag.Content[0]
	var root yaml.Node
	if err := yaml.Unmarshal(zkvContent, &root); err != nil {
		return nil, fmt.Errorf("parse z-kv-secrets values.yaml: %w", err)
	}
	if len(root.Content) == 0 {
		return nil, fmt.Errorf("empty z-kv-secrets values.yaml")
	}
	rdoc := root.Content[0]
	appended := false
	for _, seg := range []string{"tidbSecrets", "secrets"} {
		fseq, err := findMappingKey(fdoc, seg)
		if err != nil || fseq.Kind != yaml.SequenceNode || len(fseq.Content) == 0 {
			continue
		}
		tseq := ensureSeqSegment(rdoc, seg)
		exist := map[string]bool{}
		for _, it := range tseq.Content {
			if n, e := findMappingKey(it, "name"); e == nil {
				exist[n.Value] = true
			}
		}
		for _, it := range fseq.Content {
			if it.Kind != yaml.MappingNode {
				continue
			}
			n, e := findMappingKey(it, "name")
			if e != nil || strings.TrimSpace(n.Value) == "" {
				return nil, fmt.Errorf("密钥 YAML 里 %s 有条目缺 name", seg)
			}
			if exist[n.Value] {
				return nil, fmt.Errorf("secret %q 已在 z-kv-secrets 中登记，请勿重复新增", n.Value)
			}
			tseq.Content = append(tseq.Content, it)
			exist[n.Value] = true
			appended = true
		}
	}
	if !appended {
		return zkvContent, nil
	}
	return encodeYAML(&root)
}
