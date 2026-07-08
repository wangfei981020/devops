package migrations

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"sort"
	"strings"

	"github.com/go-sql-driver/mysql"
)

//go:embed *.sql
var fsys embed.FS

func Run(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version VARCHAR(64) NOT NULL PRIMARY KEY,
		applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	for _, f := range files {
		ver := strings.TrimSuffix(f, ".sql")
		var applied int
		if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version=?", ver).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", ver, err)
		}
		if applied > 0 {
			log.Printf("migration %s: already applied, skip", ver)
			continue
		}
		log.Printf("migration %s: applying...", ver)
		sqlBytes, err := fs.ReadFile(fsys, f)
		if err != nil {
			return fmt.Errorf("read %s: %w", f, err)
		}
		for _, s := range splitSQL(string(sqlBytes)) {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if _, err := db.Exec(s); err != nil {
				// 迁移可重入：ADD COLUMN / ADD INDEX 等无 IF NOT EXISTS 的 DDL，
				// 若某语句在上次中断的运行里已生效，重跑会报"已存在"，此处容错跳过，
				// 让 pod 重启即可自愈补齐剩余语句（DDL 在 MySQL 不可回滚，只能靠幂等）。
				if isAlreadyExistsErr(err) {
					log.Printf("migration %s: stmt already applied, skip (%v)", f, err)
					continue
				}
				return fmt.Errorf("exec in %s: %w\n--stmt--\n%s", f, err, s)
			}
		}
		if _, err := db.Exec("INSERT INTO schema_migrations(version) VALUES(?)", ver); err != nil {
			return err
		}
		log.Printf("migration %s: OK", ver)
	}
	return nil
}

// isAlreadyExistsErr 判断是否"对象已存在/已删除"类 MySQL 错误（迁移重跑时可安全跳过）。
// 1050 表已存在 / 1060 列重复 / 1061 索引重复 / 1091 待删对象不存在 / 1826 外键重复。
func isAlreadyExistsErr(err error) bool {
	var me *mysql.MySQLError
	if errors.As(err, &me) {
		switch me.Number {
		case 1050, 1060, 1061, 1091, 1826:
			return true
		}
	}
	return false
}

func splitSQL(s string) []string {
	lines := strings.Split(s, "\n")
	var out []string
	var buf strings.Builder
	for _, l := range lines {
		// 剥离行内/整行 -- 注释（迁移里字符串字面量不含 --，安全）。
		// 否则带尾注释的语句(如 `ALTER ... DEFAULT '';   -- 说明`)不以 ; 判定，会与后续语句粘连。
		if i := strings.Index(l, "--"); i >= 0 {
			l = l[:i]
		}
		trim := strings.TrimSpace(l)
		if trim == "" {
			continue
		}
		buf.WriteString(l)
		buf.WriteString("\n")
		if strings.HasSuffix(trim, ";") {
			out = append(out, buf.String())
			buf.Reset()
		}
	}
	if strings.TrimSpace(buf.String()) != "" {
		out = append(out, buf.String())
	}
	return out
}
