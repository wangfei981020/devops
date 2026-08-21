package store

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// 拦「拿 Platform() 去查租户表」。
//
// # 为什么值得单独写一道
//
// 这个错误一次带走了四个定时任务：证书自动续期、磁盘水位巡检、
// DNS 同步、主机同步。它们的查询**从多租户改造以后一次都没成功过**，
// 而界面上一直显示「正常」（scheduled_tasks.last_ok 默认值是 1）。
//
// 编译器抓不到（SQL 是字符串），单测也抓不到（没有真库就走不到那行），
// 而运行期只留下一句 `store: 该表不是平台级表` 埋在任务日志里。
// 唯一能在提交时挡下它的就是这种静态扫描。
//
// # 两个刻意的排除，都是踩过才知道的
//
//  1. **注释行不扫**。文档里演示反面写法是有价值的（ForJob 的注释现在就
//     明确写着"下面这种是错的"），扫注释会把这种好文档判成违规，
//     逼人把教训删掉。
//  2. **先切掉 ON DUPLICATE KEY UPDATE 之后的部分**再找表名。
//     `... ON DUPLICATE KEY UPDATE fence = fence + 1` 里的 fence 是**列**，
//     不切的话会判定「fence 不是平台级表」—— 报一个根本不存在的表名，
//     排查时极具误导性。生产代码的 checkPlatformTables 早就这么处理了，
//     检查器抄它的规则，而不是自己另发明一套。
func TestPlatformNotUsedOnTenantTables(t *testing.T) {
	root := ".."
	// Platform("x").Query(ctx, `... FROM 表`) —— 抓 Platform 调用之后那段 SQL 里的表名
	call := regexp.MustCompile(`Platform\([^)]*\)\.\s*(?:Query|QueryRow|Exec)\(`)
	table := regexp.MustCompile(`(?i)\b(?:FROM|JOIN|INTO|UPDATE)\s+` + "`?" + `([a-z_][a-z0-9_]*)` + "`?")

	dupKey := regexp.MustCompile(`(?is)\bon\s+duplicate\s+key\s+update\b`)
	var bad []string
	err := filepath.Walk(filepath.Join(root, ".."), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		// 去掉整行注释：反面示例写在文档里是有意为之的
		lines := strings.Split(string(src), "\n")
		for i, ln := range lines {
			if strings.HasPrefix(strings.TrimSpace(ln), "//") {
				lines[i] = ""
			}
		}
		text := strings.Join(lines, "\n")
		for _, loc := range call.FindAllStringIndex(text, -1) {
			// 取调用之后的一小段，足够覆盖一条 SQL 字面量
			end := loc[1] + 700
			if end > len(text) {
				end = len(text)
			}
			seg := text[loc[1]:end]
			// 只看到下一个语句结束为止，避免把后面无关的 SQL 也算进来
			if i := strings.Index(seg, "\n\tif err"); i > 0 {
				seg = seg[:i]
			}
			// ON DUPLICATE KEY UPDATE 之后是列名，不是表名
			if m := dupKey.FindStringIndex(seg); m != nil {
				seg = seg[:m[0]]
			}
			for _, m := range table.FindAllStringSubmatch(seg, -1) {
				name := strings.ToLower(m[1])
				if name == "" || platformTables[name] {
					continue
				}
				// SQL 关键字与派生表别名不是表名
				switch name {
				case "select", "dual", "t", "t1", "t2", "r", "u", "s":
					continue
				}
				bad = append(bad, filepath.Base(path)+": Platform() 查了租户表 "+name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	if len(bad) > 0 {
		t.Errorf("Platform() 只能查平台级白名单表，下面这些查询在运行期会直接失败：\n  %s\n\n"+
			"跨租户的后台任务用 store.ForEachTenant 逐租户跑（见 foreach.go）。\n"+
			"⚠️ 这类错误不会让编译失败，只会让任务静默不工作 —— 而任务状态列默认值是「正常」。",
			strings.Join(bad, "\n  "))
	}
}
