package services

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

type ScannedModule struct {
	Name            string
	ImageRepository string
	CurrentTag      string
	Namespace       string // 从 -apps/values.yaml 的 argocdApplications.{name}.namespace 读出
}

// appsValuesStructure 结构用于解析 -apps/values.yaml
// 关键字段：argocdApplications.{moduleName}.namespace
// 以及 global.spec.destination.namespace 作为 fallback
type appsValuesStructure struct {
	Global struct {
		Spec struct {
			Destination struct {
				Namespace string `yaml:"namespace"`
			} `yaml:"destination"`
		} `yaml:"spec"`
	} `yaml:"global"`
	ArgocdApplications map[string]struct {
		Namespace string `yaml:"namespace"`
	} `yaml:"argocdApplications"`
}

// ParseAppsNamespaces 读取 -apps/values.yaml 返回 { 模块名: namespace }
// appsValuesPath 指向 chart_base_path 同级的 -apps/values.yaml 文件
func ParseAppsNamespaces(appsValuesPath string) (map[string]string, string, error) {
	raw, err := os.ReadFile(appsValuesPath)
	if err != nil {
		return nil, "", err
	}
	var v appsValuesStructure
	if err := yaml.Unmarshal(raw, &v); err != nil {
		return nil, "", err
	}
	result := map[string]string{}
	for name, app := range v.ArgocdApplications {
		if app.Namespace != "" {
			result[name] = app.Namespace
		}
	}
	return result, v.Global.Spec.Destination.Namespace, nil
}

// ScanModulesFromDir 扫描某 chart_base_path 目录，每个子目录 = 一个模块
// 读子目录下 values.yaml 的 image.repository / image.tag
// 同时尝试读 {chartBaseDir}-apps/values.yaml 获取每个模块的 namespace
func ScanModulesFromDir(chartBaseDir string) ([]ScannedModule, error) {
	entries, err := os.ReadDir(chartBaseDir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", chartBaseDir, err)
	}

	// 尝试解析 -apps/values.yaml 拿每模块的 namespace（失败无所谓，回退空串）
	appsPath := chartBaseDir + "-apps/values.yaml"
	nsMap, defaultNS, _ := ParseAppsNamespaces(appsPath)

	var out []ScannedModule
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		valuesPath := filepath.Join(chartBaseDir, e.Name(), "values.yaml")
		content, err := os.ReadFile(valuesPath)
		if err != nil {
			continue
		}
		repo, tag, err := ReadImage(content)
		if err != nil {
			continue
		}
		ns := nsMap[e.Name()]
		if ns == "" {
			ns = defaultNS
		}
		out = append(out, ScannedModule{
			Name:            e.Name(),
			ImageRepository: repo,
			CurrentTag:      tag,
			Namespace:       ns,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
