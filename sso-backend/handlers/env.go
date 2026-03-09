package handlers

import (
	"net/http"

	"sso-backend/config"
	"sso-backend/database"
)

func HandlePublicEnv(w http.ResponseWriter, r *http.Request) {
	label := config.EnvLabel
	color := ""
	switch label {
	case "开发":
		color = "#3b82f6"
	case "测试":
		color = "#f59e0b"
	case "生产":
		color = "#ef4444"
	default:
		color = "#6b7280"
	}
	jsonOK(w, map[string]string{
		"env_label": label,
		"env_color": color,
	})
}

func HandlePublicEnvironments(w http.ResponseWriter, r *http.Request) {
	envs, err := database.ListEnvironments()
	if err != nil {
		jsonOK(w, []struct{}{})
		return
	}
	jsonOK(w, envs)
}
