package inventory

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Service 一个 ansible playbook 抽象成的 service
type Service struct {
	Name         string   `json:"name"`          // playbook 文件名（去 .yaml）
	AnsibleGroup string   `json:"ansible_group"` // playbook 第一行 hosts: -<group>
	Hosts        []string `json:"hosts"`         // inventory 里 group 对应的 IP 列表
	AppName      string   `json:"app_name"`      // playbook vars.app_name（多数 == name）
	SourcePath   string   `json:"source_path"`   // playbook vars.source_path
}

// Scanner 扫某个 project + env 下的 services
//
//	playbook 路径：<ansibleRoot>/<project>/<env>/*.yaml
//	inventory 路径：<ansibleRoot>/inventory/<project>/<env>/<project>_<env>_hosts
type Scanner struct {
	AnsibleRoot string
}

// ListServices 扫 playbook 目录 + 解析 yaml 关键字段 + 关联 inventory hosts
//
//	playbook 解析容错：解析失败返回空字段不阻塞列表
//	inventory 缺失时 Hosts 为空（不报错，让上层显示"未在 inventory 找到"）
func (s *Scanner) ListServices(project, env string) ([]Service, error) {
	dir := filepath.Join(s.AnsibleRoot, project, env)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read playbook dir %s: %w", dir, err)
	}

	// 先把 inventory 里所有 group → hosts 列表读出来，下面查表用
	groupHosts, _ := s.readInventory(project, env)

	out := []Service{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		serviceName := strings.TrimSuffix(e.Name(), ".yaml")
		pbPath := filepath.Join(dir, e.Name())
		group, appName, sourcePath := parsePlaybook(pbPath)
		hosts := groupHosts[group]
		out = append(out, Service{
			Name:         serviceName,
			AnsibleGroup: group,
			Hosts:        hosts,
			AppName:      appName,
			SourcePath:   sourcePath,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// parsePlaybook 解析 playbook yaml 拿关键字段。失败返回空字段，不报错
//
//	playbook 是数组结构 [{ hosts:[...], vars:{...} }]，第一项是主 play
func parsePlaybook(path string) (group, appName, sourcePath string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	type play struct {
		Hosts interface{}            `yaml:"hosts"` // 可能 string 或 []string
		Vars  map[string]interface{} `yaml:"vars"`
	}
	var plays []play
	if err := yaml.Unmarshal(raw, &plays); err != nil || len(plays) == 0 {
		return
	}
	p := plays[0]
	switch h := p.Hosts.(type) {
	case string:
		group = h
	case []interface{}:
		if len(h) > 0 {
			if s, ok := h[0].(string); ok {
				group = s
			}
		}
	}
	if v, ok := p.Vars["app_name"].(string); ok {
		appName = v
	}
	if v, ok := p.Vars["source_path"].(string); ok {
		sourcePath = v
	}
	return
}

// readInventory 解析 ini 风格的 inventory 文件，返回 group → hosts 列表
//
//	支持的简化语法：
//	  [group_name]
//	  10.x.x.x
//	  hostname.foo
//	  10.x.x.x ansible_user=root  ← 取空格前的 IP/host
//	  ; / # 开头是注释
//	  group:children 暂不展开（直接当成普通 group）
func (s *Scanner) readInventory(project, env string) (map[string][]string, error) {
	path := filepath.Join(s.AnsibleRoot, "inventory", project, env,
		fmt.Sprintf("%s_%s_hosts", project, env))
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	groups := map[string][]string{}
	current := ""
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = strings.TrimSpace(line[1 : len(line)-1])
			// 去 :children / :vars 后缀，简化处理
			if i := strings.Index(current, ":"); i > 0 {
				current = current[:i]
			}
			if _, ok := groups[current]; !ok {
				groups[current] = []string{}
			}
			continue
		}
		if current == "" {
			continue
		}
		// 取行首到第一个空格之间的 host
		host := line
		if i := strings.IndexAny(line, " \t"); i > 0 {
			host = line[:i]
		}
		if host != "" {
			groups[current] = append(groups[current], host)
		}
	}
	return groups, scanner.Err()
}
