package database

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/go-sql-driver/mysql"
	_ "github.com/go-sql-driver/mysql"

	"opsplatform-gke-version-backend/database/migrations"
)

// Open: 解析 DSN，库不存在则自动 CREATE DATABASE，连接后跑 migration
func Open(dsn string) (*sql.DB, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse DSN: %w", err)
	}
	if cfg.DBName == "" {
		return nil, fmt.Errorf("DSN missing dbname")
	}

	// 用不带 dbname 的 DSN 连一次，自动建库
	dbName := cfg.DBName
	cfg.DBName = ""
	bootDSN := cfg.FormatDSN()
	if err := ensureDatabase(bootDSN, dbName); err != nil {
		return nil, fmt.Errorf("ensure database: %w", err)
	}

	// 正式连接
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.Ping(); err != nil {
		return nil, err
	}
	log.Printf("MySQL connected (db=%s)", dbName)

	if err := migrations.Run(db); err != nil {
		return nil, fmt.Errorf("run migrations: %w", err)
	}
	return db, nil
}

func ensureDatabase(bootDSN, dbName string) error {
	boot, err := sql.Open("mysql", bootDSN)
	if err != nil {
		return err
	}
	defer boot.Close()
	if err := boot.Ping(); err != nil {
		return fmt.Errorf("ping mysql server: %w", err)
	}
	if _, err := boot.Exec(fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` DEFAULT CHARSET utf8mb4", dbName)); err != nil {
		return err
	}
	log.Printf("database %s: ensured", dbName)
	return nil
}
