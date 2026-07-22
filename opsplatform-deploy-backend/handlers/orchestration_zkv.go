package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"opsplatform-deploy-backend/models"
	"opsplatform-deploy-backend/services"
)

// z-kv-secrets 初始化：新项目还没有 z-kv-secrets 时，从「模板库」里的 zkv 模板整份复制一份到本项目，
// 只把 secret 名的项目前缀改成目标项目（key/值不动），用户在弹窗里改值后提交。
// 复用现有 ModuleSpec + SubmitNewModule 引擎（z-kv-secrets 就是个 chart，拷目录+改名+追加 -apps 条目）。

// GET /api/orchestration/zkv-status/{id} —— 目标环境 z-kv-secrets 是否已存在（项目参数展示状态用）。
func HandleZkvStatus(w http.ResponseWriter, r *http.Request) {
	envID := ParseID(mux.Vars(r)["id"])
	dst, err := loadEnvGit("id=?", envID)
	if err != nil {
		JSONError(w, 40001, "环境不存在")
		return
	}
	path := zkvSecretsPathForEnv(dst)
	exists := false
	gs := getGitService()
	ctx, cancel := services.GitCtx(context.Background(), 60)
	defer cancel()
	if err := gs.EnsureClone(ctx, dst.Name, dst.GitRepo, dst.GitBranch); err == nil {
		if _, e := gs.ReadFile(dst.Name, path+"/values.yaml"); e == nil {
			exists = true
		}
	}
	JSONSuccess(w, map[string]interface{}{"exists": exists, "zkv_path": path})
}

// POST /api/orchestration/zkv-preview —— 选 zkv 模板 → 读源 z-kv values + 按项目前缀改 secret 名 → 交初始化弹窗编辑。
func HandleZkvPreview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TemplateID  int64 `json:"template_id"`
		TargetEnvID int64 `json:"target_env_id"`
	}
	if !DecodeJSON(w, r, &req) {
		return
	}
	tpl, err := LoadTemplate(req.TemplateID)
	if err != nil || tpl.ModuleType != models.ModuleTypeZkv {
		JSONError(w, 40001, "请选 z-kv-secrets 类型的模板")
		return
	}
	dst, err := loadEnvGit("id=?", req.TargetEnvID)
	if err != nil {
		JSONError(w, 40001, "目标环境不存在")
		return
	}
	src, err := loadEnvGit("name=?", tpl.SrcEnv)
	if err != nil {
		JSONError(w, 40001, "样板环境不存在: "+tpl.SrcEnv)
		return
	}
	gs := getGitService()
	ctx, cancel := services.GitCtx(context.Background(), 60)
	defer cancel()
	if err := gs.EnsureClone(ctx, src.Name, src.GitRepo, src.GitBranch); err != nil {
		InternalErr(w, r, fmt.Errorf("同步样板仓库失败: %w", err))
		return
	}
	raw, err := gs.ReadFile(src.Name, src.ChartBasePath+"/z-kv-secrets/values.yaml")
	if err != nil {
		JSONError(w, 40400, "样板 z-kv-secrets/values.yaml 不存在: "+src.ChartBasePath)
		return
	}
	dstProj := effectivePrefix(dst)
	renamed := services.RenameZkvSecretNames(raw, allProjectPrefixes(), dstProj)
	// 目标是否已存在（存在则不该重复初始化）
	exists := false
	if err := gs.EnsureClone(ctx, dst.Name, dst.GitRepo, dst.GitBranch); err == nil {
		if _, e := gs.ReadFile(dst.Name, zkvSecretsPathForEnv(dst)+"/values.yaml"); e == nil {
			exists = true
		}
	}
	JSONSuccess(w, map[string]interface{}{
		"values_yaml": string(renamed),
		"zkv_path":    zkvSecretsPathForEnv(dst),
		"exists":      exists,
	})
}

// POST /api/orchestration/zkv-init —— 把改后的 z-kv values 复制成本项目的 z-kv-secrets chart + 追加 -apps 条目 + 提交。
func HandleInitZkv(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TemplateID  int64  `json:"template_id"`
		TargetEnvID int64  `json:"target_env_id"`
		ValuesYAML  string `json:"values_yaml"`
	}
	if !DecodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.ValuesYAML) == "" {
		JSONError(w, 40001, "values.yaml 不能为空")
		return
	}
	tpl, err := LoadTemplate(req.TemplateID)
	if err != nil || tpl.ModuleType != models.ModuleTypeZkv {
		JSONError(w, 40001, "请选 z-kv-secrets 类型的模板")
		return
	}
	dst, err := loadEnvGit("id=?", req.TargetEnvID)
	if err != nil {
		JSONError(w, 40001, "目标环境不存在")
		return
	}
	// 复用模块引擎：ModuleName=z-kv-secrets → 目标目录 <chart_base>/z-kv-secrets；追加 -apps 条目 name=z-kv-secrets
	spec, _, _, err := buildSpec(req.TemplateID, req.TargetEnvID, "z-kv-secrets", defaultNamespaceForEnv(dst))
	if err != nil {
		JSONError(w, 40001, err.Error())
		return
	}
	spec.ValuesYAML = []byte(req.ValuesYAML)
	spec.Disable = false // z-kv-secrets 直接部署（app-of-apps 生成 Application）
	gs := getGitService()
	ctx, cancel := services.GitCtx(context.Background(), 180)
	defer cancel()
	res, err := gs.SubmitNewModule(ctx, spec, UsernameFromCtx(r), "init z-kv-secrets for "+dst.Name)
	if err != nil {
		JSONError(w, 40001, err.Error())
		return
	}
	Audit(r, "orchestration.zkv.init", "project_env", strconv.FormatInt(dst.ID, 10), nil)
	JSONSuccess(w, res)
}
