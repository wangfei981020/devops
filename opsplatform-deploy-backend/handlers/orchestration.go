package handlers

import (
	"context"
	"fmt"
	"log"
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

// PUT /api/orchestration/env-namespaces/{id} —— 更新某项目环境的 namespace 列表（「项目参数」页用）。
func HandleUpdateEnvNamespaces(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	var req struct {
		DefaultNamespaces string `json:"default_namespaces"`
	}
	if !DecodeJSON(w, r, &req) {
		return
	}
	if _, err := database.DB.Exec(`UPDATE project_env SET default_namespaces=? WHERE id=?`,
		strings.TrimSpace(req.DefaultNamespaces), id); err != nil {
		InternalErr(w, r, err)
		return
	}
	Audit(r, "orchestration.env_namespaces.update", "project_env", strconv.FormatInt(id, 10), nil)
	JSONSuccess(w, nil)
}

// PUT /api/orchestration/env-zkv-path/{id} —— 更新某项目环境的 z-kv-secrets 路径（「项目参数」页用）。
func HandleUpdateEnvZkvPath(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	var req struct {
		ZkvSecretsPath string `json:"zkv_secrets_path"`
	}
	if !DecodeJSON(w, r, &req) {
		return
	}
	if _, err := database.DB.Exec(`UPDATE project_env SET zkv_secrets_path=? WHERE id=?`,
		strings.TrimSpace(req.ZkvSecretsPath), id); err != nil {
		InternalErr(w, r, err)
		return
	}
	Audit(r, "orchestration.env_zkv_path.update", "project_env", strconv.FormatInt(id, 10), nil)
	JSONSuccess(w, nil)
}

// PUT /api/orchestration/env-secret-prefix/{id} —— 更新某项目环境的密钥前缀覆盖（「项目参数」页用）。
// 留空=按项目名自动转小写；填了=固定用它(如 g33 复用 g32 填 g32，密钥保持 g32-*)。存小写，去空白。
func HandleUpdateEnvSecretPrefix(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	var req struct {
		SecretPrefix string `json:"secret_prefix"`
	}
	if !DecodeJSON(w, r, &req) {
		return
	}
	prefix := strings.ToLower(strings.TrimSpace(req.SecretPrefix))
	if _, err := database.DB.Exec(`UPDATE project_env SET secret_prefix=? WHERE id=?`, prefix, id); err != nil {
		InternalErr(w, r, err)
		return
	}
	Audit(r, "orchestration.env_secret_prefix.update", "project_env", strconv.FormatInt(id, 10), nil)
	JSONSuccess(w, nil)
}

// allProjectPrefixes 收集所有已登记项目名（project_env 去 -env 后缀），供跨项目 secret 改前缀用。
// 一律转小写：secret 名（k8s 资源名）都是小写，项目名可能存成大写(如 G32-uat)，不转小写会匹配不上。
func allProjectPrefixes() map[string]bool {
	set := map[string]bool{}
	rows, err := database.DB.Query(`SELECT name, env_type FROM project_env`)
	if err != nil {
		return set
	}
	defer rows.Close()
	for rows.Next() {
		var name, envType string
		if rows.Scan(&name, &envType) != nil {
			continue
		}
		proj := strings.ToLower(strings.TrimSuffix(name, "-"+envType))
		if proj != "" {
			set[proj] = true
		}
	}
	return set
}

// effectivePrefix 该环境的「密钥项目前缀」：配了 secret_prefix 用配的(历史特例，如 g33 复用 g32 填 g32)，
// 否则 = 项目名去环境后缀再转小写(G50-uat → g50、pa-re-uat → pa-re)。跨项目复用换密钥前缀时的目标前缀。
func effectivePrefix(p *models.ProjectEnv) string {
	if s := strings.ToLower(strings.TrimSpace(p.SecretPrefix)); s != "" {
		return s
	}
	return strings.ToLower(strings.TrimSuffix(p.Name, "-"+p.EnvType))
}

// PUT /api/orchestration/env-at-larks/{id} —— 更新某项目环境的固定艾特人（Lark ID 列表，「项目参数」页用）。
func HandleUpdateEnvAtLarks(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	var req struct {
		AtLarkIDs string `json:"at_lark_ids"`
	}
	if !DecodeJSON(w, r, &req) {
		return
	}
	if _, err := database.DB.Exec(`UPDATE project_env SET at_lark_ids=? WHERE id=?`,
		strings.TrimSpace(req.AtLarkIDs), id); err != nil {
		InternalErr(w, r, err)
		return
	}
	Audit(r, "orchestration.env_at_larks.update", "project_env", strconv.FormatInt(id, 10), nil)
	JSONSuccess(w, nil)
}

// zkvSecretsPathForEnv 该环境 z-kv-secrets 的 chart 路径：配了用配的；留空=自动推 <chart_base_path>/z-kv-secrets。
func zkvSecretsPathForEnv(p *models.ProjectEnv) string {
	if s := strings.TrimSpace(p.ZkvSecretsPath); s != "" {
		return s
	}
	return strings.TrimRight(p.ChartBasePath, "/") + "/z-kv-secrets"
}

// pendingSecret 一条「引用了但 z-kv 里还没有」的 secret —— 需用户填内容后追加到 z-kv-secrets。
type pendingSecret struct {
	Name      string        `json:"name"`
	Type      string        `json:"type"`      // "tidb" | "opaque"
	Namespace string        `json:"namespace"` // 建议=模块 namespace（提交时带进 secret 条目）
	Database  string        `json:"database"`  // tidb 用，默认空
	Extra     []services.KV `json:"extra"`     // tidb 默认带 TIDB_PWDSALT/PWDCRYPT（复用环境公共）
}

// secretRefsOut 预填时返回给前端的「密钥引用分类」。
type secretRefsOut struct {
	ZkvPath  string          `json:"zkv_path"`  // z-kv-secrets/values.yaml 完整路径（展示用）
	ZkvFound bool            `json:"zkv_found"` // 路径是否找到 & 可读
	Existing []string        `json:"existing"`  // 引用且已存在（复用，不填）
	Pending  []pendingSecret `json:"pending"`   // 引用但缺（需填内容 → 追加到 z-kv）
}

// buildSecretRefs 扫服务 values 的 extraEnvVars，对照目标环境 z-kv-secrets 现有内容分「已存在/待新建」。
// 非后端(无 extraEnvVars) → 返回空壳（前端不显示密钥区）。
func buildSecretRefs(ctx context.Context, gs *services.GitService, dst *models.ProjectEnv, serviceValues []byte) secretRefsOut {
	out := secretRefsOut{ZkvPath: zkvSecretsPathForEnv(dst) + "/values.yaml", Existing: []string{}, Pending: []pendingSecret{}}
	refs := services.ExtraEnvVarNames(serviceValues)
	if len(refs) == 0 {
		return out
	}
	var tidbSchema []services.KV // 现有 tidb 密钥的字段模板（自动识别 key；值一致的预填、不一致的留空）
	existset := map[string]bool{}
	tidbSuffixes := map[string]bool{}
	zkvPath := zkvSecretsPathForEnv(dst)
	if err := gs.EnsureClone(ctx, dst.Name, dst.GitRepo, dst.GitBranch); err == nil {
		if raw, err := gs.ReadFile(dst.Name, zkvPath+"/values.yaml"); err == nil {
			out.ZkvFound = true
			for _, n := range services.ZkvSecretNames(raw) {
				existset[n] = true
			}
			tidbSchema = services.ZkvTidbKeySchema(raw)
			tidbSuffixes = services.ZkvTidbSuffixSet(raw)
		}
	}
	log.Printf("[orch] secret 分类 env=%s zkv=%s found=%v refs=%d existing=%d tidb字段=%d",
		dst.Name, zkvPath, out.ZkvFound, len(refs), len(existset), len(tidbSchema))
	nsSuggest := defaultNamespaceForEnv(dst)
	for _, ref := range refs {
		if existset[ref] {
			out.Existing = append(out.Existing, ref)
			continue
		}
		p := pendingSecret{Name: ref, Namespace: nsSuggest, Type: "opaque"}
		if services.ClassifyTidb(ref, tidbSuffixes) { // 按 z-kv 现有 tidbSecrets 后缀识别(含 uid-secret)
			p.Type = "tidb"
			p.Extra = tidbKeySchemaOrDefault(tidbSchema)
		}
		out.Pending = append(out.Pending, p)
	}
	return out
}

// tidbKeySchemaOrDefault 自动识别到字段就用；识别不到(全新环境无现有 tidb)兜底给 salt/crypt 两个空字段。
func tidbKeySchemaOrDefault(schema []services.KV) []services.KV {
	if len(schema) > 0 {
		out := make([]services.KV, len(schema))
		copy(out, schema)
		return out
	}
	return []services.KV{{Key: "TIDB_PWDSALT", Value: ""}, {Key: "TIDB_PWDCRYPT", Value: ""}}
}

// parseNamespaces 把配置的 namespace 列表(换行/逗号/空格分隔)拆成去重的切片。
func parseNamespaces(raw string) []string {
	f := func(c rune) bool { return c == '\n' || c == '\r' || c == ',' || c == ' ' || c == '\t' }
	seen := map[string]bool{}
	var out []string
	for _, s := range strings.FieldsFunc(raw, f) {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// defaultNamespaceForEnv 该环境新增模块的默认 namespace：配了列表用第一个，否则用环境名。
func defaultNamespaceForEnv(p *models.ProjectEnv) string {
	if ns := parseNamespaces(p.DefaultNamespaces); len(ns) > 0 {
		return ns[0]
	}
	return p.Name
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

// HandleDeriveModules POST /api/orchestration/derive —— 轻量派生（不碰 git）：
// 给一批模块名，返回每个的 镜像仓库/最新tag/是否缺镜像/域名 + 该环境 namespace 列表 + 默认 namespace。
// 批量表格「解析成行」时调，用来自动填 namespace + 只读展示镜像仓库/tag。
func HandleDeriveModules(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TargetEnvID int64    `json:"target_env_id"`
		TemplateID  int64    `json:"template_id"` // 用来判模板是否开 ingress(决定要不要带域名)
		ModuleNames []string `json:"module_names"`
	}
	if !DecodeJSON(w, r, &req) {
		return
	}
	dst, err := loadEnvGit("id=?", req.TargetEnvID)
	if err != nil {
		JSONError(w, 40400, "目标环境不存在")
		return
	}
	if !IsEnvIDAllowed(r, dst.ID) {
		JSONError(w, 40300, "无权访问该环境")
		return
	}
	ctx, cancel := services.GitCtx(r.Context(), 30)
	defer cancel()
	// 读模板样板 values，判断是否开了对外访问（决定域名列要不要带）
	ingressEnabled := templateIngressEnabled(ctx, req.TemplateID)

	type modOut struct {
		ModuleName      string `json:"module_name"`
		ImageRepository string `json:"image_repository"` // 完整(带域名)
		ImageShort      string `json:"image_short"`      // 项目/服务(界面显示)
		LatestTag       string `json:"latest_tag"`
		ImageMissing    bool   `json:"image_missing"`
		Domain          string `json:"domain"`
	}
	mods := make([]modOut, 0, len(req.ModuleNames))
	for _, name := range req.ModuleNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		repo := deriveImageRepoForEnv(dst, name)
		m := modOut{ModuleName: name, ImageRepository: repo, ImageShort: harborShortRepo(repo), Domain: deriveDomainForEnv(dst, name, ingressEnabled)}
		if repo != "" {
			if checked, missing, latest := harborImageStatus(ctx, repo); checked {
				m.ImageMissing = missing
				m.LatestTag = latest
			}
		}
		mods = append(mods, m)
	}
	JSONSuccess(w, map[string]interface{}{
		"namespaces":        parseNamespaces(dst.DefaultNamespaces),
		"default_namespace": defaultNamespaceForEnv(dst),
		"modules":           mods,
		// 解析成行时就把「待新建密钥」算出来（派生每模块 values → 对照 z-kv），前端立刻显示填 database。
		"secret_refs": derivePendingSecrets(ctx, dst, req.TemplateID, req.ModuleNames),
	})
}

// derivePendingSecrets 解析成行用：按批量口径派生每个模块的 values(prefillValues + 换前缀) → 对照目标环境
// z-kv 分「已存在(复用)/待新建(带归属模块/字段)」。best-effort：模板/仓库读不到就返回空，不影响解析。
func derivePendingSecrets(ctx context.Context, dst *models.ProjectEnv, templateID int64, names []string) batchSecretsOut {
	base, tpl, _, err := buildSpec(templateID, dst.ID, "", "")
	if err != nil {
		return batchSecretsOut{}
	}
	src, err := loadEnvGit("name=?", tpl.SrcEnv)
	if err != nil {
		return batchSecretsOut{}
	}
	gs := getGitService()
	if err := gs.EnsureClone(ctx, src.Name, src.GitRepo, src.GitBranch); err != nil {
		return batchSecretsOut{}
	}
	raw, err := gs.ReadFile(src.Name, base.SrcChartBasePath+"/"+base.SrcService+"/values.yaml")
	if err != nil {
		return batchSecretsOut{}
	}
	rows := make([]services.BatchRow, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		vals := prefillValues(raw, src, dst, base.SrcService, name)
		// 跟单个一致：把 extraEnvVars 里任意已知项目前缀(g32/g33…)统一换成目标项目(g50)
		vals = services.RenameSecretRefs(vals, allProjectPrefixes(), effectivePrefix(dst))
		rows = append(rows, services.BatchRow{
			ModuleName: name,
			Namespace:  defaultNamespaceForEnv(dst),
			ValuesYAML: vals,
		})
	}
	return collectBatchPending(ctx, gs, dst, rows)
}

// templateIngressEnabled 读模板样板服务 values.yaml，判断是否开了对外访问(ingressGateway)。
func templateIngressEnabled(ctx context.Context, templateID int64) bool {
	if templateID <= 0 {
		return false
	}
	tpl, err := LoadTemplate(templateID)
	if err != nil {
		return false
	}
	src, err := loadEnvGit("name=?", tpl.SrcEnv)
	if err != nil {
		return false
	}
	gs := getGitService()
	if err := gs.EnsureClone(ctx, src.Name, src.GitRepo, src.GitBranch); err != nil {
		return false
	}
	raw, err := gs.ReadFile(src.Name, src.ChartBasePath+"/"+tpl.SrcService+"/values.yaml")
	if err != nil {
		return false
	}
	return services.IngressEnabled(raw)
}

// deriveDomainForEnv 推导访问域名：<模块名去 -frontend/-backend 后缀>.<环境域名后缀>。
//   ingressEnabled=模板是否开了对外访问(ingressGateway.enabled)——开了才带域名(前端/后端都算)。
//   域名后缀没配 → 返回空。
func deriveDomainForEnv(dst *models.ProjectEnv, moduleName string, ingressEnabled bool) string {
	doms := parseNamespaces(dst.DomainSuffix) // 主域名列表(多个)，同 namespace 格式(换行/逗号)
	if len(doms) == 0 || !ingressEnabled {
		return ""
	}
	// 非生产：取第一个主域名推 <模块名去后缀>.<主域名>（生产走「按数量生成占位」，不在这自动推）
	return stripTypeSuffix(moduleName) + "." + strings.TrimLeft(doms[0], ".")
}

// stripTypeSuffix 去掉模块名的 -frontend/-backend 后缀，得域名前缀。
func stripTypeSuffix(m string) string {
	for _, s := range []string{"-frontend", "-backend"} {
		if strings.HasSuffix(m, s) {
			return strings.TrimSuffix(m, s)
		}
	}
	return m
}

// ingressEnabledFromValues 判断模板 values 是否开了对外访问：
//   有 ingressGateway 且 enabled 不为 false → true（前端一般 true；后端配了 ingress 也 true）。
func ingressEnabledFromValues(raw []byte) bool {
	return services.IngressEnabled(raw)
}

// harborShortRepo 去掉镜像仓库的域名前缀，只留 项目/服务（界面显示用）。
//   harbor.slileisure.com/g32/301game-frontend → g32/301game-frontend
func harborShortRepo(full string) string {
	if i := strings.Index(full, "/"); i >= 0 {
		return full[i+1:]
	}
	return full
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
		`SELECT id, name, env_type, git_repo, git_branch, chart_base_path, IFNULL(ingress_gateway,''), IFNULL(harbor_project,''), IFNULL(domain_suffix,''), IFNULL(default_namespaces,''), IFNULL(zkv_secrets_path,''), IFNULL(at_lark_ids,''), IFNULL(secret_prefix,'')
		 FROM project_env WHERE `+where, arg).
		Scan(&p.ID, &p.Name, &p.EnvType, &p.GitRepo, &p.GitBranch, &p.ChartBasePath, &p.IngressGateway, &p.HarborProject, &p.DomainSuffix, &p.DefaultNamespaces, &p.ZkvSecretsPath, &p.AtLarkIDs, &p.SecretPrefix)
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
	// 项目段一律走 effectivePrefix：转小写(匹配小写的 secret 名) + 支持前缀覆盖(g33 复用 g32 时目标仍是 g32，不误换)
	srcProj := effectivePrefix(srcEnv)
	dstProj := effectivePrefix(dstEnv)
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
	// 访问域名：非生产自动带 <模块名去后缀>.<第一个主域名> 单个；生产留空（前端按数量生成占位再手改）。
	isProd := dst.EnvType == "prod"
	domains := []string{}
	if !isProd {
		if domain := deriveDomainForEnv(dst, req.ModuleName, ingressEnabledFromValues(raw)); domain != "" {
			domains = []string{domain}
		}
	}
	filled = services.SetIngressHosts(filled, domains)
	// 镜像仓库自动推导：全局 harbor 域名 / 该环境 Harbor 项目(留空=项目名) / 服务名。
	// 只是智能默认值，用户可在编辑器里手动改；没配全局 harbor 域名则保留模板原值。
	imageMissing := false
	imageRepo := ""
	latestTag := ""
	if repo := deriveImageRepoForEnv(dst, req.ModuleName); repo != "" {
		filled = services.SetImageRepository(filled, repo)
		imageRepo = repo
		// 查 Harbor：有镜像→自动填最新 tag（同"更新服务"取推送时间最新）；缺镜像→清空 tag + 标记 missing
		checked, missing, latest := harborImageStatus(ctx, repo)
		if checked && missing {
			imageMissing = true
			filled = services.SetImageTag(filled, "")
		} else if checked && latest != "" {
			latestTag = latest
			filled = services.SetImageTag(filled, latest)
		}
	}
	// 缺 global.labels 就补，避免 web.labels 渲染 nil
	filled = services.EnsureGlobalLabels(filled)
	// 跨项目复用模板：把 extraEnvVars 引用的 secret 名的项目前缀换成目标项目（含别项目前缀，如 g33→g50）
	filled = services.RenameSecretRefs(filled, allProjectPrefixes(), effectivePrefix(dst))
	// 后端专属密钥分类：服务 extraEnvVars 引用 vs 目标环境 z-kv-secrets 现有（已存在复用 / 待新建填内容）
	secretRefs := buildSecretRefs(ctx, gs, dst, filled)
	// 扫 templates/ 下的 configmap（多个，按文件名前端做 tab），同样令牌替换
	cms := scanConfigmaps(gs, srcEnv.Name, spec.SrcChartBasePath, spec.SrcService,
		func(b []byte) []byte { return prefillValues(b, srcEnv, dst, spec.SrcService, req.ModuleName) })

	JSONSuccess(w, map[string]interface{}{
		"values_yaml":       string(filled),
		"configmaps":        cms, // [] 表示没有（后端服务通常没有）
		"suggest_namespace": defaultNamespaceForEnv(dst),
		"namespaces":        parseNamespaces(dst.DefaultNamespaces), // 下拉可选列表
		"target_chart_path": spec.TargetChartBasePath + "/" + req.ModuleName,
		"apps_file":         spec.TargetChartBasePath + "-apps/values.yaml",
		"secret_refs":       secretRefs, // 后端专属密钥分类（已存在复用 / 待新建填内容）
		"image_repository":  imageRepo,
		"image_short":       harborShortRepo(imageRepo),                                          // 去域名短显示：g32/301game-frontend
		"latest_tag":        latestTag,                                                            // 自动带出的最新 tag（缺镜像为空）
		"domains":           domains,                                        // 访问域名列表(非生产自动带1个/生产空)；前端可加删改
		"primary_domains":   parseNamespaces(dst.DomainSuffix),              // 主域名列表(生产按数量生成占位域名用)
		"is_prod":           isProd,                                         // 生产环境：前端走"按数量生成占位域名"
		"ingress_enabled":   ingressEnabledFromValues(raw),                  // 模板是否开 ingress(开了才有域名)
		"env_at_lark_ids":   parseNamespaces(dst.AtLarkIDs),                 // 该环境固定艾特人(Lark ID)，弹窗只读展示
		"image_missing":     imageMissing,                                                         // true=Harbor 缺该镜像 → 前端红字禁止提交/预览
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
	ModuleName  string          `json:"module_name"`
	Namespace   string          `json:"namespace"`
	ValuesYAML  string          `json:"values_yaml"`
	Configmaps    []cmItem       `json:"configmaps"`      // 用户编辑过的 configmap（可空）
	NewSecrets    []newSecretReq `json:"new_secrets"`     // 表单模式：待新建 secret（tidb / opaque）
	NewSecretsYAML string        `json:"new_secrets_yaml"` // YAML 模式：直接编辑的片段（非空时优先）
	Domains       []string       `json:"domains"`         // 访问域名列表（前端域名区编辑后的，覆盖 values 的 ingressGateway.host）
	AtLarkIDs     []string       `json:"at_lark_ids"`     // 本次临时额外艾特人的 Lark ID（新增弹窗选的通知人）
	Disable       *bool          `json:"disable"`         // 默认 true（安全预演，app-of-apps 先不生成）
}

// newSecretReq 前端提交的一条待新建 secret（tidb 或 普通 Opaque）。
type newSecretReq struct {
	Name      string        `json:"name"`
	Type      string        `json:"type"` // tidb | opaque
	Namespace string        `json:"namespace"`
	Database  string        `json:"database"` // tidb 用
	Extra     []services.KV `json:"extra"`    // tidb=extraStringData / opaque=键值对(明文 stringData)
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
	// 域名区编辑后的访问域名覆盖进 values 的 ingressGateway.host（多条；无 ingress 的模板原样不变）
	spec.ValuesYAML = services.SetIngressHosts(spec.ValuesYAML, req.Domains)
	spec.ConfigMaps = cmMap(req.Configmaps)
	// 后端专属密钥：往目标环境 z-kv-secrets 追加（路径按项目参数配/自动推）
	spec.ZkvSecretsPath = zkvSecretsPathForEnv(dst)
	if strings.TrimSpace(req.NewSecretsYAML) != "" {
		// YAML 模式：整段片段交给引擎并入（helm 校验兜底格式）
		spec.NewSecretsYAML = req.NewSecretsYAML
	} else {
		for _, s := range req.NewSecrets {
			name := strings.TrimSpace(s.Name)
			ns := strings.TrimSpace(s.Namespace)
			if ns == "" {
				ns = req.Namespace // 默认跟随模块 namespace
			}
			switch s.Type {
			case "tidb":
				if strings.TrimSpace(s.Database) == "" {
					JSONError(w, 40001, fmt.Sprintf("专属密钥 %s 的 database 必填", name))
					return services.ModuleSpec{}, nil, false
				}
				spec.NewTidbSecrets = append(spec.NewTidbSecrets, services.TidbSecretEntry{
					Name: name, Namespace: ns, Database: strings.TrimSpace(s.Database), Extra: s.Extra,
				})
			case "opaque":
				spec.NewPlainSecrets = append(spec.NewPlainSecrets, services.PlainSecretEntry{
					Name: name, Namespace: ns, Type: "Opaque", KVs: s.Extra,
				})
			}
		}
	}
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
	// 后台异步执行：立即建任务返回。git(clone+helm+commit+push) → 真部署则轮询 ArgoCD 新 app 到就绪。
	taskID, err := insertOrchTask(dst.ID, dst.Name, spec.ModuleName, "single", operator, spec.Disable)
	if err != nil {
		JSONError(w, 50000, "创建任务失败: "+err.Error())
		return
	}
	Audit(r, "orchestration.add_module", "module", spec.ModuleName, map[string]interface{}{
		"env": dst.Name, "task_id": taskID, "disable": spec.Disable,
	})
	envID := dst.ID
	tempAt := req.AtLarkIDs // 本次临时额外艾特人
	InflightTrack(func() {
		start := time.Now()
		gs := getGitService()
		gctx, cancel := services.GitCtx(context.Background(), 300)
		res, serr := gs.SubmitNewModule(gctx, spec, operator, msg)
		cancel()
		if serr != nil {
			log.Printf("⚠ [orch-fail] task=%d env=%s module=%s git/helm 阶段失败: %v", taskID, dst.Name, spec.ModuleName, serr)
			updateOrchTaskFailed(taskID, serr.Error())
			return
		}
		setOrchTaskCommit(taskID, res.CommitSHA, res.CommitURL)
		if spec.Disable {
			finishOrchTask(taskID, "success", "已提交（预演 disable:true，未部署）", nil, dur(start))
			return
		}
		// 真部署 → 轮询新模块的 ArgoCD app 到 Synced+Healthy，成功/失败发 Lark
		_, version := services.GetImageRepoTag(spec.ValuesYAML)
		domains := services.GetIngressHosts(spec.ValuesYAML) // 前端模块的访问域名，Lark 卡片列出
		deployAndPollNewModule(taskID, envID, operator, spec.ModuleName, spec.Namespace, version, domains, tempAt, start)
	})
	JSONSuccess(w, map[string]interface{}{"task_id": taskID, "status": "pending"})
}

// ============ 批量新增 ============

type batchReq struct {
	TemplateID     int64 `json:"template_id"`
	TargetEnvID    int64 `json:"target_env_id"`
	Disable        *bool    `json:"disable"`
	AtLarkIDs      []string `json:"at_lark_ids"`      // 本次临时额外艾特人的 Lark ID
	// 表单模式：待新建密钥逐条填的内容（名字+类型+database+字段/键值对）。
	SecretFills []batchSecretFill `json:"secret_fills"`
	// YAML 模式：直接粘的 z-kv secrets/tidbSecrets 片段（非空时优先，跳过表单）。
	SecretsYAML string `json:"secrets_yaml"`
	Rows        []struct {
		ModuleName string   `json:"module_name"`
		Namespace  string   `json:"namespace"`
		ValuesYAML string   `json:"values_yaml"` // 用户在「配置」里自定义了就带上；空=按模板派生
		Configmaps []cmItem `json:"configmaps"`  // 同上，空=派生
	} `json:"rows"`
}

// batchSecretFill 前端填的一条待新建密钥内容：名字 + 类型 + namespace + database(tidb) + 字段/键值对 KV。
// 既覆盖检测到的待新建，也覆盖用户「+新增密钥」手动加的（手动的不在 collectBatchPending 里）。
type batchSecretFill struct {
	Name      string        `json:"name"`
	Type      string        `json:"type"` // tidb | opaque
	Namespace string        `json:"namespace"`
	Database  string        `json:"database"`
	Fields    []services.KV `json:"fields"`
}

// batchPendingSecret 批量新增里一条「引用了但目标环境 z-kv 还没有」的待新建密钥（跨行汇总去重）。
// Modules = 引用它的模块列表：=1=模块专属(挂该模块名下)；>1=多模块同名引用=环境共享，只建一次。
// Fields = tidb 密钥的字段模板（按现有 tidb 自动识别 key；值一致预填、不一致留空）；opaque 为空(前端自己加键值对)。
type batchPendingSecret struct {
	Name      string        `json:"name"`
	Type      string        `json:"type"`      // tidb | opaque
	Namespace string        `json:"namespace"` // 首个引用它的模块的 namespace
	Modules   []string      `json:"modules"`
	Fields    []services.KV `json:"fields,omitempty"`
}

// batchSecretsOut 批量的密钥分类：已存在(复用) + 待新建(带归属模块/类型/字段)。
type batchSecretsOut struct {
	Existing []string             `json:"existing"`
	Pending  []batchPendingSecret `json:"pending"`
}

// collectBatchPending 读一次目标环境 z-kv，扫每行 values 的 extraEnvVars，分「已存在(复用)/待新建」，
// 待新建带归属模块 + tidb 自动识别字段。前提：rows 的 values 已经过 RenameSecretRefs 换成目标前缀。
func collectBatchPending(ctx context.Context, gs *services.GitService, dst *models.ProjectEnv, rows []services.BatchRow) batchSecretsOut {
	existset := map[string]bool{}
	var tidbSchema []services.KV
	tidbSuffixes := map[string]bool{}
	if err := gs.EnsureClone(ctx, dst.Name, dst.GitRepo, dst.GitBranch); err == nil {
		if raw, err := gs.ReadFile(dst.Name, zkvSecretsPathForEnv(dst)+"/values.yaml"); err == nil {
			for _, n := range services.ZkvSecretNames(raw) {
				existset[n] = true
			}
			tidbSchema = services.ZkvTidbKeySchema(raw)
			tidbSuffixes = services.ZkvTidbSuffixSet(raw)
		}
	}
	out := batchSecretsOut{Existing: []string{}, Pending: []batchPendingSecret{}}
	existSeen := map[string]bool{}
	order := []string{}
	byName := map[string]*batchPendingSecret{}
	for _, row := range rows {
		for _, ref := range services.ExtraEnvVarNames(row.ValuesYAML) {
			if existset[ref] {
				if !existSeen[ref] { // 已存在 → 复用（去重列出）
					existSeen[ref] = true
					out.Existing = append(out.Existing, ref)
				}
				continue
			}
			p := byName[ref]
			if p == nil {
				typ := "opaque"
				if services.ClassifyTidb(ref, tidbSuffixes) { // 按 z-kv 现有 tidbSecrets 后缀识别(含 uid-secret)
					typ = "tidb"
				}
				p = &batchPendingSecret{Name: ref, Type: typ, Namespace: row.Namespace}
				if typ == "tidb" {
					p.Fields = tidbKeySchemaOrDefault(tidbSchema) // 每条自己一份，值各模块独立填
				}
				byName[ref] = p
				order = append(order, ref)
			}
			p.Modules = append(p.Modules, row.ModuleName)
		}
	}
	for _, n := range order {
		out.Pending = append(out.Pending, *byName[n])
	}
	return out
}

// buildBatch 解析请求 → base spec + 每行(模块名/namespace/派生values)。每行 values 用模板派生预填。
func buildBatch(req batchReq, w http.ResponseWriter) (services.ModuleSpec, []services.BatchRow, *models.ProjectEnv, batchSecretsOut, bool) {
	base, tpl, dst, err := buildSpec(req.TemplateID, req.TargetEnvID, "", "")
	if err != nil {
		JSONError(w, 40001, err.Error())
		return base, nil, nil, batchSecretsOut{}, false
	}
	src, err := loadEnvGit("name=?", tpl.SrcEnv)
	if err != nil {
		JSONError(w, 40001, "样板环境不存在")
		return base, nil, nil, batchSecretsOut{}, false
	}
	gs := getGitService()
	ctx, cancel := services.GitCtx(context.Background(), 60)
	defer cancel()
	if err := gs.EnsureClone(ctx, src.Name, src.GitRepo, src.GitBranch); err != nil {
		InternalErr(w, nil, fmt.Errorf("同步样板仓库失败: %w", err))
		return base, nil, nil, batchSecretsOut{}, false
	}
	raw, err := gs.ReadFile(src.Name, base.SrcChartBasePath+"/"+base.SrcService+"/values.yaml")
	if err != nil {
		JSONError(w, 40400, "样板 values.yaml 不存在")
		return base, nil, nil, batchSecretsOut{}, false
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
			return base, nil, nil, batchSecretsOut{}, false
		}
		if ns == "" {
			ns = defaultNamespaceForEnv(dst) // 空则回落环境默认 namespace（前端一般已自动填）
		}
		// 用户在「配置」里自定义了 values 就用它，否则按模板派生预填
		vals := []byte(r.ValuesYAML)
		if strings.TrimSpace(r.ValuesYAML) == "" {
			vals = prefillValues(raw, src, dst, base.SrcService, name)
			if f, err := services.ApplyEnvIngress(vals, dst.IngressGateway); err == nil {
				vals = f
			}
			// 域名：非生产自动带单个；生产留空（批量生产域名先留空，需要在配置里手填/后续做）
			if dst.EnvType != "prod" {
				if domain := deriveDomainForEnv(dst, name, ingressEnabledFromValues(raw)); domain != "" {
					vals = services.SetIngressHosts(vals, []string{domain})
				}
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
			// 跟单个一致：extraEnvVars 里任意已知项目前缀(g32/g33…)统一换成目标项目(g50)
			vals = services.RenameSecretRefs(vals, allProjectPrefixes(), effectivePrefix(dst))
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
		return base, nil, nil, batchSecretsOut{}, false
	}
	// 待新建密钥（批量）：分类 → 按前端逐条填(secret_fills)挂到 base.NewTidbSecrets/NewPlainSecrets
	// （每行写 z-kv 幂等去重，多模块同名的只会落一条）。tidb 走 tidbSecrets 段、opaque 走 secrets 段。
	secretsOut := collectBatchPending(ctx, gs, dst, rows)
	base.ZkvSecretsPath = zkvSecretsPathForEnv(dst)
	// YAML 模式：直接把整段并入 z-kv（跳过表单逐条）。
	if strings.TrimSpace(req.SecretsYAML) != "" {
		base.NewSecretsYAML = req.SecretsYAML
		return base, rows, dst, secretsOut, true
	}
	// 表单模式：逐条 secret_fills 拼（既含检测到的，也含「+新增密钥」手动加的）。类型/database/字段/ns 都以填的为准。
	defNs := defaultNamespaceForEnv(dst)
	for _, f := range req.SecretFills {
		if strings.TrimSpace(f.Name) == "" {
			continue
		}
		ns := strings.TrimSpace(f.Namespace)
		if ns == "" {
			ns = defNs
		}
		if f.Type == "tidb" {
			db := strings.TrimSpace(f.Database)
			if db == "" {
				continue // 预览没填 / 提交时由调用方校验
			}
			base.NewTidbSecrets = append(base.NewTidbSecrets, services.TidbSecretEntry{
				Name: f.Name, Namespace: ns, Database: db, Extra: nonEmptyKVs(f.Fields),
			})
		} else {
			kvs := nonEmptyKVs(f.Fields)
			if len(kvs) == 0 {
				continue // opaque 没填键值对就先不建
			}
			base.NewPlainSecrets = append(base.NewPlainSecrets, services.PlainSecretEntry{
				Name: f.Name, Namespace: ns, Type: "Opaque", KVs: kvs,
			})
		}
	}
	return base, rows, dst, secretsOut, true
}

// nonEmptyKVs 去掉 key 为空的键值对（前端可能留空行）。
func nonEmptyKVs(kvs []services.KV) []services.KV {
	out := make([]services.KV, 0, len(kvs))
	for _, kv := range kvs {
		if strings.TrimSpace(kv.Key) != "" {
			out = append(out, kv)
		}
	}
	return out
}

// POST /api/orchestration/batch-preview
func HandleBatchPreview(w http.ResponseWriter, r *http.Request) {
	var req batchReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	if err := services.ValidateSecretsYAMLFragment([]byte(req.SecretsYAML)); err != nil {
		JSONError(w, 40001, err.Error())
		return
	}
	base, rows, _, secretsOut, ok := buildBatch(req, w)
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
	// 带上密钥分类（复用 + 待新建，含归属模块/字段）；沿用 BatchResult 的字段名不变。
	JSONSuccess(w, map[string]interface{}{
		"rows":          res.Rows,
		"changed_files": res.ChangedFiles,
		"all_ok":        res.AllOK,
		"secret_refs":   secretsOut,
	})
}

// POST /api/orchestration/batch-submit
func HandleBatchSubmit(w http.ResponseWriter, r *http.Request) {
	var req batchReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	if err := services.ValidateSecretsYAMLFragment([]byte(req.SecretsYAML)); err != nil {
		JSONError(w, 40001, err.Error())
		return
	}
	base, rows, dst, secretsOut, ok := buildBatch(req, w)
	if !ok {
		return
	}
	_ = secretsOut
	// 待新建的 tidb 密钥必须都填了 database 才能提交（YAML 模式跳过；逐条 fill 校验，含手动新增）。
	if strings.TrimSpace(req.SecretsYAML) == "" {
		for _, f := range req.SecretFills {
			if f.Type == "tidb" && strings.TrimSpace(f.Name) != "" && strings.TrimSpace(f.Database) == "" {
				JSONError(w, 40001, "待新建密钥 "+f.Name+" 需填写 database（批量密钥区）")
				return
			}
		}
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
	disable := req.Disable != nil && *req.Disable
	taskID, err := insertOrchTask(dst.ID, dst.Name, summary, "batch", operator, disable)
	if err != nil {
		JSONError(w, 50000, "创建任务失败: "+err.Error())
		return
	}
	Audit(r, "orchestration.add_batch", "module", fmt.Sprintf("%d modules", len(rows)), map[string]interface{}{
		"env": dst.Name, "task_id": taskID,
	})
	tempAt := req.AtLarkIDs // 本次临时额外艾特人
	InflightTrack(func() {
		start := time.Now()
		gs := getGitService()
		ctx, cancel := services.GitCtx(context.Background(), 600)
		defer cancel()
		res, serr := gs.SubmitBatch(ctx, base, rows, operator, msg)
		if serr != nil {
			log.Printf("⚠ [orch-fail] task=%d env=%s 批量 git/helm 阶段失败: %v", taskID, dst.Name, serr)
			updateOrchTaskFailed(taskID, serr.Error())
			return
		}
		if !res.AllOK {
			log.Printf("⚠ [orch-fail] task=%d env=%s 批量部分模块 helm 校验未通过", taskID, dst.Name)
			updateOrchTaskFailed(taskID, "部分模块 helm 校验未通过，未提交（请在批量预览里查看具体行）")
			return
		}
		setOrchTaskCommit(taskID, res.CommitSHA, res.CommitURL)
		if disable {
			finishOrchTask(taskID, "success", "已提交（预演 disable:true，未部署）", nil, dur(start))
			return
		}
		// 真部署 → 逐个轮询每个新模块的 ArgoCD 部署，聚合后发 Lark（拆成功/失败）
		mods := make([]newModDeploy, 0, len(rows))
		for _, rr := range rows {
			_, ver := services.GetImageRepoTag(rr.ValuesYAML)
			mods = append(mods, newModDeploy{Module: rr.ModuleName, Namespace: rr.Namespace, Version: ver, Domains: services.GetIngressHosts(rr.ValuesYAML)})
		}
		deployAndPollBatch(taskID, dst.ID, operator, mods, tempAt, start)
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
