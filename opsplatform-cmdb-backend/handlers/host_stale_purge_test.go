package handlers

import "testing"

// 这个判据挡的是**最坏的情况**：同步因为凭据过期/限流/权限被收回而返回空列表，
// 于是整个 project 的主机被一次性标 stale——此时若按 stale 清理，
// 等于把完好的台账整个删掉。
//
// 所以判据要保守到"看着像有问题就跳过"：
// 跳过的代价是记录多留几天，误删的代价是台账没了。两者不对称。
func TestSyncResultLooksBad(t *testing.T) {
	bad := []string{
		"", // 没有结果 = 不知道成没成功
		"同步 0 台，失效 65；⚠️ 网络资源（VPC/防火墙/负载均衡）未同步：403", // 部分失败
		"同步失败：凭据解密失败",
		"error: rate limit exceeded",
		"ERROR: permission denied", // 大小写不敏感
		"同步 10 台，失效 3；请求超时",
		"context deadline exceeded (timeout)",
	}
	for _, s := range bad {
		if !syncResultLooksBad(s) {
			t.Errorf("%q 看着有问题，必须跳过清理——误删台账没法恢复", s)
		}
	}

	good := []string{
		"同步 65 台，失效 35",
		"同步 25 台，失效 15",
		"同步 38 台，失效 0",
	}
	for _, s := range good {
		if syncResultLooksBad(s) {
			t.Errorf("%q 是正常结果，不该阻止清理（否则这个任务永远不干活）", s)
		}
	}
}
