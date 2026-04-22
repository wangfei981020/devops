package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/models"
)

// GET /api/gitlab-repos
func HandleListGitlabRepos(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`SELECT id, name, repo_url, default_branch, description, created_at, updated_at
		FROM gitlab_repo ORDER BY name`)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	defer rows.Close()
	list := []models.GitlabRepo{}
	for rows.Next() {
		var g models.GitlabRepo
		_ = rows.Scan(&g.ID, &g.Name, &g.RepoURL, &g.DefaultBranch, &g.Description, &g.CreatedAt, &g.UpdatedAt)
		list = append(list, g)
	}
	JSONSuccess(w, list)
}

type gitlabRepoReq struct {
	Name          string `json:"name"`
	RepoURL       string `json:"repo_url"`
	DefaultBranch string `json:"default_branch"`
	Description   string `json:"description"`
}

// POST /api/gitlab-repos
func HandleCreateGitlabRepo(w http.ResponseWriter, r *http.Request) {
	var req gitlabRepoReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.RepoURL = strings.TrimSpace(req.RepoURL)
	req.DefaultBranch = strings.TrimSpace(req.DefaultBranch)
	if err := ValidateName(req.Name); err != nil {
		JSONError(w, 40001, "name: "+err.Error())
		return
	}
	if err := ValidateGitRepoURL(req.RepoURL); err != nil {
		JSONError(w, 40001, err.Error())
		return
	}
	if req.DefaultBranch == "" {
		req.DefaultBranch = "main"
	}
	res, err := database.DB.Exec(
		`INSERT INTO gitlab_repo (name, repo_url, default_branch, description) VALUES (?, ?, ?, ?)`,
		req.Name, req.RepoURL, req.DefaultBranch, strings.TrimSpace(req.Description))
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate") {
			JSONError(w, 40900, "name 已存在")
			return
		}
		InternalErr(w, r, err)
		return
	}
	id, _ := res.LastInsertId()
	Audit(r, "gitlab_repo.create", "gitlab_repo", req.Name, nil)
	JSONSuccess(w, map[string]interface{}{"id": id})
}

// PUT /api/gitlab-repos/{id}
func HandleUpdateGitlabRepo(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	var req gitlabRepoReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	req.RepoURL = strings.TrimSpace(req.RepoURL)
	if err := ValidateGitRepoURL(req.RepoURL); err != nil {
		JSONError(w, 40001, err.Error())
		return
	}
	branch := strings.TrimSpace(req.DefaultBranch)
	if branch == "" {
		branch = "main"
	}
	if _, err := database.DB.Exec(
		`UPDATE gitlab_repo SET repo_url=?, default_branch=?, description=? WHERE id=?`,
		req.RepoURL, branch, strings.TrimSpace(req.Description), id); err != nil {
		InternalErr(w, r, err)
		return
	}
	Audit(r, "gitlab_repo.update", "gitlab_repo", strconv.FormatInt(id, 10), nil)
	JSONSuccess(w, nil)
}

// DELETE /api/gitlab-repos/{id}
func HandleDeleteGitlabRepo(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	// 删之前检查：有没有 project_env 引用这个仓库
	var cnt int
	_ = database.DB.QueryRow(`SELECT COUNT(*) FROM project_env WHERE gitlab_repo_id=?`, id).Scan(&cnt)
	if cnt > 0 {
		JSONError(w, 40900, "还有 "+strconv.Itoa(cnt)+" 个项目环境引用此仓库，不能删除")
		return
	}
	if _, err := database.DB.Exec(`DELETE FROM gitlab_repo WHERE id=?`, id); err != nil {
		InternalErr(w, r, err)
		return
	}
	Audit(r, "gitlab_repo.delete", "gitlab_repo", strconv.FormatInt(id, 10), nil)
	JSONSuccess(w, nil)
}

// LoadGitlabRepo 内部：按 ID 加载
func LoadGitlabRepo(id int64) (*models.GitlabRepo, error) {
	var g models.GitlabRepo
	err := database.DB.QueryRow(
		`SELECT id, name, repo_url, default_branch, description, created_at, updated_at
		 FROM gitlab_repo WHERE id=?`, id).
		Scan(&g.ID, &g.Name, &g.RepoURL, &g.DefaultBranch, &g.Description, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &g, nil
}
