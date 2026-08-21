// Package health 提供健康与指标端口。
//
// # 为什么是独立端口，不是业务端口上的一条路由
//
// 业务端口 hang 死时（连接池被挂死的查询占满、goroutine 泄漏），
// 探针仍然要能回答。挂在业务端口上的话，"卡住"会被表现成"进程没了"，
// K8s 直接重启，而重启掩盖了真实原因。
//
// # 为什么这个契约不能各产品各定
//
// helm chart 是**跨产品统一**的：探针路径、端口名都写死在模板里。
// 服务去适配模板，不是给每个服务改一份模板 —— 否则每加一个产品就多一套
// chart，统一的意义就没了。
// 端口与路径必须与 ops-cmdb 保持一致：`:8088` + /health /ready /metrics。
package health

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"ops-sso-backend/logx"
)

// Start 起健康端口。阻塞，调用方用 go 起。
//
// db 为 nil 时 /ready 只做进程存活检查 —— 网关早期启动阶段可能还没连库。
func Start(addr string, db *sql.DB) {
	mux := http.NewServeMux()

	// /health：进程活着就行。**不查数据库** ——
	// 数据库抖一下就重启进程，是在故障上再加一层故障。
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// /ready：数据库真的可查才算就绪，不可查就摘流量（但不重启）。
	//
	// ⚠️ 必须带超时：裸 Ping 会去连接池要连接，池子被挂死的查询占满时
	// 它自己也一起无限期挂住 —— 探针既不成功也不失败，K8s 只能干等到
	// timeoutSeconds，而"为什么失败"完全没有信息。
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		if db == nil {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ready"))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			logx.Line("health", fmt.Sprintf("readiness 失败: %v", err))
			http.Error(w, "db down", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})

	logx.Line("health", fmt.Sprintf("健康端口 %s（/health, /ready）", addr))
	if err := http.ListenAndServe(addr, mux); err != nil {
		logx.Line("health", fmt.Sprintf("健康端口退出: %v", err))
	}
}
