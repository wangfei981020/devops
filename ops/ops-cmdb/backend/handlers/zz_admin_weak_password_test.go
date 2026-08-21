package handlers

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// ★ 已知弱口令自检（OPSCMDB-074）。
//
// 🔴 这条要防的不是"能不能设弱口令"，而是**产品对自己的后门有没有答案**。
//
//	修复 OPSCMDB-050 时只处理了「admin 不存在时不建账号」，
//	而生产那个 admin 是在此之前用旧逻辑建的 —— 默认口令至今有效，
//	且 EnsureAdmin 对存量账号直接 return，不校验不提示。
//	实测 grep：代码里没有任何 must_change_password / weak_password 逻辑。
func TestKnownWeakPasswords(t *testing.T) {
	t.Run("默认口令必须在表里", func(t *testing.T) {
		// admin123 是曾经写死在源码里的那个 —— 它必须被认出来，
		// 否则这次修复对生产那个账号毫无作用
		found := false
		for _, w := range knownWeakPasswords {
			if w == "admin123" {
				found = true
			}
		}
		if !found {
			t.Fatal("admin123 不在弱口令表里 —— 那正是生产 admin 可能仍在用的那个")
		}
	})

	t.Run("bcrypt 比对能认出弱口令", func(t *testing.T) {
		hash, err := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
		if err != nil {
			t.Fatal(err)
		}
		hit := false
		for _, w := range knownWeakPasswords {
			if bcrypt.CompareHashAndPassword(hash, []byte(w)) == nil {
				hit = true
				break
			}
		}
		if !hit {
			t.Error("哈希是 admin123 生成的，却没被认出来")
		}
	})

	t.Run("强口令不能被误报", func(t *testing.T) {
		// ⚠️ 误报的代价是真实的：界面上挂一条"你的后门是公开的"红字，
		//	而实际口令很强 —— 人会开始忽略这条提示，下次真出问题也不看了
		hash, err := bcrypt.GenerateFromPassword([]byte("Xk9#mQ2vL7pR@wZ4"), bcrypt.DefaultCost)
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range knownWeakPasswords {
			if bcrypt.CompareHashAndPassword(hash, []byte(w)) == nil {
				t.Errorf("强口令被误判成弱口令 %q", w)
			}
		}
	})
}
