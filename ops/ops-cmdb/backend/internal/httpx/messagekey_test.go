package httpx

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 语言包相对本包的位置。跟着目录搬迁时要改这里。
const localesDir = "../../../../packages/i18n/locales"

// 每个错误码的 message_key 必须在**所有**语言包里真实存在。
//
// # 为什么值得一条测试
//
// 这类缺失是**静默**的：i18next 查不到 key 就把 key 本身原样显示出来，
// 不报错、不告警。界面上会出现一行 "error.licenseInvalid"，
// 而后端日志一切正常 —— 客户截图来问，我们才知道。
//
// 实际就漏过一个：授权激活失败用的 "error.licenseInvalid" 从来没进过语言包，
// 而那恰恰是最需要把话说清楚的场景（是粘错了？过期了？还是买的不是这个产品？）。
//
// 这条只覆盖 messageKey 表。handler 里用 FailKey 直接传的 key 覆盖不到 ——
// 那种写法本身就该少用，需要自定义文案时优先往 messageKey 里加一个码。
func TestMessageKeysExistInLocales(t *testing.T) {
	entries, err := os.ReadDir(localesDir)
	if err != nil {
		t.Fatalf("读不到语言包目录 %s: %v（目录搬迁了就改 localesDir）", localesDir, err)
	}

	checked := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(localesDir, e.Name(), "common.json")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("语言包 %s 读不到: %v", path, err)
			continue
		}
		var pack map[string]any
		if err := json.Unmarshal(raw, &pack); err != nil {
			t.Errorf("语言包 %s 不是合法 JSON: %v", path, err)
			continue
		}
		checked++
		for code, key := range messageKey {
			if !lookup(pack, key) {
				t.Errorf("[%s] 错误码 %q 的文案 key %q 不在语言包里 —— "+
					"界面会把这串 key 原样显示给客户，且不报任何错", e.Name(), code, key)
			}
		}
	}
	if checked == 0 {
		t.Fatal("一个语言包都没检查到 —— 这条测试等于没跑")
	}
}

// lookup 按点分路径在嵌套 map 里找一个字符串值。
func lookup(pack map[string]any, key string) bool {
	var cur any = pack
	for _, seg := range strings.Split(key, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return false
		}
		if cur, ok = m[seg]; !ok {
			return false
		}
	}
	_, ok := cur.(string)
	return ok
}
