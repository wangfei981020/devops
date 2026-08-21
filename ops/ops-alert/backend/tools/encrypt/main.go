// encrypt 把明文凭据加密成可直接写进 auth_enc / config_enc 的密文。
//
// 用途：本地调试与应急（界面还没做完、或需要手工修一条配置）。
// 生产环境的凭据一律走接口写入，不要用它往库里灌——手工写库绕过审计。
//
//	go run ./tools/encrypt '{"username":"u","password":"p"}'
//	ALERT_AES_KEY=xxx go run ./tools/encrypt '...'   # 与服务同一个密钥
package main

import (
	"fmt"
	"os"

	"ops-alert-backend/crypto"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "用法: encrypt <明文 JSON>")
		os.Exit(2)
	}
	key := os.Getenv("ALERT_AES_KEY")
	if key == "" {
		// 与 config.Load 的默认值保持一致。不一致的话加密出来的密文
		// 服务解不开，报错是"凭据解密失败"，很容易被误判成密钥损坏。
		key = "alert-dev-aes-key-change-in-prod"
	}
	c, err := crypto.New(key)
	if err != nil {
		fmt.Fprintln(os.Stderr, "密钥无效:", err)
		os.Exit(1)
	}
	out, err := c.Encrypt(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "加密失败:", err)
		os.Exit(1)
	}
	fmt.Println(out)
}
