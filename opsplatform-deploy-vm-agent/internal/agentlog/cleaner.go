package agentlog

import (
	"log"
	"os"
	"path/filepath"
	"time"
)

// StartCleaner 起一个后台 goroutine，定时扫 baseDir 下的日期目录，删超过 retentionDays 的。
//
//	布局是 <baseDir>/YYYY-MM-DD/，所以判断目录名是不是合法日期就够，无需读文件 mtime。
//	间隔：固定 1 小时跑一次。retentionDays<=0 时函数空转不删。
//	失败只 log.Printf，不影响主流程。
func StartCleaner(baseDir string, retentionDays int) {
	if baseDir == "" || retentionDays <= 0 {
		return
	}
	go func() {
		// 启动时立刻跑一次（拉起就清陈旧目录），之后每小时跑一次
		runOnce(baseDir, retentionDays)
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			runOnce(baseDir, retentionDays)
		}
	}()
}

func runOnce(baseDir string, retentionDays int) {
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	cutoffStr := cutoff.Format("2006-01-02")

	entries, err := os.ReadDir(baseDir)
	if err != nil {
		// baseDir 还没创建（agent 刚启动还没写过日志）→ 不算错
		if !os.IsNotExist(err) {
			log.Printf("[agentlog-cleaner] readdir %s: %v", baseDir, err)
		}
		return
	}
	deleted := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		// 只处理 YYYY-MM-DD 命名的目录（防误删用户放的其他东西）
		if !looksLikeDate(name) {
			continue
		}
		if name >= cutoffStr {
			continue // 字典序比较 = 时间序比较（YYYY-MM-DD 格式特性）
		}
		fullPath := filepath.Join(baseDir, name)
		if err := os.RemoveAll(fullPath); err != nil {
			log.Printf("[agentlog-cleaner] remove %s: %v", fullPath, err)
			continue
		}
		deleted++
	}
	if deleted > 0 {
		log.Printf("[agentlog-cleaner] cleaned %d old day(s) under %s (retention=%d)", deleted, baseDir, retentionDays)
	}
}

// looksLikeDate 检查 "2026-04-29" 这种 10 字符日期格式
//
//	防御性：如果 baseDir 下被其他人放了奇怪文件名/目录，不要误删
func looksLikeDate(s string) bool {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return false
	}
	// 各位都是数字
	for i, ch := range s {
		if i == 4 || i == 7 {
			continue
		}
		if ch < '0' || ch > '9' {
			return false
		}
	}
	// 月份 / 日期合法性（粗略，01~12, 01~31）
	month := s[5:7]
	day := s[8:10]
	if month < "01" || month > "12" || day < "01" || day > "31" {
		return false
	}
	return true
}
