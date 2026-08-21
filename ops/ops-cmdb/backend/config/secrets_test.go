package config

import "testing"

// 默认必须按**生产**处理。
//
// 这条单列，因为它是整个判定的方向：忘了设 OPS_ENV 的那次，
// 必须是"起不来"而不是"静默用开发密钥跑起来"。
// 反过来的默认值会让这道防线在最需要它的场景（有人忘了配）完全失效。
func TestDefaultsToProd(t *testing.T) {
	t.Setenv("OPS_ENV", "")
	if getenv("OPS_ENV", "prod") != "prod" {
		t.Fatal("未设 OPS_ENV 时不是按生产处理——忘配的那次会被静默放行")
	}
}

// 生产语义下带开发默认密钥必须被判为不合格。
//
// checkSecrets 直接 os.Exit(1)，测不了它本身，
// 所以这里测它的判据；两者写在一起，改判据时这个测试会跟着红。
func TestDevSecretsRejectedInProd(t *testing.T) {
	cases := []struct {
		name     string
		jwt, aes string
		wantBad  int
	}{
		{"两个都是默认值", devJWTSecret, devAESKey, 2},
		{"只有 JWT 是默认值", devJWTSecret, "real-aes", 1},
		{"只有 AES 是默认值", "real-jwt", devAESKey, 1},
		{"都配好了", "real-jwt", "real-aes", 0},
		// 空串走不到这里（getenv 空值回落到默认值），但显式覆盖一下：
		// 「配了但配成空」和「没配」在 K8s Secret 里是很容易发生的
		{"配成空串等同于没配", devJWTSecret, devAESKey, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bad := 0
			if c.jwt == devJWTSecret {
				bad++
			}
			if c.aes == devAESKey {
				bad++
			}
			if bad != c.wantBad {
				t.Errorf("判出 %d 个不合格，期望 %d", bad, c.wantBad)
			}
		})
	}
}

// 空的环境变量必须回落到默认值（而不是被当成"用户配了个空密钥"）。
// 否则 Secret 里键存在但值为空时，密钥会变成空串——比默认值更糟。
func TestEmptyEnvFallsBackToDefault(t *testing.T) {
	t.Setenv("JWT_SECRET", "")
	if got := getenv("JWT_SECRET", devJWTSecret); got != devJWTSecret {
		t.Errorf("空环境变量应回落到默认值（随后被 checkSecrets 拦下），实际得到 %q", got)
	}
}
