package services

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type ScannedModule struct {
	Name            string
	ImageRepository string
	CurrentTag      string
}

// ScanModulesFromDir 扫描某 chart_base_path 目录，每个子目录 = 一个模块
// 读子目录下 values.yaml 的 image.repository / image.tag
func ScanModulesFromDir(chartBaseDir string) ([]ScannedModule, error) {
	entries, err := os.ReadDir(chartBaseDir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", chartBaseDir, err)
	}
	var out []ScannedModule
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		valuesPath := filepath.Join(chartBaseDir, e.Name(), "values.yaml")
		content, err := os.ReadFile(valuesPath)
		if err != nil {
			continue // 没 values.yaml 跳过
		}
		repo, tag, err := ReadImage(content)
		if err != nil {
			continue
		}
		out = append(out, ScannedModule{
			Name:            e.Name(),
			ImageRepository: repo,
			CurrentTag:      tag,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
