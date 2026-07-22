package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
)

// Uploader 轮询输出目录,把有变化的 jpg 上传到 MinIO(对象名 = 文件名)。
// 采用 mtime 轮询而非 inotify: 依赖少。ffmpeg 用 -update 1 原地覆写,轮询可能撞上写一半,
// 故只上传"已静置 settle 秒"的文件(避免传出半张/花屏图)。
type Uploader struct {
	client   *minio.Client
	bucket   string
	dir      string
	interval time.Duration
	settle   time.Duration // 文件 mtime 距今须 ≥ settle 才上传,躲开 ffmpeg 原地覆写窗口
	metrics  *Metrics
	seen     map[string]time.Time // 文件名 -> 上次上传的 mtime
}

func NewUploader(c *minio.Client, bucket, dir string, interval, settle time.Duration, m *Metrics) *Uploader {
	return &Uploader{client: c, bucket: bucket, dir: dir, interval: interval, settle: settle, metrics: m, seen: map[string]time.Time{}}
}

func (u *Uploader) Run(ctx context.Context) {
	t := time.NewTicker(u.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			u.scanOnce(ctx)
		}
	}
}

func (u *Uploader) scanOnce(ctx context.Context) {
	entries, err := os.ReadDir(u.dir)
	if err != nil {
		log.Printf("WARN 读取输出目录失败 %s: %v", u.dir, err)
		return
	}
	present := make(map[string]bool, len(entries)) // 本轮实际存在的 .jpg,用于回收 seen 残留
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".jpg") {
			continue
		}
		present[ent.Name()] = true
		info, err := ent.Info()
		if err != nil {
			continue
		}
		mt := info.ModTime()
		if last, ok := u.seen[ent.Name()]; ok && !mt.After(last) {
			continue // 没变化,跳过
		}
		if time.Since(mt) < u.settle {
			continue // 刚写入,可能正被 ffmpeg 原地覆写,本轮先跳过(不标 seen),下轮再传
		}
		if err := u.upload(ctx, ent.Name()); err != nil {
			if u.metrics != nil {
				u.metrics.UpFails.Add(1)
			}
			log.Printf("ERROR 上传失败 %s: %v", ent.Name(), err)
			continue
		}
		u.seen[ent.Name()] = mt
		if u.metrics != nil {
			u.metrics.Uploads.Add(1)
			u.metrics.UpBytes.Add(info.Size())
		}
		log.Printf("INFO 上传成功 %s", ent.Name())
	}
	// 回收:删掉已不在目录里的文件(禁用/删除的流)在 seen 里的残留,防缓慢内存增长
	for name := range u.seen {
		if !present[name] {
			delete(u.seen, name)
		}
	}
}

func (u *Uploader) upload(ctx context.Context, name string) error {
	path := filepath.Join(u.dir, name)
	_, err := u.client.FPutObject(ctx, u.bucket, name, path, minio.PutObjectOptions{
		ContentType:  "image/jpeg",
		CacheControl: "max-age=30",
	})
	return err
}
