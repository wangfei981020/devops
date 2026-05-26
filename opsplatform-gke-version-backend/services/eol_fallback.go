package services

// EOL 硬编码映射：minor 版本 → 标准/扩展支持结束日期（YYYY-MM-DD）
// 数据来源：https://cloud.google.com/kubernetes-engine/docs/release-schedule
// 半年同步一次（下次 2026-11）
var eolFallback = map[string]struct {
	Std string
	Ext string
}{
	"1.27": {"2024-06-30", "2025-06-30"},
	"1.28": {"2024-10-31", "2025-10-31"},
	"1.29": {"2025-02-28", "2026-02-28"},
	"1.30": {"2025-06-30", "2026-06-30"},
	"1.31": {"2025-10-31", "2026-10-31"},
	"1.32": {"2026-02-28", "2027-02-28"},
	"1.33": {"2026-08-03", "2027-06-03"},
	"1.34": {"2027-02-28", "2028-02-28"},
}

func EolFallback(minorKey string) (std, ext string, ok bool) {
	v, exist := eolFallback[minorKey]
	if !exist {
		return "", "", false
	}
	return v.Std, v.Ext, true
}
