package handlers

import (
	"os"
	"strings"
	"testing"
)

// 🔴 本地 admin **权限校验全放行**。所以它绝不能有默认口令。
//
// 这条测试守的不是某个实现细节，是一条安全边界：
// 「不设 ADMIN_PASSWORD 就不建这个账号」——不建是安全的失败方式
// （少一个兜底入口），而"人人都知道口令的全权账号"不是。
//
// ⚠️ 实测教训：生产 chart 里根本没有这个变量，于是线上 admin 一直用着
// 源码里的 admin123，而且没有任何报错或告警（验收会话 NEW-2）。
func TestNoDefaultAdminPasswordInSource(t *testing.T) {
	raw, err := os.ReadFile("auth.go")
	if err != nil {
		t.Fatalf("读 auth.go: %v", err)
	}
	// ⚠️ 先剥掉注释再判。注释里**应该**写清这个坑（"曾经默认 admin123"），
	//	连注释一起扫的话，把教训写下来反而会让测试变红 ——
	//	那会逼着后人删掉注释来过测试，正好反了。
	src := stripGoComments(string(raw))
	// 🔴 判据要区分**兜底赋值**与**黑名单**：
	//
	//	pw = "admin123"                          ← 兜底，绝不允许
	//	knownWeakPasswords = []string{"admin123"} ← 黑名单，正是用来挡住它的
	//
	//	第一版判据只搜字符串 "admin123"，于是加弱口令黑名单时直接红了 ——
	//	而那次改动恰恰是在**加强**这条边界（OPSCMDB-074）。
	//	一条会因为"你把防御做得更严"而失败的测试，会逼着后人把防御删掉。
	//	所以这里只认赋值形态，并单独确认黑名单确实存在。
	for _, bad := range []string{`pw = "admin`, `pw = "123`, `pw = "password`, `pw := "admin`} {
		if strings.Contains(src, bad) {
			t.Errorf("auth.go 里出现了默认口令的兜底赋值 %q —— "+
				"这个账号权限校验全放行，不能有任何写死的口令兜底", bad)
		}
	}
	// 反过来：黑名单必须在，且必须含那个曾经写死在源码里的口令
	if !strings.Contains(src, "knownWeakPasswords") {
		t.Error("弱口令黑名单不见了 —— 存量账号就再也查不出来了")
	}
	if !strings.Contains(src, `"admin123"`) {
		t.Error("黑名单里没有 admin123 —— 那正是生产 admin 可能仍在用的那个")
	}
}

// 口令强度下限：挡"随手填个 123"。
// ⚠️ 这不是完整密码策略，只是不让一个全放行账号配一个弱口令。
func TestAdminPasswordMinLength(t *testing.T) {
	// 判据直接读源码里的那个数字，避免测试和实现各写一份而分叉
	src, err := os.ReadFile("auth.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "len(pw) < 12") {
		t.Error("auth.go 里没有口令长度下限检查 —— 全放行账号配弱口令和没设密码差别不大")
	}
}

// stripGoComments 去掉 // 与 /* */ 注释。
// 判"源码里有没有写死的口令"时必须先剥注释 —— 否则把踩坑经过写进注释
// 反而会让守卫报警，逼人删注释来过测试。
func stripGoComments(s string) string {
	out := []byte{}
	for i := 0; i < len(s); i++ {
		if s[i] == '/' && i+1 < len(s) {
			if s[i+1] == '/' {
				for i < len(s) && s[i] != '\n' {
					i++
				}
				continue
			}
			if s[i+1] == '*' {
				i += 2
				for i+1 < len(s) && !(s[i] == '*' && s[i+1] == '/') {
					i++
				}
				i++
				continue
			}
		}
		out = append(out, s[i])
	}
	return string(out)
}
