package services

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

// UpdateImageTag 在 values.yaml 里定位 image.tag 并替换。
// 使用 yaml.v3 的 Node API 以保留注释和原始格式。
// 返回 (newContent, changed, err)。changed=false 表示原值就是 newTag。
func UpdateImageTag(content []byte, newTag string) ([]byte, bool, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		return nil, false, fmt.Errorf("parse yaml: %w", err)
	}
	if len(root.Content) == 0 {
		return nil, false, fmt.Errorf("empty yaml")
	}
	doc := root.Content[0]
	tagNode, err := findMappingKey(doc, "image", "tag")
	if err != nil {
		return nil, false, err
	}
	if tagNode.Value == newTag {
		return content, false, nil
	}
	tagNode.Value = newTag
	tagNode.Style = yaml.DoubleQuotedStyle

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&root); err != nil {
		return nil, false, err
	}
	_ = enc.Close()
	return buf.Bytes(), true, nil
}

func ReadImage(content []byte) (repo, tag string, err error) {
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		return "", "", err
	}
	if len(root.Content) == 0 {
		return "", "", fmt.Errorf("empty yaml")
	}
	doc := root.Content[0]
	if r, e := findMappingKey(doc, "image", "repository"); e == nil {
		repo = r.Value
	}
	if t, e := findMappingKey(doc, "image", "tag"); e == nil {
		tag = t.Value
	}
	return repo, tag, nil
}

// findMappingKey 沿 path 深入 MappingNode，返回对应 value Node。
func findMappingKey(node *yaml.Node, path ...string) (*yaml.Node, error) {
	current := node
	for _, key := range path {
		if current.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("not a mapping while looking up %q", key)
		}
		found := false
		for i := 0; i+1 < len(current.Content); i += 2 {
			k := current.Content[i]
			v := current.Content[i+1]
			if k.Value == key {
				current = v
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("key %q not found", key)
		}
	}
	return current, nil
}
