package database

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	_ "github.com/go-sql-driver/mysql"

	"opsplatform-cmdb-backend/database/migrations"
)

// Open 连接 MySQL（库需预先创建），然后自动跑 migration 建表。
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
		if strings.Contains(err.Error(), "Unknown database") {
			_ = db.Close()
			return nil, fmt.Errorf("database %q does not exist; create it first", cfg.DBName)
		}
		return nil, err
	}
	log.Printf("MySQL connected (db=%s)", cfg.DBName)

	if err := migrations.Run(db); err != nil {
		return nil, fmt.Errorf("run migrations: %w", err)
	}
	return db, nil
}
