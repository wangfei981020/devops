package handlers

import (
	"encoding/json"
	"strings"
	"testing"
)

// `fields` 参数遇到不存在的字段名，必须**说出来**，不能静默丢弃。
//
// 守的是 OPSCMDB-031 P1-8：
//
//	list_certificates(fields="domain,cn,expires_at,expire_at,issuer,days,…")
//	→ 只有 domain 命中，其余 7 个全被丢掉，且无任何提示
//
// 后果：调用方（AI）拿到一堆只有 domain 的记录，会得出
// 「这些证书没有到期日」的结论 —— 而真相是字段名写错了。
//
// ⚠️ 这是本项目最核心的失效模式：**不报错、不空、只是答非所问**。
// 静默丢弃让"写错了"和"这个字段真的没值"看起来一模一样。
func TestApplyFieldsReportsUnknownNames(t *testing.T) {
	body := `[{"name":"a.com","expiry_at":"2026-09-03","issuer":"R3"},
	          {"name":"b.com","expiry_at":"2026-10-01","issuer":"R3"}]`

	t.Run("字段名全对：形状不变", func(t *testing.T) {
		got := applyFields(body, "name,expiry_at")
		var rows []map[string]any
		if err := json.Unmarshal([]byte(got), &rows); err != nil {
			t.Fatalf("正常调用的响应形状不该变（应仍是数组）：%v\n%s", err, got)
		}
		if len(rows) != 2 || rows[0]["name"] != "a.com" {
			t.Errorf("裁剪结果不对：%s", got)
		}
		if _, ok := rows[0]["issuer"]; ok {
			t.Error("没请求的字段不该返回")
		}
	})

	t.Run("有不存在的字段名：必须报出来", func(t *testing.T) {
		// 🔴 P1-8 的原型：expires_at / days / check_error 都不存在
		got := applyFields(body, "name,expires_at,days,check_error")
		var w struct {
			Items           []map[string]any `json:"items"`
			UnknownFields   []string         `json:"unknown_fields"`
			AvailableFields []string         `json:"available_fields"`
			Warning         string           `json:"warning"`
		}
		if err := json.Unmarshal([]byte(got), &w); err != nil {
			t.Fatalf("有未知字段时应返回带说明的对象：%v\n%s", err, got)
		}
		if len(w.UnknownFields) != 3 {
			t.Errorf("未知字段应有 3 个，实际 %v", w.UnknownFields)
		}
		for _, want := range []string{"expires_at", "days", "check_error"} {
			if !contains2(w.UnknownFields, want) {
				t.Errorf("没报出未知字段 %q：%v", want, w.UnknownFields)
			}
		}
		// 可用字段名要给出来 —— 否则调用方只知道"错了"，不知道该写什么
		for _, want := range []string{"name", "expiry_at", "issuer"} {
			if !contains2(w.AvailableFields, want) {
				t.Errorf("可用字段里少了 %q：%v", want, w.AvailableFields)
			}
		}
		// 警告必须点破那个误读
		if !strings.Contains(w.Warning, "空") {
			t.Errorf("警告要说清「不要把缺字段读成数据是空的」：%q", w.Warning)
		}
		// 数据本身还得在
		if len(w.Items) != 2 || w.Items[0]["name"] != "a.com" {
			t.Errorf("数据丢了：%s", got)
		}
	})

	t.Run("不传 fields：原样返回", func(t *testing.T) {
		if got := applyFields(body, ""); got != body {
			t.Error("不传 fields 时不该改动响应")
		}
	})

	t.Run("空数组：不报未知字段", func(t *testing.T) {
		// 没有任何行时无从判断字段存不存在 —— 这时**不能**说人家写错了
		got := applyFields(`[]`, "whatever")
		if strings.Contains(got, "unknown_fields") {
			t.Errorf("空结果时无从判断字段是否存在，不该报未知：%s", got)
		}
	})
}

func contains2(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
