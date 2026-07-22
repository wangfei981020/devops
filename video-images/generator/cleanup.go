package main

import (
	"context"
	"log"
	"time"

	"github.com/minio/minio-go/v7"
)

// Cleaner 只在分片 0 上跑:定期把 MinIO 里"配置中已不存在的对象"(被 ; 禁用/删除的死流老图)清掉。
// 判据:对象名不在当前 config 的全部 filename 集合里 → 孤儿 → 删除。
// 被 rebalance 到别的分片的流仍在 config 里,不会误删。
type Cleaner struct {
	client   *minio.Client
	bucket   string
	interval time.Duration
	getNames func() map[string]bool // 当前 config 全部 "filename.jpg" 集合
	metrics  *Metrics
}

func NewCleaner(c *minio.Client, bucket string, interval time.Duration, getNames func() map[string]bool, m *Metrics) *Cleaner {
	return &Cleaner{client: c, bucket: bucket, interval: interval, getNames: getNames, metrics: m}
}

func (c *Cleaner) Run(ctx context.Context) {
	t := time.NewTicker(c.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.cleanOnce(ctx)
		}
	}
}

func (c *Cleaner) cleanOnce(ctx context.Context) {
	names := c.getNames()
	if len(names) == 0 {
		return // 配置还没加载好,别误删
	}
	var orphans int
	for obj := range c.client.ListObjects(ctx, c.bucket, minio.ListObjectsOptions{Recursive: true}) {
		if obj.Err != nil {
			log.Printf("WARN 清理:列举对象失败: %v", obj.Err)
			return
		}
		if names[obj.Key] {
			continue // 仍在配置中,保留
		}
		if err := c.client.RemoveObject(ctx, c.bucket, obj.Key, minio.RemoveObjectOptions{}); err != nil {
			log.Printf("WARN 清理:删除孤儿对象 %s 失败: %v", obj.Key, err)
			continue
		}
		orphans++
		log.Printf("INFO 清理:删除已禁用/删除流的孤儿图 %s", obj.Key)
	}
	if orphans > 0 {
		log.Printf("INFO 清理:本轮删除 %d 个孤儿对象", orphans)
	}
}
