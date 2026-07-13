package handlers

import (
	"context"
	"log"
	"time"

	"opsplatform-deploy-backend/database"
)

// WarmAllEnvCaches 开机后台把所有项目环境的 git 缓存预热(clone/fetch)。
//
//	git 缓存在持久卷(PVC)上，pod 重启不丢——正常重启无需预热(缓存还在，Checkout 直接硬链接克隆)。
//	这里覆盖的是"全新环境 / 空卷第一次"的冷启动：开机顺手把缓存都拉好，避免第一个新增/发布要等全量克隆。
//	best-effort，串行慢慢来不抢资源；失败只记日志不阻断。
func WarmAllEnvCaches() {
	rows, err := database.DB.Query(`SELECT name, git_repo, git_branch FROM project_env WHERE git_repo<>'' ORDER BY id`)
	if err != nil {
		log.Printf("[cache-warm] list envs: %v", err)
		return
	}
	type env struct{ name, repo, branch string }
	var envs []env
	for rows.Next() {
		var e env
		if rows.Scan(&e.name, &e.repo, &e.branch) == nil {
			envs = append(envs, e)
		}
	}
	rows.Close()

	gs := getGitService()
	warmed := 0
	for _, e := range envs {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		if err := gs.EnsureClone(ctx, e.name, e.repo, e.branch); err != nil {
			log.Printf("[cache-warm] %s: %v", e.name, err)
		} else {
			warmed++
		}
		cancel()
	}
	log.Printf("[cache-warm] done: %d/%d envs warmed", warmed, len(envs))
}
