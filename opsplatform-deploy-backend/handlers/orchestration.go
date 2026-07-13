package handlers

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/models"
	"opsplatform-deploy-backend/services"
)

// PUT /api/orchestration/env-gateway/{id} —— 更新某项目环境的 ingress 网关名（「项目参数」页用）。
func HandleUpdateEnvGateway(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	var req struct {
		IngressGateway string `json:"ingress_gateway"`
	}
	if !DecodeJSON(w, r, &req) {
		return
	}
	if _, err := database.DB.Exec(`UPDATE project_env SET ingress_gateway=? WHERE id=?`,
		strings.TrimSpace(req.IngressGateway), id); err != nil {
		InternalErr(w, r, err)
		return
	}
	Audit(r, "orchestration.env_gateway.update", "project_env", strconv.FormatInt(id, 10), nil)
	JSONSuccess(w, nil)
}

// cmItem 一个 configmap 文件：相对模块目录的路径 + 内容
type cmItem struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// scanConfigmaps 扫样板服务 templates/ 下 kind: ConfigMap 的文件，令牌替换后返回（前端做 tab 展示）。
func scanConfigmaps(gs *services.GitService, srcEnv, chartBasePath, srcService string, replace func([]byte) []byte) []cmItem {
	dir := filepath.Join(gs.RepoPath(srcEnv), chartBasePath, srcService, "templates")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []cmItem
	for _, e := range entries {
		if e.IsDir() || (!strings.HasSuffix(e.Name(), ".yaml") && !strings.HasSuffix(e.Name(), ".yml")) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil || !strings.Contains(string(raw), "kind: ConfigMap") {
			continue
		}
		out = append(out, cmItem{Path: "templates/" + e.Name(), Content: string(replace(raw))})
	}
	return out
}

// ============ 新增模块（服务编排）：预填 / 预览 / 提交 ============

// loadEnvGit 只取 git 坐标（不加密字段），供编排使用。
func loadEnvGit(where string, arg interface{}) (*models.ProjectEnv, error) {
	var p models.ProjectEnv
	err := database.DB.QueryRow(
		`SELECT id, name, env_type, git_repo, git_branch, chart_base_path, IFNULL(ingress_gateway,'')
		 FROM project_env WHERE `+where, arg).
		Scan(&p.ID, &p.Name, &p.EnvType, &p.GitRepo, &p.GitBranch, &p.ChartBasePath, &p.IngressGateway)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// prefillValues 尽力把样板 values.yaml 里的令牌替换成目标（服务名/环境名/项目段）。
// 顺序：服务名 → 环境全名 → 项目段（避免 g32 先被替换导致 g32-uat / 服务名匹配不到）。
// 这是"建议值"，用户会在编辑器里复核（模式 B）。
func prefillValues(raw []byte, srcEnv, dstEnv *models.ProjectEnv, srcService, moduleName string) []byte {
	s := string(raw)
	srcProj := strings.TrimSuffix(srcEnv.Name, "-"+srcEnv.EnvType)
	dstProj := strings.TrimSuffix(dstEnv.Name, "-"+dstEnv.EnvType)
	if srcService != "" && srcService != moduleName {
		s = strings.ReplaceAll(s, srcService, moduleName)
	}
	if srcEnv.Name != dstEnv.Name {
		s = strings.ReplaceAll(s, srcEnv.Name, dstEnv.Name)
	}
	if srcProj != "" && srcProj != dstProj {
		s = strings.ReplaceAll(s, srcProj, dstProj)
	}
	return []byte(s)
}

// buildSpec 从请求解析出引擎所需的 ModuleSpec（不含 ValuesYAML，由各接口填）。
func buildSpec(templateID, targetEnvID int64, moduleName, namespace string) (services.ModuleSpec, *models.OrchestrationTemplate, *models.ProjectEnv, error) {
	var spec services.ModuleSpec
	tpl, err := LoadTemplate(templateID)
	if err != nil {
		return spec, nil, nil, fmt.Errorf("模板不存在")
	}
	dst, err := loadEnvGit("id=?", targetEnvID)
	if err != nil {
		return spec, nil, nil, fmt.Errorf("目标环境不存在")
	}
	src, err := loadEnvGit("name=?", tpl.SrcEnv)
	if err != nil {
		return spec, nil, nil, fmt.Errorf("样板环境不存在: %s", tpl.SrcEnv)
	}
	spec = services.ModuleSpec{
		TargetEnv:           dst.Name,
		TargetRepoURL:       dst.GitRepo,
		TargetBranch:        dst.GitBranch,
		TargetChartBasePath: dst.ChartBasePath,
		ModuleName:          strings.TrimSpace(moduleName),
		Namespace:           strings.TrimSpace(namespace),
		SrcEnv:              src.Name,
		SrcRepoURL:          src.GitRepo,
		SrcBranch:           src.GitBranch,
		SrcChartBasePath:    src.ChartBasePath,
		SrcService:          tpl.SrcService,
	}
	return spec, tpl, dst, nil
}

func validModuleName(name string) error {
	if name == "" {
		return fmt.Errorf("模块名必填")
	}
	if err := ValidateName(name); err != nil {
		return fmt.Errorf("模块名: %v", err)
	}
	return nil
}

type prefillReq struct {
	TemplateID  int64  `json:"template_id"`
	TargetEnvID int64  `json:"target_env_id"`
	ModuleName  string `json:"module_name"`
}

// POST /api/orchestration/prefill —— 返回预填的 values.yaml 建议 + 派生字段，供前端编辑。
func HandlePrefillModule(w http.ResponseWriter, r *http.Request) {
	var req prefillReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	req.ModuleName = strings.TrimSpace(req.ModuleName)
	if err := validModuleName(req.ModuleName); err != nil {
		JSONError(w, 40001, err.Error())
		return
	}
	spec, tpl, dst, err := buildSpec(req.TemplateID, req.TargetEnvID, req.ModuleName, "")
	if err != nil {
		JSONError(w, 40001, err.Error())
		return
	}
	// 同步样板仓库，读样板 values.yaml
	gs := getGitService()
	ctx, cancel := services.GitCtx(context.Background(), 60)
	defer cancel()
	srcEnv, _ := loadEnvGit("name=?", tpl.SrcEnv)
	if err := gs.EnsureClone(ctx, srcEnv.Name, srcEnv.GitRepo, srcEnv.GitBranch); err != nil {
		InternalErr(w, r, fmt.Errorf("同步样板仓库失败: %w", err))
		return
	}
	raw, err := gs.ReadFile(srcEnv.Name, spec.SrcChartBasePath+"/"+spec.SrcService+"/values.yaml")
	if err != nil {
		JSONError(w, 40400, fmt.Sprintf("样板 values.yaml 不存在: %s/%s", spec.SrcChartBasePath, spec.SrcService))
		return
	}
	filled := prefillValues(raw, srcEnv, dst, spec.SrcService, req.ModuleName)
	// 网关名按目标环境自动带出、域名默认清空（一个模板跨项目复用）
	if f, err := services.ApplyEnvIngress(filled, dst.IngressGateway); err == nil {
		filled = f
	}
	// 扫 templates/ 下的 configmap（多个，按文件名前端做 tab），同样令牌替换
	cms := scanConfigmaps(gs, srcEnv.Name, spec.SrcChartBasePath, spec.SrcService,
		func(b []byte) []byte { return prefillValues(b, srcEnv, dst, spec.SrcService, req.ModuleName) })

	JSONSuccess(w, map[string]interface{}{
		"values_yaml":       string(filled),
		"configmaps":        cms, // [] 表示没有（后端服务通常没有）
		"suggest_namespace": dst.Name,
		"target_chart_path": spec.TargetChartBasePath + "/" + req.ModuleName,
		"apps_file":         spec.TargetChartBasePath + "-apps/values.yaml",
	})
}

type moduleAddReq struct {
	TemplateID  int64  `json:"template_id"`
	TargetEnvID int64  `json:"target_env_id"`
	ModuleName  string   `json:"module_name"`
	Namespace   string   `json:"namespace"`
	ValuesYAML  string   `json:"values_yaml"`
	Configmaps  []cmItem `json:"configmaps"` // 用户编辑过的 configmap（可空）
	Disable     *bool    `json:"disable"`    // 默认 true（安全预演，app-of-apps 先不生成）
}

// cmMap 把 []cmItem 转成 path→content
func cmMap(items []cmItem) map[string]string {
	if len(items) == 0 {
		return nil
	}
	m := map[string]string{}
	for _, it := range items {
		if strings.TrimSpace(it.Path) != "" {
			m[it.Path] = it.Content
		}
	}
	return m
}

func (req *moduleAddReq) toSpec(w http.ResponseWriter) (services.ModuleSpec, *models.ProjectEnv, bool) {
	req.ModuleName = strings.TrimSpace(req.ModuleName)
	req.Namespace = strings.TrimSpace(req.Namespace)
	if err := validModuleName(req.ModuleName); err != nil {
		JSONError(w, 40001, err.Error())
		return services.ModuleSpec{}, nil, false
	}
	if req.Namespace == "" {
		JSONError(w, 40001, "namespace 必填")
		return services.ModuleSpec{}, nil, false
	}
	if strings.TrimSpace(req.ValuesYAML) == "" {
		JSONError(w, 40001, "values.yaml 不能为空")
		return services.ModuleSpec{}, nil, false
	}
	spec, _, dst, err := buildSpec(req.TemplateID, req.TargetEnvID, req.ModuleName, req.Namespace)
	if err != nil {
		JSONError(w, 40001, err.Error())
		return services.ModuleSpec{}, nil, false
	}
	spec.ValuesYAML = []byte(req.ValuesYAML)
	spec.ConfigMaps = cmMap(req.Configmaps)
	disable := true // 默认安全预演
	if req.Disable != nil {
		disable = *req.Disable
	}
	spec.Disable = disable
	return spec, dst, true
}

// POST /api/orchestration/preview —— helm 校验 + diff，不提交。
func HandlePreviewModule(w http.ResponseWriter, r *http.Request) {
	var req moduleAddReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	spec, _, ok := req.toSpec(w)
	if !ok {
		return
	}
	gs := getGitService()
	ctx, cancel := services.GitCtx(context.Background(), 120)
	defer cancel()
	res, err := gs.PreviewNewModule(ctx, spec)
	if err != nil {
		JSONError(w, 40001, err.Error())
		return
	}
	JSONSuccess(w, res)
}

// POST /api/orchestration/submit —— 校验通过后 commit + push。按环境权限 submit_<env> 放行。
func HandleSubmitModule(w http.ResponseWriter, r *http.Request) {
	var req moduleAddReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	spec, dst, ok := req.toSpec(w)
	if !ok {
		return
	}
	// 每环境独立权限档
	perm := envPermissionCode(dst.EnvType)
	if !HasButton(r, perm) {
		JSONError(w, 40300, "没有向环境 "+dst.EnvType+" 提交的权限（需 "+perm+"）")
		return
	}
	operator := UsernameFromCtx(r)
	msg := fmt.Sprintf("feat(%s): add module %s", dst.Name, spec.ModuleName)
	gs := getGitService()
	ctx, cancel := services.GitCtx(context.Background(), 180)
	defer cancel()
	res, err := gs.SubmitNewModule(ctx, spec, operator, msg)
	if err != nil {
		JSONError(w, 40001, err.Error())
		return
	}
	Audit(r, "orchestration.add_module", "module", spec.ModuleName, map[string]interface{}{
		"env": dst.Name, "commit": res.CommitSHA, "disable": spec.Disable,
	})
	JSONSuccess(w, res)
}

// ============ 批量新增 ============

type batchReq struct {
	TemplateID  int64 `json:"template_id"`
	TargetEnvID int64 `json:"target_env_id"`
	Disable     *bool `json:"disable"`
	Rows        []struct {
		ModuleName string   `json:"module_name"`
		Namespace  string   `json:"namespace"`
		ValuesYAML string   `json:"values_yaml"` // 用户在「配置」里自定义了就带上；空=按模板派生
		Configmaps []cmItem `json:"configmaps"`  // 同上，空=派生
	} `json:"rows"`
}

// buildBatch 解析请求 → base spec + 每行(模块名/namespace/派生values)。每行 values 用模板派生预填。
func buildBatch(req batchReq, w http.ResponseWriter) (services.ModuleSpec, []services.BatchRow, *models.ProjectEnv, bool) {
	base, tpl, dst, err := buildSpec(req.TemplateID, req.TargetEnvID, "", "")
	if err != nil {
		JSONError(w, 40001, err.Error())
		return base, nil, nil, false
	}
	src, err := loadEnvGit("name=?", tpl.SrcEnv)
	if err != nil {
		JSONError(w, 40001, "样板环境不存在")
		return base, nil, nil, false
	}
	gs := getGitService()
	ctx, cancel := services.GitCtx(context.Background(), 60)
	defer cancel()
	if err := gs.EnsureClone(ctx, src.Name, src.GitRepo, src.GitBranch); err != nil {
		InternalErr(w, nil, fmt.Errorf("同步样板仓库失败: %w", err))
		return base, nil, nil, false
	}
	raw, err := gs.ReadFile(src.Name, base.SrcChartBasePath+"/"+base.SrcService+"/values.yaml")
	if err != nil {
		JSONError(w, 40400, "样板 values.yaml 不存在")
		return base, nil, nil, false
	}
	if req.Disable != nil {
		base.Disable = *req.Disable
	} else {
		base.Disable = true
	}
	var rows []services.BatchRow
	for _, r := range req.Rows {
		name := strings.TrimSpace(r.ModuleName)
		ns := strings.TrimSpace(r.Namespace)
		if name == "" {
			continue
		}
		if err := validModuleName(name); err != nil {
			JSONError(w, 40001, err.Error())
			return base, nil, nil, false
		}
		if ns == "" {
			JSONError(w, 40001, "namespace 必填（模块 "+name+"）")
			return base, nil, nil, false
		}
		// 用户在「配置」里自定义了 values 就用它，否则按模板派生预填
		vals := []byte(r.ValuesYAML)
		if strings.TrimSpace(r.ValuesYAML) == "" {
			vals = prefillValues(raw, src, dst, base.SrcService, name)
			if f, err := services.ApplyEnvIngress(vals, dst.IngressGateway); err == nil {
				vals = f
			}
		}
		// configmap：自定义了用它，否则按模板派生（令牌替换），保证前端批量的 configmap 令牌也对
		cms := cmMap(r.Configmaps)
		if cms == nil {
			derived := scanConfigmaps(gs, src.Name, base.SrcChartBasePath, base.SrcService,
				func(b []byte) []byte { return prefillValues(b, src, dst, base.SrcService, name) })
			cms = cmMap(derived)
		}
		rows = append(rows, services.BatchRow{
			ModuleName: name,
			Namespace:  ns,
			ValuesYAML: vals,
			ConfigMaps: cms,
		})
	}
	if len(rows) == 0 {
		JSONError(w, 40001, "没有有效的模块行")
		return base, nil, nil, false
	}
	return base, rows, dst, true
}

// POST /api/orchestration/batch-preview
func HandleBatchPreview(w http.ResponseWriter, r *http.Request) {
	var req batchReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	base, rows, _, ok := buildBatch(req, w)
	if !ok {
		return
	}
	gs := getGitService()
	ctx, cancel := services.GitCtx(context.Background(), 300)
	defer cancel()
	res, err := gs.PreviewBatch(ctx, base, rows)
	if err != nil {
		JSONError(w, 40001, err.Error())
		return
	}
	JSONSuccess(w, res)
}

// POST /api/orchestration/batch-submit
func HandleBatchSubmit(w http.ResponseWriter, r *http.Request) {
	var req batchReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	base, rows, dst, ok := buildBatch(req, w)
	if !ok {
		return
	}
	perm := envPermissionCode(dst.EnvType)
	if !HasButton(r, perm) {
		JSONError(w, 40300, "没有向环境 "+dst.EnvType+" 提交的权限（需 "+perm+"）")
		return
	}
	operator := UsernameFromCtx(r)
	msg := fmt.Sprintf("feat(%s): add %d modules (orchestration batch)", dst.Name, len(rows))
	gs := getGitService()
	ctx, cancel := services.GitCtx(context.Background(), 600)
	defer cancel()
	res, err := gs.SubmitBatch(ctx, base, rows, operator, msg)
	if err != nil {
		JSONError(w, 40001, err.Error())
		return
	}
	Audit(r, "orchestration.add_batch", "module", fmt.Sprintf("%d modules", len(rows)), map[string]interface{}{
		"env": dst.Name, "commit": res.CommitSHA, "all_ok": res.AllOK,
	})
	JSONSuccess(w, res)
}

// envPermissionCode 查 deploy_environment.permission_code，回退 submit_<env>。
func envPermissionCode(envType string) string {
	var code string
	_ = database.DB.QueryRow(`SELECT permission_code FROM deploy_environment WHERE name=?`, envType).Scan(&code)
	if strings.TrimSpace(code) != "" {
		return code
	}
	return "submit_" + envType
}
