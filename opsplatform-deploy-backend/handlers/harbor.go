package handlers

import "net/http"

func HandleHarborProjects(w http.ResponseWriter, r *http.Request) {
	jsonSuccess(w, map[string]interface{}{"status": "stub", "msg": "Harbor 项目列表代理: 待实现 (阶段2)"})
}

func HandleHarborRepositories(w http.ResponseWriter, r *http.Request) {
	jsonSuccess(w, map[string]interface{}{"status": "stub", "msg": "Harbor 仓库列表代理: 待实现 (阶段2)"})
}

func HandleHarborTags(w http.ResponseWriter, r *http.Request) {
	jsonSuccess(w, map[string]interface{}{"status": "stub", "msg": "Harbor tag 列表代理: 待实现 (阶段2)"})
}
