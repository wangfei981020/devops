package services

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResolveAppNameSuffix 覆盖生产里出现过的两种命名约定 + 三种回退场景
func TestResolveAppNameSuffix(t *testing.T) {
	tmp := t.TempDir()
	g := &GitService{CacheDir: tmp}

	// 用 envName "g32-uat" / "g50-uat" 模拟两个项目
	cases := []struct {
		name           string
		envName        string
		chartBasePath  string
		appsTemplate   string
		appsValues     string
		expected       string
		desc           string
	}{
		{
			name:          "g32-style-uses-global.env",
			envName:       "g32-uat",
			chartBasePath: "argocd-apps/charts/g32-uat",
			appsTemplate: `{{- range $app := .Values.argocdApplications -}}
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: {{ .name }}-{{ $.Values.global.env }}
  namespace: argocd
{{ end }}`,
			appsValues: `global:
  env: uat
  helmDefault: false
argocdApplications:
  foo: { name: foo }
`,
			expected: "uat",
			desc:     "新格式 g32：模板用 global.env → 后缀 uat",
		},
		{
			name:          "g50-style-uses-spec.project",
			envName:       "g50-uat",
			chartBasePath: "argocd-apps/charts/g50-uat",
			appsTemplate: `{{- range $app := .Values.argocdApplications -}}
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: {{ .name }}-{{ $.Values.global.spec.project }}
  namespace: argocd
{{ end }}`,
			appsValues: `global:
  helmDefault: false
  spec:
    project: g50-uat
argocdApplications:
  foo: { name: foo }
`,
			expected: "g50-uat",
			desc:     "老格式 g50：模板用 global.spec.project（嵌套字段）→ 后缀 g50-uat",
		},
		{
			name:          "fallback-when-template-missing",
			envName:       "noapps-uat",
			chartBasePath: "argocd-apps/charts/noapps-uat",
			appsTemplate:  "", // 不写模板文件
			appsValues:    "",
			expected:      "noapps-uat",
			desc:          "无 app-of-apps 模式 → 回退 ToLower(envName)",
		},
		{
			name:          "fallback-when-name-pattern-not-match",
			envName:       "weird-uat",
			chartBasePath: "argocd-apps/charts/weird-uat",
			appsTemplate: `metadata:
  name: {{ $app.name | default $name }}    # 裸名（sandbox sample 写法）
`,
			appsValues: `global:
  env: weird
`,
			expected: "weird-uat",
			desc:     "模板 name 表达式不是 {{ .name }}-{{ ... }} 形式 → 回退",
		},
		{
			name:          "fallback-when-values-missing-key",
			envName:       "broken-uat",
			chartBasePath: "argocd-apps/charts/broken-uat",
			appsTemplate: `metadata:
  name: {{ .name }}-{{ $.Values.global.env }}
`,
			appsValues: `global:
  helmDefault: false
  # env 缺失！
`,
			expected: "broken-uat",
			desc:     "模板引用 global.env 但 values 里没这字段 → 回退",
		},
		{
			name:          "trim-marker-in-template",
			envName:       "trim-uat",
			chartBasePath: "argocd-apps/charts/trim-uat",
			appsTemplate: `metadata:
  name: {{- .name -}}-{{- $.Values.global.env -}}
`,
			appsValues: `global:
  env: trimmed
`,
			expected: "trimmed",
			desc:     "helm trim 标记 {{- ... -}} 也能匹配",
		},
		{
			name:          "uppercase-suffix-normalized-to-lower",
			envName:       "upper-uat",
			chartBasePath: "argocd-apps/charts/upper-uat",
			appsTemplate: `metadata:
  name: {{ .name }}-{{ $.Values.global.env }}
`,
			appsValues: `global:
  env: PROD
`,
			expected: "prod",
			desc:     "values 里大写 → ResolveAppNameSuffix 自动小写化",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// 在 tmp/{envName}/{chartBasePath}-apps/{templates,values.yaml} 写文件
			repoPath := g.RepoPath(c.envName)
			appsDir := filepath.Join(repoPath, c.chartBasePath+"-apps")
			if c.appsTemplate != "" {
				_ = os.MkdirAll(filepath.Join(appsDir, "templates"), 0o755)
				_ = os.WriteFile(filepath.Join(appsDir, "templates", "applications.yaml"), []byte(c.appsTemplate), 0o644)
			}
			if c.appsValues != "" {
				_ = os.MkdirAll(appsDir, 0o755)
				_ = os.WriteFile(filepath.Join(appsDir, "values.yaml"), []byte(c.appsValues), 0o644)
			}

			got := g.ResolveAppNameSuffix(c.envName, c.chartBasePath)
			if got != c.expected {
				t.Errorf("%s\n  expected: %q\n  got:      %q", c.desc, c.expected, got)
			}
		})
	}
}
