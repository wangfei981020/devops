package services

// ResolveEOL: 给定版本字符串，返回 std/ext 支持结束日期 YYYY-MM-DD
// 优先从 apiEOL（GCP API 返回的 map：version → {std, ext}）取，命中不到回退硬编码（按 minor）
func ResolveEOL(version string, apiEOL map[string]struct{ Std, Ext string }) (std, ext string) {
	if apiEOL != nil {
		if v, ok := apiEOL[version]; ok && v.Std != "" {
			return v.Std, v.Ext
		}
	}
	parsed, err := ParseVersion(version)
	if err != nil {
		return "", ""
	}
	s, e, ok := EolFallback(parsed.MinorKey())
	if !ok {
		return "", ""
	}
	return s, e
}
