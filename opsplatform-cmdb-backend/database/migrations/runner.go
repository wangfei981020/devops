package migrations

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"sort"
	"strings"
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

func splitSQL(s string) []string {
	lines := strings.Split(s, "\n")
	var out []string
	var buf strings.Builder
	for _, l := range lines {
		trim := strings.TrimSpace(l)
		if strings.HasPrefix(trim, "--") || trim == "" {
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
