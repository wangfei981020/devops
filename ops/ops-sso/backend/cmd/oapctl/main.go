// oapctl：本地与实施用的小工具。
//
// 只放**必须在没有界面时也能做**的操作：设初始口令、签发应急口令、查版本。
// 其余一律走界面 —— 命令行能做的事越多，审计的盲区就越大。
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"ops-sso-backend/internal/domain/auth"
	"ops-sso-backend/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=true&loc=Local",
			env("DB_USER", "access"), os.Getenv("DB_PASSWORD"),
			env("DB_HOST", "127.0.0.1"), env("DB_PORT", "3306"), env("DB_NAME", "ops_access_plane"))
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil || db.Ping() != nil {
		fmt.Fprintln(os.Stderr, "连接数据库失败")
		os.Exit(1)
	}
	svc := auth.New(store.New(db))
	ctx := context.Background()

	switch os.Args[1] {
	case "set-password":
		// 用法：oapctl set-password <user_id> <password>
		if len(os.Args) != 4 {
			usage()
			os.Exit(2)
		}
		var uid int64
		fmt.Sscanf(os.Args[2], "%d", &uid)
		if err := svc.SetLocalPassword(ctx, uid, os.Args[3]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("已设置。首次登录后请立即改密。")

	case "bootstrap-admin":
		// 用法：oapctl bootstrap-admin <username> <password> [display_name]
		//
		// 建一个管理员并设初装口令。**允许弱口令**（方便本地与 POC），
		// 但会标记 must_change：这个账号在改掉口令之前，除了改口令什么都做不了。
		if len(os.Args) < 4 {
			usage()
			os.Exit(2)
		}
		username, password := os.Args[2], os.Args[3]
		display := username
		if len(os.Args) > 4 {
			display = os.Args[4]
		}
		uid, err := ensureAdmin(ctx, db, username, display)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := svc.SetBootstrapPassword(ctx, uid, password); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("已创建管理员 %s（id=%d），口令已设置。\n", username, uid)
		fmt.Println("⚠️ 已标记「必须改密」：首次登录会被强制改口令，在那之前它做不了别的事。")
		fmt.Println("⚠️ 这个口令是公开的 —— 谁先登进去谁就能改掉它。生产上建议这一步直接给强口令。")

	case "issue-break-glass":
		// 用法：oapctl issue-break-glass <user_id> [数量] [有效天数]
		if len(os.Args) < 3 {
			usage()
			os.Exit(2)
		}
		var uid int64
		n, days := 5, 180
		fmt.Sscanf(os.Args[2], "%d", &uid)
		if len(os.Args) > 3 {
			fmt.Sscanf(os.Args[3], "%d", &n)
		}
		if len(os.Args) > 4 {
			fmt.Sscanf(os.Args[4], "%d", &days)
		}
		codes, err := svc.IssueBreakGlassCodes(ctx, uid, n,
			time.Duration(days)*24*time.Hour, 0, time.Now().Format("20060102"))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("── 应急一次性口令（明文只出现这一次）──")
		for _, c := range codes {
			fmt.Println("  " + c)
		}
		fmt.Println("\n打印出来封存。存进任何联网系统，都会把「应急通道依赖它要救的东西」这个环形依赖装回去。")

	default:
		usage()
		os.Exit(2)
	}
}

// ensureAdmin 建（或复用）一个本地管理员账号。
func ensureAdmin(ctx context.Context, db *sql.DB, username, display string) (int64, error) {
	var uid int64
	err := db.QueryRowContext(ctx,
		`SELECT id FROM users WHERE username = ? AND source = 'local' AND deleted_at IS NULL`,
		username).Scan(&uid)
	if err == sql.ErrNoRows {
		res, err := db.ExecContext(ctx, `INSERT INTO users
			(username, display_name, source, status) VALUES (?, ?, 'local', 'active')`,
			username, display)
		if err != nil {
			return 0, err
		}
		uid, _ = res.LastInsertId()
	} else if err != nil {
		return 0, err
	}
	// 绑到默认租户并给管理员角色
	if _, err := db.ExecContext(ctx, `INSERT INTO user_tenants (user_id, tenant_id, role_code)
		VALUES (?, 1, 'admin') ON DUPLICATE KEY UPDATE role_code = 'admin'`, uid); err != nil {
		return 0, err
	}
	return uid, nil
}

func usage() {
	fmt.Fprintln(os.Stderr, `oapctl 用法：
  oapctl bootstrap-admin <username> <password> [显示名]   建管理员并设初装口令（允许弱口令）
  oapctl set-password <user_id> <password>
  oapctl issue-break-glass <user_id> [数量=5] [有效天数=180]

环境变量：DB_HOST/DB_PORT/DB_USER/DB_PASSWORD/DB_NAME 或 DB_DSN`)
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
