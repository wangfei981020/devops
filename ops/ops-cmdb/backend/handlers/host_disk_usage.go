package handlers

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"ops-cmdb-backend/crypto"
	"ops-cmdb-backend/logx"
)

// 磁盘用量采集：从 Prometheus 的 node_exporter 指标补齐 host_disks.used_percent。
//
// ⚠️ 这里有一个绕不开的现实问题：**云盘和文件系统对不上号**。
//
//	云 API 给的是磁盘名     persistent-disk-1 / kafka-data-1
//	node_exporter 给的是    device=/dev/sdb, mountpoint=/var/lib/kafka
//
// 两者之间没有直接可用的关联键 —— GCP 的 deviceName 只体现在节点上的
// /dev/disk/by-id/google-<deviceName> 符号链接里，而 node_exporter 默认不报它。
//
// 所以本实现用**容量近似**做启发式配对，并接受它可能配错归属。
// 这个取舍是有意的：
//   - mount_point 和 used_percent 来自 node_exporter，**数值是准的**
//   - 只有"这个用量属于哪块云盘"是猜的
//
// 而运维真正要回答的问题是"哪个挂载点快满了"（/var/lib/kafka 96%），
// 不是"persistent-disk-2 满了"。所以即使归属配错，界面上呈现的信息仍然可用。
//
// 配不上的盘一律**保持 used_percent 为 NULL**，绝不写 0 —— 宁可没数据，不可给错数据。

const (
	// 容量匹配的容差。文件系统可用容量总比云盘标称小（元数据、保留块），
	// 通常在 3%–7%，给 12% 留足余量。
	diskMatchTolerance = 0.12
	// 排除的伪文件系统。不排的话每台机器会多出几十条无意义的记录，
	// 且它们的"用量"（如 tmpfs 100%）会误报成磁盘告警。
	fsTypeExclude = `tmpfs|overlay|squashfs|ramfs|devtmpfs|iso9660|autofs|nsfs`
)

// fsUsage 一个文件系统的用量观测。
type fsUsage struct {
	InstanceIP string
	Mountpoint string
	Device     string
	SizeGB     float64
	UsedPct    float64
}

// diskRecord 库里的一块云盘，待配对。
type diskRecord struct {
	ID     int64
	SizeGB int
	Name   string
}

// matchDisks 把文件系统观测配对到云盘记录上。
//
// 抽成纯函数是为了能脱离 Prometheus 和数据库测试 ——
// 配对逻辑正是最容易出错、又最难在真实环境里发现的部分：
// 配错了不报错，只是某块盘的用量数字挂到了另一块上。
//
// 返回 diskID → 观测。配不上的云盘不出现在结果里（保持 NULL）。
func matchDisks(disks []diskRecord, fs []fsUsage) map[int64]fsUsage {
	out := map[int64]fsUsage{}
	if len(disks) == 0 || len(fs) == 0 {
		return out
	}

	// 排序让配对结果**确定**：容量相同的多块盘（如 Kafka 的两块 500G 数据盘）
	// 本来就无法区分，但至少要保证同样的输入每次得到同样的输出，
	// 否则用量会在两块盘之间来回跳，看起来像数据在抖。
	ds := append([]diskRecord(nil), disks...)
	sort.Slice(ds, func(i, j int) bool {
		if ds[i].SizeGB != ds[j].SizeGB {
			return ds[i].SizeGB > ds[j].SizeGB
		}
		return ds[i].Name < ds[j].Name
	})
	fss := append([]fsUsage(nil), fs...)
	sort.Slice(fss, func(i, j int) bool {
		if fss[i].SizeGB != fss[j].SizeGB {
			return fss[i].SizeGB > fss[j].SizeGB
		}
		return fss[i].Mountpoint < fss[j].Mountpoint
	})

	used := make([]bool, len(fss))
	for _, d := range ds {
		if d.SizeGB <= 0 {
			continue
		}
		best, bestDiff := -1, diskMatchTolerance
		for i, f := range fss {
			if used[i] || f.SizeGB <= 0 {
				continue
			}
			diff := abs(f.SizeGB-float64(d.SizeGB)) / float64(d.SizeGB)
			if diff < bestDiff {
				best, bestDiff = i, diff
			}
		}
		if best >= 0 {
			used[best] = true
			out[d.ID] = fss[best]
		}
	}
	return out
}

// instanceIP 从 node_exporter 的 instance 标签里取 IP。
// 标签形如 10.128.0.41:9100，也可能是纯 IP 或带 http:// 前缀。
func instanceIP(instance string) string {
	s := strings.TrimPrefix(strings.TrimPrefix(instance, "http://"), "https://")
	if i := strings.LastIndex(s, ":"); i > 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// fetchFSUsage 从一个 Prometheus 端点拉全部文件系统的容量与用量。
func fetchFSUsage(base, token string) ([]fsUsage, error) {
	sizeQ := fmt.Sprintf(`node_filesystem_size_bytes{fstype!~"%s"}`, fsTypeExclude)
	availQ := fmt.Sprintf(`node_filesystem_avail_bytes{fstype!~"%s"}`, fsTypeExclude)

	sizes, err := promInstant(base, token, sizeQ)
	if err != nil {
		return nil, fmt.Errorf("查 node_filesystem_size_bytes 失败: %w", err)
	}
	avails, err := promInstant(base, token, availQ)
	if err != nil {
		return nil, fmt.Errorf("查 node_filesystem_avail_bytes 失败: %w", err)
	}

	// 按 instance+mountpoint 关联两个指标。
	// 用 device 做键会在 bind mount 等场景下重复；mountpoint 才是唯一的。
	key := func(m map[string]string) string {
		return m["instance"] + "\x00" + m["mountpoint"]
	}
	availBy := make(map[string]float64, len(avails))
	for _, s := range avails {
		availBy[key(s.Metric)] = s.Value
	}

	const gib = 1024 * 1024 * 1024
	out := make([]fsUsage, 0, len(sizes))
	for _, s := range sizes {
		if s.Value <= 0 {
			continue
		}
		av, ok := availBy[key(s.Metric)]
		if !ok {
			// 只有容量没有可用量，算不出百分比。跳过而不是当成 0% ——
			// 0% 会让一块满盘看起来是空的。
			continue
		}
		out = append(out, fsUsage{
			InstanceIP: instanceIP(s.Metric["instance"]),
			Mountpoint: s.Metric["mountpoint"],
			Device:     s.Metric["device"],
			SizeGB:     s.Value / gib,
			UsedPct:    (1 - av/s.Value) * 100,
		})
	}
	return out, nil
}

// SyncHostDiskUsage 采集任务入口：把 Prometheus 里的文件系统用量写回 host_disks。
//
// 返回 (摘要, 失败列表, 是否全部成功)，与 scheduler 里其他任务一致。
func SyncHostDiskUsage(db *sql.DB, cipher *crypto.Cipher) (string, []TaskFailure, bool) {
	var failures []TaskFailure

	base, token, _, err := resolveEndpointFull(db, cipher, "prometheus", "", 0)
	if err != nil || base == "" {
		// 没接 Prometheus 不是故障，是这套环境还没配 —— 明确说出来，
		// 而不是报一个让人以为采集坏了的错误。
		// ⚠️ 用 ErrTaskSkipped 声明「跳过」，不要 return true。
		// 返回 true 会让这个任务在界面上显示绿色的「正常」，
		// 而它一次都没采到过数据（OPSCMDB-006）
		return "未配置指标数据源，跳过磁盘用量采集。去「管理 → 观测端点」绑定后才会真正采集",
			[]TaskFailure{{Reason: ErrTaskSkipped.Error()}}, true
	}

	fs, err := fetchFSUsage(base, token)
	if err != nil {
		failures = append(failures, TaskFailure{Target: "prometheus", Reason: err.Error()})
		return "磁盘用量采集失败", failures, false
	}
	if len(fs) == 0 {
		return "Prometheus 未返回任何文件系统指标（node_exporter 可能没跑）", nil, true
	}

	// 按 IP 分组，一次遍历配对
	byIP := map[string][]fsUsage{}
	for _, f := range fs {
		if f.InstanceIP != "" {
			byIP[f.InstanceIP] = append(byIP[f.InstanceIP], f)
		}
	}

	rows, err := db.Query(`SELECT h.ci_id, h.internal_ip, d.id, d.size_gb, d.name
		FROM hosts h JOIN host_disks d ON d.host_ci_id = h.ci_id
		WHERE h.stale = 0 AND h.internal_ip <> ''`)
	if err != nil {
		failures = append(failures, TaskFailure{Target: "host_disks", Reason: err.Error()})
		return "读取磁盘台账失败", failures, false
	}
	disksByIP := map[string][]diskRecord{}
	for rows.Next() {
		var ciID, diskID int64
		var ip, name string
		var size int
		if rows.Scan(&ciID, &ip, &diskID, &size, &name) == nil {
			disksByIP[ip] = append(disksByIP[ip], diskRecord{ID: diskID, SizeGB: size, Name: name})
		}
	}
	rows.Close()

	now := time.Now()
	matched, hosts, unmatchedHosts := 0, 0, 0
	for ip, disks := range disksByIP {
		obs, ok := byIP[ip]
		if !ok {
			// 台账里有这台机器，但 Prometheus 里没有它的指标。
			// 常见原因：该集群没接 Prometheus、node_exporter 没部署、IP 变了。
			unmatchedHosts++
			continue
		}
		hosts++
		for diskID, f := range matchDisks(disks, obs) {
			pct := f.UsedPct
			if pct < 0 {
				pct = 0
			} else if pct > 100 {
				pct = 100
			}
			logExec(db, "磁盘用量写",
				`UPDATE host_disks SET used_percent=?, mount_point=?, device_name=?, used_at=? WHERE id=?`,
				pct, f.Mountpoint, f.Device, now, diskID)
			matched++
		}
	}

	// ⚠️ 未匹配数必须出现在摘要里。它是这条链路唯一的健康信号：
	// 采集"成功"但一块也没配上，和采集失败一样糟，区别只是前者不报错。
	msg := fmt.Sprintf("磁盘用量：%d 台主机 %d 块盘已更新", hosts, matched)
	if unmatchedHosts > 0 {
		msg += fmt.Sprintf("；%d 台主机在 Prometheus 里查不到指标", unmatchedHosts)
	}
	if matched == 0 && hosts > 0 {
		msg += "；⚠️ 没有任何盘配对成功，检查容量是否与云上标称差异过大"
		logx.J("磁盘用量", "no_disk_matched", map[string]any{
			"hosts_with_metrics": hosts,
			"hint":               "云盘标称容量与文件系统实际容量差异超过容差，或 node_exporter 只暴露了根分区",
		})
	}
	return msg, failures, len(failures) == 0
}
