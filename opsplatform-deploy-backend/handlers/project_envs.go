package handlers

import (
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/models"
)

func HandleListProjectEnvs(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("project_id")
	envID := r.URL.Query().Get("env_id")

	q := `SELECT pe.id, pe.project_id, pe.env_id, pe.git_repo, pe.git_branch, pe.git_base_path,
	             pe.namespace, pe.argocd_project, pe.argocd_cluster, pe.status, pe.created_at, pe.updated_at,
	             p.name, e.name
	      FROM project_envs pe
	      LEFT JOIN projects p ON pe.project_id = p.id
	      LEFT JOIN environments e ON pe.env_id = e.id
	      WHERE 1=1`
	args := []interface{}{}
	if projectID != "" {
		q += " AND pe.project_id=?"
		args = append(args, projectID)
	}
	if envID != "" {
		q += " AND pe.env_id=?"
		args = append(args, envID)
	}
	q += " ORDER BY pe.id"

	rows, err := database.DB.Query(q, args...)
	if err != nil {
		jsonError(w, 50000, err.Error())
		return
	}
	defer rows.Close()

	list := []models.ProjectEnv{}
	for rows.Next() {
		var pe models.ProjectEnv
		rows.Scan(&pe.ID, &pe.ProjectID, &pe.EnvID, &pe.GitRepo, &pe.GitBranch, &pe.GitBasePath,
			&pe.Namespace, &pe.ArgocdProject, &pe.ArgocdCluster, &pe.Status, &pe.CreatedAt, &pe.UpdatedAt,
			&pe.ProjectName, &pe.EnvName)
		list = append(list, pe)
	}
	jsonSuccess(w, list)
}

func HandleGetProjectEnv(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	var pe models.ProjectEnv
	err := database.DB.QueryRow(`SELECT pe.id, pe.project_id, pe.env_id, pe.git_repo, pe.git_branch, pe.git_base_path,
	                                    pe.namespace, pe.argocd_project, pe.argocd_cluster, pe.status, pe.created_at, pe.updated_at,
	                                    p.name, e.name
	                             FROM project_envs pe
	                             LEFT JOIN projects p ON pe.project_id = p.id
	                             LEFT JOIN environments e ON pe.env_id = e.id
	                             WHERE pe.id=?`, id).
		Scan(&pe.ID, &pe.ProjectID, &pe.EnvID, &pe.GitRepo, &pe.GitBranch, &pe.GitBasePath,
			&pe.Namespace, &pe.ArgocdProject, &pe.ArgocdCluster, &pe.Status, &pe.CreatedAt, &pe.UpdatedAt,
			&pe.ProjectName, &pe.EnvName)
	if err != nil {
		jsonError(w, 40400, "项目-环境不存在")
		return
	}
	jsonSuccess(w, pe)
}

func HandleCreateProjectEnv(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectID     int64  `json:"project_id"`
		EnvID         int64  `json:"env_id"`
		GitRepo       string `json:"git_repo"`
		GitBranch     string `json:"git_branch"`
		GitBasePath   string `json:"git_base_path"`
		Namespace     string `json:"namespace"`
		ArgocdProject string `json:"argocd_project"`
		ArgocdCluster string `json:"argocd_cluster"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 40000, "无效的请求")
		return
	}
	if req.ProjectID == 0 || req.EnvID == 0 || req.GitRepo == "" {
		jsonError(w, 40001, "project_id/env_id/git_repo 必填")
		return
	}
	if req.GitBranch == "" {
		req.GitBranch = "main"
	}
	if req.ArgocdProject == "" {
		req.ArgocdProject = "default"
	}
	if req.ArgocdCluster == "" {
		req.ArgocdCluster = "in-cluster"
	}
	res, err := database.DB.Exec(`INSERT INTO project_envs (project_id, env_id, git_repo, git_branch, git_base_path, namespace, argocd_project, argocd_cluster) VALUES (?,?,?,?,?,?,?,?)`,
		req.ProjectID, req.EnvID, req.GitRepo, req.GitBranch, req.GitBasePath, req.Namespace, req.ArgocdProject, req.ArgocdCluster)
	if err != nil {
		jsonError(w, 40900, "创建失败(可能已存在): "+err.Error())
		return
	}
	id, _ := res.LastInsertId()
	jsonSuccess(w, map[string]interface{}{"id": id})
}

func HandleUpdateProjectEnv(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	var req struct {
		GitRepo       string `json:"git_repo"`
		GitBranch     string `json:"git_branch"`
		GitBasePath   string `json:"git_base_path"`
		Namespace     string `json:"namespace"`
		ArgocdProject string `json:"argocd_project"`
		ArgocdCluster string `json:"argocd_cluster"`
		Status        string `json:"status"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 40000, "无效的请求")
		return
	}
	_, err := database.DB.Exec(`UPDATE project_envs SET git_repo=?, git_branch=?, git_base_path=?, namespace=?, argocd_project=?, argocd_cluster=?, status=? WHERE id=?`,
		req.GitRepo, req.GitBranch, req.GitBasePath, req.Namespace, req.ArgocdProject, req.ArgocdCluster, req.Status, id)
	if err != nil {
		jsonError(w, 50000, err.Error())
		return
	}
	jsonSuccess(w, nil)
}

func HandleDeleteProjectEnv(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	var c int
	database.DB.QueryRow(`SELECT COUNT(*) FROM modules WHERE project_env_id=? AND status!='deleted'`, id).Scan(&c)
	if c > 0 {
		jsonError(w, 40900, "存在模块，无法删除")
		return
	}
	_, err := database.DB.Exec(`DELETE FROM project_envs WHERE id=?`, id)
	if err != nil {
		jsonError(w, 50000, err.Error())
		return
	}
	jsonSuccess(w, nil)
}

func HandleTestGit(w http.ResponseWriter, r *http.Request) {
	jsonSuccess(w, map[string]interface{}{"status": "stub", "msg": "Git连通性测试待实现 (阶段2)"})
}

func HandleTestArgocd(w http.ResponseWriter, r *http.Request) {
	jsonSuccess(w, map[string]interface{}{"status": "stub", "msg": "ArgoCD连通性测试待实现 (阶段2)"})
}
