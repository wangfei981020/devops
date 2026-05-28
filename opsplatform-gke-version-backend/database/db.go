package database

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	_ "github.com/go-sql-driver/mysql"

	"opsplatform-gke-version-backend/database/migrations"
)

// Open: 连接 MySQL（库需用户预先创建），然后自动跑 migration 建表。
func Open(dsn string) (*sql.DB, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse DSN: %w", err)
	}
	if cfg.DBName == "" {
		return nil, fmt.Errorf("DSN missing dbname")
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	if err := db.Ping(); err != nil {
		// 库不存在时给出明确指引
		if strings.Contains(err.Error(), "Unknown database") {
			_ = db.Close()
			return nil, fmt.Errorf("database %q does not exist. Please create it first:\n\n"+
				"  mysql> CREATE DATABASE `%s` DEFAULT CHARSET utf8mb4;\n\n"+
				"Underlying error: %w", cfg.DBName, cfg.DBName, err)
		}
		return nil, err
	}
	log.Printf("MySQL connected (db=%s)", cfg.DBName)

	if err := migrations.Run(db); err != nil {
		return nil, fmt.Errorf("run migrations: %w", err)
	}
	return db, nil
}
