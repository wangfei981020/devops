package handlers

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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

// PUT /api/orchestration/env-harbor/{id} —— 更新某项目环境的 Harbor 项目名（「项目参数」页用）。
// 留空=新增模块时自动用项目名。
func HandleUpdateEnvHarbor(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	var req struct {
		HarborProject string `json:"harbor_project"`
	}
	if !DecodeJSON(w, r, &req) {
		return
	}
	if _, err := database.DB.Exec(`UPDATE project_env SET harbor_project=? WHERE id=?`,
		strings.TrimSpace(req.HarborProject), id); err != nil {
		InternalErr(w, r, err)
		return
	}
	Audit(r, "orchestration.env_harbor.update", "project_env", strconv.FormatInt(id, 10), nil)
	JSONSuccess(w, nil)
}

// PUT /api/orchestration/env-domain/{id} —— 更新某项目环境的域名后缀（「项目参数」页用）。
// 留空=新增前端模块时域名不自动带出。
func HandleUpdateEnvDomain(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	var req struct {
		DomainSuffix string `json:"domain_suffix"`
	}
	if !DecodeJSON(w, r, &req) {
		return
	}
	if _, err := database.DB.Exec(`UPDATE project_env SET domain_suffix=? WHERE id=?`,
		strings.TrimSpace(req.DomainSuffix), id); err != nil {
		InternalErr(w, r, err)
		return
	}
	Audit(r, "orchestration.env_domain.update", "project_env", strconv.FormatInt(id, 10), nil)
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

// deriveDomainForEnv 推导前端模块的访问域名：<模块名去-frontend>.<环境域名后缀>。
//   只对 -frontend 模块生效；域名后缀没配 → 返回空（域名保持空，不瞎拼）。
func deriveDomainForEnv(dst *models.ProjectEnv, moduleName string) string {
	suffix := strings.TrimSpace(dst.DomainSuffix)
	if suffix == "" {
		return ""
	}
	if !strings.HasSuffix(moduleName, "-frontend") {
		return "" // 只有前端模块才自动带域名
	}
	prefix := strings.TrimSuffix(moduleName, "-frontend") // g66-301game-frontend → g66-301game
	return prefix + "." + strings.TrimLeft(suffix, ".")
}

// deriveImageRepoForEnv 推导镜像仓库：<全局harbor域名>/<Harbor项目(留空=项目名)>/<服务名>。
// 服务名 = 模块名去掉项目前缀（项目名从环境名去 -env 后缀推）。
// 没配全局 harbor 域名 → 返回空，调用方保留模板原值。
func deriveImageRepoForEnv(dst *models.ProjectEnv, moduleName string) string {
	var harborURL string
	_ = database.DB.QueryRow(`SELECT IFNULL(harbor_url,'') FROM global_config WHERE id=1`).Scan(&harborURL)
	domain := services.HarborDomain(harborURL)
	if domain == "" {
		return ""
	}
	project := strings.TrimSuffix(dst.Name, "-"+dst.EnvType) // 项目名，如 g66
	harborProject := strings.TrimSpace(dst.HarborProject)
	if harborProject == "" {
		harborProject = project // 留空=用项目名
	}
	svc := strings.TrimPrefix(moduleName, project+"-") // 去项目前缀得服务名
	return fmt.Sprintf("%s/%s/%s", domain, harborProject, svc)
}

// harborImageStatus 查 Harbor 该镜像仓库有没有镜像 + 最新 tag。
//   checked=false：Harbor 未配置 / 连不上 → 调用方应跳过硬卡（别把人卡死）。
//   missing=true：Harbor 里这个仓库没有任何镜像。latest：有镜像时的最新 tag（按推送时间）。
func harborImageStatus(ctx context.Context, imageRepo string) (checked, missing bool, latest string) {
	hc := getHarborClient()
	if hc == nil || imageRepo == "" {
		return false, false, ""
	}
	_, projectRepo, ok := services.SplitImageRepoForHarbor(imageRepo)
	if !ok {
		return false, false, ""
	}
	c, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	res, err := hc.ListTags(c, projectRepo, 1, 1)
	if err != nil {
		return false, false, "" // 连不上 → 不硬卡
	}
	if len(res.Tags) == 0 {
		return true, true, ""
	}
	return true, false, res.Tags[0].Name
}

// missingImageMsg 缺镜像的统一文案。
func missingImageMsg(moduleName, envType string) string {
	return fmt.Sprintf("需要先同步当前模块 %s 到 %s 的 harbor", moduleName, envType)
}

// verifyImageForSubmit 提交兜底：校验 values 里的 image.repository:tag 在 Harbor 存在。
//   返回 error 表示应拦截提交（缺镜像/缺 tag）；nil 表示放行（含 Harbor 未配/连不上时跳过）。
func verifyImageForSubmit(valuesYAML []byte, moduleName, envType string) error {
	hc := getHarborClient()
	if hc == nil {
		return nil // Harbor 未配 → 跳过校验
	}
	repo, tag := services.GetImageRepoTag(valuesYAML)
	if repo == "" {
		return nil // 没有 image.repository（非标准模板）→ 不管
	}
	_, projectRepo, ok := services.SplitImageRepoForHarbor(repo)
	if !ok {
		return nil
	}
	if tag == "" {
		return fmt.Errorf("%s", missingImageMsg(moduleName, envType))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	exists, err := hc.VerifyTag(ctx, projectRepo, tag)
	if err != nil {
		return nil // 连不上 Harbor → 跳过不卡
	}
	if !exists {
		return fmt.Errorf("%s", missingImageMsg(moduleName, envType))
	}
	return nil
}

// loadEnvGit 只取 git 坐标（不加密字段），供编排使用。
func loadEnvGit(where string, arg interface{}) (*models.ProjectEnv, error) {
	var p models.ProjectEnv
	err := database.DB.QueryRow(
		`SELECT id, name, env_type, git_repo, git_branch, chart_base_path, IFNULL(ingress_gateway,''), IFNULL(harbor_project,''), IFNULL(domain_suffix,'')
		 FROM project_env WHERE `+where, arg).
		Scan(&p.ID, &p.Name, &p.EnvType, &p.GitRepo, &p.GitBranch, &p.ChartBasePath, &p.IngressGateway, &p.HarborProject, &p.DomainSuffix)
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
	// 网关名按目标环境自动带出、域名先清空（一个模板跨项目复用）
	if f, err := services.ApplyEnvIngress(filled, dst.IngressGateway); err == nil {
		filled = f
	}
	// 前端模块访问域名自动带出：<模块名去-frontend>.<环境域名后缀>（没配后缀则保持空，可手改）
	if domain := deriveDomainForEnv(dst, req.ModuleName); domain != "" {
		filled = services.SetIngressHost(filled, domain)
	}
	// 镜像仓库自动推导：全局 harbor 域名 / 该环境 Harbor 项目(留空=项目名) / 服务名。
	// 只是智能默认值，用户可在编辑器里手动改；没配全局 harbor 域名则保留模板原值。
	imageMissing := false
	imageRepo := ""
	if repo := deriveImageRepoForEnv(dst, req.ModuleName); repo != "" {
		filled = services.SetImageRepository(filled, repo)
		imageRepo = repo
		// 查 Harbor：有镜像→自动填最新 tag（同"更新服务"取推送时间最新）；缺镜像→清空 tag + 标记 missing
		checked, missing, latest := harborImageStatus(ctx, repo)
		if checked && missing {
			imageMissing = true
			filled = services.SetImageTag(filled, "")
		} else if checked && latest != "" {
			filled = services.SetImageTag(filled, latest)
		}
	}
	// 缺 global.labels 就补，避免 web.labels 渲染 nil
	filled = services.EnsureGlobalLabels(filled)
	// 扫 templates/ 下的 configmap（多个，按文件名前端做 tab），同样令牌替换
	cms := scanConfigmaps(gs, srcEnv.Name, spec.SrcChartBasePath, spec.SrcService,
		func(b []byte) []byte { return prefillValues(b, srcEnv, dst, spec.SrcService, req.ModuleName) })

	JSONSuccess(w, map[string]interface{}{
		"values_yaml":       string(filled),
		"configmaps":        cms, // [] 表示没有（后端服务通常没有）
		"suggest_namespace": dst.Name,
		"target_chart_path": spec.TargetChartBasePath + "/" + req.ModuleName,
		"apps_file":         spec.TargetChartBasePath + "-apps/values.yaml",
		"image_repository":  imageRepo,
		"image_missing":     imageMissing, // true=Harbor 缺该镜像 → 前端红字禁止提交/预览
		"image_missing_msg": func() string {
			if imageMissing {
				return missingImageMsg(req.ModuleName, dst.EnvType)
			}
			return ""
		}(),
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
	disable := false // 默认直接部署（helm 校验兜底）；显式传 true 才安全预演
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
	// 缺镜像硬卡：Harbor 里没有该 image.repository:tag 就禁止提交（Harbor 未配/连不上则跳过）
	if err := verifyImageForSubmit(spec.ValuesYAML, spec.ModuleName, dst.EnvType); err != nil {
		JSONError(w, 40001, err.Error())
		return
	}
	operator := UsernameFromCtx(r)
	msg := fmt.Sprintf("feat(%s): add module %s", dst.Name, spec.ModuleName)
	// 后台异步执行：立即建任务返回，clone+helm+commit+push 在 goroutine 里跑，结果去「新增历史」看。
	taskID, err := insertOrchTask(dst.ID, dst.Name, spec.ModuleName, "single", operator)
	if err != nil {
		JSONError(w, 50000, "创建任务失败: "+err.Error())
		return
	}
	Audit(r, "orchestration.add_module", "module", spec.ModuleName, map[string]interface{}{
		"env": dst.Name, "task_id": taskID, "disable": spec.Disable,
	})
	InflightTrack(func() {
		gs := getGitService()
		ctx, cancel := services.GitCtx(context.Background(), 300)
		defer cancel()
		res, serr := gs.SubmitNewModule(ctx, spec, operator, msg)
		if serr != nil {
			updateOrchTaskFailed(taskID, serr.Error())
			return
		}
		updateOrchTaskSuccess(taskID, res.CommitSHA, res.CommitURL)
	})
	JSONSuccess(w, map[string]interface{}{"task_id": taskID, "status": "pending"})
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
		base.Disable = false // 默认直接部署
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
			if domain := deriveDomainForEnv(dst, name); domain != "" {
				vals = services.SetIngressHost(vals, domain)
			}
			if repo := deriveImageRepoForEnv(dst, name); repo != "" {
				vals = services.SetImageRepository(vals, repo)
				// 有镜像→带最新 tag；缺镜像→清空(提交时会被硬卡)
				if checked, missing, latest := harborImageStatus(context.Background(), repo); checked {
					if missing {
						vals = services.SetImageTag(vals, "")
					} else if latest != "" {
						vals = services.SetImageTag(vals, latest)
					}
				}
			}
			vals = services.EnsureGlobalLabels(vals)
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
	// 缺镜像硬卡：任一行的镜像在 Harbor 缺失就禁止整批提交
	for _, rr := range rows {
		if err := verifyImageForSubmit(rr.ValuesYAML, rr.ModuleName, dst.EnvType); err != nil {
			JSONError(w, 40001, err.Error())
			return
		}
	}
	operator := UsernameFromCtx(r)
	msg := fmt.Sprintf("feat(%s): add %d modules (orchestration batch)", dst.Name, len(rows))
	modNames := make([]string, 0, len(rows))
	for _, rr := range rows {
		modNames = append(modNames, rr.ModuleName)
	}
	summary := fmt.Sprintf("批量 %d 个: %s", len(rows), strings.Join(modNames, ","))
	taskID, err := insertOrchTask(dst.ID, dst.Name, summary, "batch", operator)
	if err != nil {
		JSONError(w, 50000, "创建任务失败: "+err.Error())
		return
	}
	Audit(r, "orchestration.add_batch", "module", fmt.Sprintf("%d modules", len(rows)), map[string]interface{}{
		"env": dst.Name, "task_id": taskID,
	})
	InflightTrack(func() {
		gs := getGitService()
		ctx, cancel := services.GitCtx(context.Background(), 600)
		defer cancel()
		res, serr := gs.SubmitBatch(ctx, base, rows, operator, msg)
		if serr != nil {
			updateOrchTaskFailed(taskID, serr.Error())
			return
		}
		if !res.AllOK {
			updateOrchTaskFailed(taskID, "部分模块 helm 校验未通过，未提交（请在批量预览里查看具体行）")
			return
		}
		updateOrchTaskSuccess(taskID, res.CommitSHA, res.CommitURL)
	})
	JSONSuccess(w, map[string]interface{}{"task_id": taskID, "status": "pending"})
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
