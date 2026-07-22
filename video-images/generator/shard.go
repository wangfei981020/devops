package main

import (
	"hash/fnv"
	"log"
	"strconv"
	"strings"
)

// OrdinalFromHostname 从 StatefulSet pod 名解析分片序号:
// "video-images-generator-3" -> 3
// 解析不出时兜底为 0 并打 WARN(错误的 pod 会当自己是分片0、还会跑孤儿清理,必须让运维看到)。
func OrdinalFromHostname(h string) int {
	i := strings.LastIndex(h, "-")
	if i < 0 {
		log.Printf("WARN 无法从主机名 %q 解析分片序号(无 - 分隔),兜底为 0,请检查 StatefulSet/POD_NAME", h)
		return 0
	}
	n, err := strconv.Atoi(h[i+1:])
	if err != nil || n < 0 {
		log.Printf("WARN 无法从主机名 %q 解析分片序号(尾段非数字),兜底为 0,请检查 StatefulSet/POD_NAME", h)
		return 0
	}
	return n
}

// Owns 用 rendezvous(HRW)哈希判断某路流是否归本分片管。
// 相比 hash%N,改副本数时只挪 ~1/N 的流,不会全量重排。
// 归属者 = 让 hash(name:shard) 最大的那个 shard。
func Owns(name string, ordinal, total int) bool {
	if total <= 1 {
		return true
	}
	bestShard, best := 0, uint32(0)
	for shard := 0; shard < total; shard++ {
		h := fnv.New32a()
		_, _ = h.Write([]byte(name + ":" + strconv.Itoa(shard)))
		if v := h.Sum32(); shard == 0 || v > best {
			best, bestShard = v, shard
		}
	}
	return bestShard == ordinal
}
