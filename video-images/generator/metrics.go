package main

import (
	"fmt"
	"io"
	"sync/atomic"
)

// Metrics Prometheus 指标(手写文本格式,不引第三方库,保持 Trivy 干净)
type Metrics struct {
	shard int
	total int

	Owned    atomic.Int64 // 本分片负责的流数
	Healthy  atomic.Int64 // 正常出图的流数
	Stale    atomic.Int64 // 卡住/无图的流数
	Restarts atomic.Int64 // ffmpeg 累计重启次数
	Uploads  atomic.Int64 // 上传成功累计
	UpFails  atomic.Int64 // 上传失败累计
	UpBytes  atomic.Int64 // 上传字节累计
	Snaps    atomic.Int64 // 快照模式:抓帧成功累计
	SnapFail atomic.Int64 // 快照模式:抓帧失败累计
}

func NewMetrics(shard, total int) *Metrics { return &Metrics{shard: shard, total: total} }

func (m *Metrics) WriteText(w io.Writer) {
	g := func(name, help string, v int64) {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n%s{shard=\"%d\"} %d\n", name, help, name, name, m.shard, v)
	}
	c := func(name, help string, v int64) {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s counter\n%s{shard=\"%d\"} %d\n", name, help, name, name, m.shard, v)
	}
	g("video_images_owned_streams", "本分片负责的流数", m.Owned.Load())
	g("video_images_healthy_streams", "正常出图的流数", m.Healthy.Load())
	g("video_images_stale_streams", "卡住或无图的流数", m.Stale.Load())
	c("video_images_ffmpeg_restarts_total", "ffmpeg 累计重启次数", m.Restarts.Load())
	c("video_images_uploads_total", "上传成功累计", m.Uploads.Load())
	c("video_images_upload_failures_total", "上传失败累计", m.UpFails.Load())
	c("video_images_upload_bytes_total", "上传字节累计", m.UpBytes.Load())
	c("video_images_snapshots_total", "快照抓帧成功累计", m.Snaps.Load())
	c("video_images_snapshot_failures_total", "快照抓帧失败累计", m.SnapFail.Load())
}
