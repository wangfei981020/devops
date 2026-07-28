package handlers

import "testing"

// ownedIngressIPs 要能把逗号分隔的多地址拆开——k8s_services.external_ip 就是这种形态，
// 拆不开会导致整条记录匹配不上，把正常域名误报成「解析到已下线 IP」。
func TestOwnedIPSplitting(t *testing.T) {
	owned := map[string]bool{}
	add := func(s string) {
		for _, v := range splitCSV(s) {
			owned[v] = true
		}
	}
	add("34.92.20.123")
	add("10.170.48.103, 10.170.48.18") // 带空格的多地址
	add("")                            // 空值不该产生空 key
	add("  35.220.194.139  ")          // 前后空格

	for _, ip := range []string{"34.92.20.123", "10.170.48.103", "10.170.48.18", "35.220.194.139"} {
		if !owned[ip] {
			t.Errorf("%q 应被识别为我方入口 IP", ip)
		}
	}
	if owned[""] {
		t.Error("空字符串不该进入入口 IP 集合，否则空 content 会被误判为已知")
	}
	if len(owned) != 4 {
		t.Errorf("入口 IP 数量应为 4，实际 %d: %v", len(owned), owned)
	}
}

// CDN 同步用的批量插入：空数据不该拼出非法 SQL。
func TestTxInsertEmptyRows(t *testing.T) {
	// rows 为空时直接返回 nil，不碰数据库——传 nil tx 也不该 panic
	if err := txInsert(nil, "cdn_zones", []string{"a", "b"}, nil); err != nil {
		t.Errorf("空数据应直接返回 nil，实际 %v", err)
	}
	if err := txInsert(nil, "cdn_zones", []string{"a", "b"}, [][]any{}); err != nil {
		t.Errorf("空切片应直接返回 nil，实际 %v", err)
	}
}
