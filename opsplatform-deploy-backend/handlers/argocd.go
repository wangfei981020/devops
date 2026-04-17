package handlers

import "net/http"

func HandleArgocdApplications(w http.ResponseWriter, r *http.Request) {
	jsonSuccess(w, map[string]interface{}{"status": "stub", "msg": "ArgoCD 应用列表代理: 待实现 (阶段2)"})
}

func HandleArgocdApplication(w http.ResponseWriter, r *http.Request) {
	jsonSuccess(w, map[string]interface{}{"status": "stub", "msg": "ArgoCD 应用详情代理: 待实现 (阶段2)"})
}

func HandleArgocdAppStatus(w http.ResponseWriter, r *http.Request) {
	jsonSuccess(w, map[string]interface{}{"status": "stub", "msg": "ArgoCD 应用状态代理: 待实现 (阶段2)"})
}

func HandleArgocdAppSync(w http.ResponseWriter, r *http.Request) {
	jsonSuccess(w, map[string]interface{}{"status": "stub", "msg": "ArgoCD 手动 sync 代理: 待实现 (阶段2)"})
}

func HandleArgocdAppEvents(w http.ResponseWriter, r *http.Request) {
	jsonSuccess(w, map[string]interface{}{"status": "stub", "msg": "ArgoCD 事件列表代理: 待实现 (阶段2)"})
}
