package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// HealthChecker 与执行模式无关:只看"每一路负责的流,输出图多久没更新"。
// 补上 ffmpeg 活着但源卡住不出帧、或快照一直失败的盲区。
type HealthChecker struct {
	stale     time.Duration
	grace     time.Duration
	metrics   *Metrics
	getOwned  func() []Stream
	firstSeen map[string]time.Time // 流首次被本分片认领的时间(算宽限期)
}

func NewHealthChecker(stale, grace time.Duration, m *Metrics, getOwned func() []Stream) *HealthChecker {
	return &HealthChecker{stale: stale, grace: grace, metrics: m, getOwned: getOwned, firstSeen: map[string]time.Time{}}
}

func (h *HealthChecker) Check() {
	owned := h.getOwned()
	now := time.Now()
	seen := make(map[string]bool, len(owned))
	var bad []string
	healthy := 0

	for _, s := range owned {
		seen[s.Name] = true
		if _, ok := h.firstSeen[s.Name]; !ok {
			h.firstSeen[s.Name] = now
		}
		path := filepath.Join(s.OutputPath, s.OutputFile)
		fi, err := os.Stat(path)
		if err != nil { // 还没出图
			if now.Sub(h.firstSeen[s.Name]) > h.grace {
				bad = append(bad, s.Name+"(启动后一直无截图)")
			} else {
				healthy++ // 宽限期内,先不算异常
			}
			continue
		}
		if age := now.Sub(fi.ModTime()); age > h.stale {
			bad = append(bad, fmt.Sprintf("%s(%.0fs 未更新)", s.Name, age.Seconds()))
		} else {
			healthy++
		}
	}
	// 清理已不再负责的流
	for n := range h.firstSeen {
		if !seen[n] {
			delete(h.firstSeen, n)
		}
	}

	if h.metrics != nil {
		h.metrics.Healthy.Store(int64(healthy))
		h.metrics.Stale.Store(int64(len(bad)))
	}
	if len(bad) > 0 {
		log.Printf("WARN 健康检查: 共 %d 路,%d 路无最新截图 -> %s", len(owned), len(bad), strings.Join(bad, ", "))
	} else {
		log.Printf("INFO 健康检查: 共 %d 路,全部正常出图", len(owned))
	}
}
