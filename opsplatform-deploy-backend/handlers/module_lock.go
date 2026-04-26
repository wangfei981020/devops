package handlers

import (
	"context"
	"database/sql"
	"log"
	"time"

	"opsplatform-deploy-backend/database"
)

// =========================================================================
//   同 (env, module) 互斥锁
//
// 防止同一环境的同一模块被两人同时发布造成 git 冲突 / values.yaml 后写赢 race。
// 不同模块仍然完全并发（锁粒度 = (env, module)）。
//
// 锁有 expires_at 兜底，发布卡死或 backend 崩溃也最多锁 10 分钟。
// 启动时 + 每 5 分钟 sweepExpiredLocks 兜底清理。
// =========================================================================

const (
	lockTTL = 10 * time.Minute // 锁默认 TTL
)

// LockConflict 锁被占用时返回的冲突信息（前端展示）
type LockConflict struct {
	Module       string `json:"module"`
	Operator     string `json:"operator"`
	DeploymentID int64  `json:"deployment_id"`
	ElapsedSec   int    `json:"elapsed_sec"`
}

// AcquireModuleLocks 抢一组模块的锁。原子性：要么全成功，要么全失败（任何一个被占
// 整个返回 conflicts，已抢到的部分自动释放）。
//
// 调用方在拿到锁后必须 defer ReleaseModuleLocks，确保异常路径也释放。
func AcquireModuleLocks(envName string, modules []string, deploymentID int64, operator string) (
	conflicts []LockConflict, err error,
) {
	if len(modules) == 0 {
		return nil, nil
	}
	expiresAt := time.Now().Add(lockTTL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tx, err := database.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// 先 sweep 一次过期锁（不影响其他人，并提高抢锁成功率）
	_, _ = tx.ExecContext(ctx,
		`DELETE FROM module_deploy_lock WHERE expires_at < NOW()`)

	acquired := []string{}
	for _, mod := range modules {
		// INSERT 抢锁；UNIQUE 冲突说明被人占着
		_, ierr := tx.ExecContext(ctx,
			`INSERT INTO module_deploy_lock(env_name, module_name, deployment_id, operator, expires_at)
			 VALUES (?, ?, ?, ?, ?)`,
			envName, mod, deploymentID, operator, expiresAt)
		if ierr == nil {
			acquired = append(acquired, mod)
			continue
		}
		// 冲突 → 查谁占着
		var c LockConflict
		c.Module = mod
		var lockedAt time.Time
		err2 := tx.QueryRowContext(ctx,
			`SELECT IFNULL(operator,''), deployment_id, locked_at
			 FROM module_deploy_lock WHERE env_name=? AND module_name=?`,
			envName, mod).Scan(&c.Operator, &c.DeploymentID, &lockedAt)
		if err2 != nil && err2 != sql.ErrNoRows {
			// 拿不到详情，给一个粗略的
			c.Operator = "未知"
		} else {
			c.ElapsedSec = int(time.Since(lockedAt).Seconds())
		}
		conflicts = append(conflicts, c)
	}

	if len(conflicts) > 0 {
		// 任意一个被占 → 全部回滚（已抢到的也吐回去）
		return conflicts, nil
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return nil, nil
}

// ReleaseModuleLocks 释放一组模块的锁
//
//	幂等：模块名不存在锁也无所谓
//	deferred 调用，所有路径（成功 / panic / context cancel）都会执行
func ReleaseModuleLocks(envName string, modules []string) {
	if len(modules) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, mod := range modules {
		_, err := database.DB.ExecContext(ctx,
			`DELETE FROM module_deploy_lock WHERE env_name=? AND module_name=?`, envName, mod)
		if err != nil {
			log.Printf("[module-lock] release env=%s mod=%s failed: %v", envName, mod, err)
		}
	}
}

// StartLockSweeper 启动 5 分钟一次的过期锁清理（兜底防 backend 崩溃留下的孤锁）
func StartLockSweeper() {
	go func() {
		for {
			time.Sleep(5 * time.Minute)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			res, err := database.DB.ExecContext(ctx,
				`DELETE FROM module_deploy_lock WHERE expires_at < NOW()`)
			cancel()
			if err != nil {
				log.Printf("[module-lock] sweep failed: %v", err)
				continue
			}
			n, _ := res.RowsAffected()
			if n > 0 {
				log.Printf("[module-lock] swept %d expired locks", n)
			}
		}
	}()
}
