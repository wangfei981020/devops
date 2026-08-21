package k8ssource

import (
	"database/sql"
	"fmt"
	"strings"
)

// replaceBatchSize 单条 INSERT 的最大行数。MySQL 占位符上限 65535，按最宽的表
// (k8s_pods 15 列) 算 500 行也只用 7500 个，远低于上限，同时避免单条语句过大。
const replaceBatchSize = 500

// replaceAll 在一个事务里完成「清空本集群旧数据 + 批量写入新数据」，返回写入行数。
//
// 为什么必须是事务：采集原先是裸 DELETE 后逐条 INSERT，两者之间存在一个窗口，
// 期间任何查询都会读到写了一半的表——曾导致 list_namespaces 只返回 48/100 个命名空间，
// 而调用方完全无从察觉。放进事务后，读方靠 InnoDB MVCC 要么看到上一轮的完整数据、
// 要么看到这一轮的完整数据，不存在中间态。
//
// 失败即整体回滚：宁可让调用方拿到一份旧但完整的数据，也不留下残缺数据。
func replaceAll(db *sql.DB, table string, cols []string, cid int, rows [][]any) (int, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() // Commit 之后再 Rollback 是空操作

	if _, err := tx.Exec("DELETE FROM "+table+" WHERE cluster_id=?", cid); err != nil {
		return 0, fmt.Errorf("清空 %s: %w", table, err)
	}

	colSQL := strings.Join(cols, ",")
	one := "(" + strings.TrimSuffix(strings.Repeat("?,", len(cols)), ",") + ")"
	for start := 0; start < len(rows); start += replaceBatchSize {
		end := start + replaceBatchSize
		if end > len(rows) {
			end = len(rows)
		}
		batch := rows[start:end]
		args := make([]any, 0, len(batch)*len(cols))
		for i, r := range batch {
			if len(r) != len(cols) {
				return 0, fmt.Errorf("%s 第 %d 行给了 %d 个值，期望 %d 个", table, start+i, len(r), len(cols))
			}
			args = append(args, r...)
		}
		q := "INSERT INTO " + table + " (" + colSQL + ") VALUES " +
			strings.TrimSuffix(strings.Repeat(one+",", len(batch)), ",")
		if _, err := tx.Exec(q, args...); err != nil {
			return 0, fmt.Errorf("写入 %s 第 %d~%d 行: %w", table, start, end, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("提交 %s: %w", table, err)
	}
	return len(rows), nil
}

// clearAll 清空本集群某表（CRD 不存在等场景），同样保证原子。
func clearAll(db *sql.DB, table string, cid int) error {
	_, err := db.Exec("DELETE FROM "+table+" WHERE cluster_id=?", cid)
	return err
}
