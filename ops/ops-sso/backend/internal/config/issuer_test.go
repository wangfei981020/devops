package config

import (
	"errors"
	"strings"
	"testing"
)

func TestResolveIssuer(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		prod    bool
		want    string
		wantErr bool
		warnHas string
	}{
		{name: "生产没配就拒启", raw: "", prod: true, wantErr: true},
		{name: "开发没配退回默认值并警告", raw: "", prod: false, want: DevIssuer,
			warnHas: "未配置 OAP_ISSUER"},

		{name: "生产正常", raw: "https://gate.example.com", prod: true,
			want: "https://gate.example.com"},
		{name: "带路径也可以", raw: "https://example.com/sso", prod: true,
			want: "https://example.com/sso"},

		// 末尾斜杠：肉眼看不出差别，下游却认不出来
		{name: "去掉末尾斜杠并警告", raw: "https://gate.example.com/", prod: true,
			want: "https://gate.example.com", warnHas: "末尾的 /"},
		{name: "多个斜杠也去掉", raw: "https://gate.example.com///", prod: true,
			want: "https://gate.example.com"},

		{name: "生产不许 http", raw: "http://gate.example.com", prod: true, wantErr: true},
		{name: "生产不许 localhost", raw: "https://localhost:30834", prod: true, wantErr: true},
		{name: "生产不许 127.0.0.1", raw: "https://127.0.0.1:8080", prod: true, wantErr: true},
		{name: "生产不许 0.0.0.0", raw: "https://0.0.0.0:8080", prod: true, wantErr: true},

		{name: "不许缺 scheme", raw: "gate.example.com", prod: true, wantErr: true},
		{name: "不许别的 scheme", raw: "ftp://gate.example.com", prod: true, wantErr: true},
		{name: "不许带查询串", raw: "https://gate.example.com?a=1", prod: true, wantErr: true},
		{name: "不许带片段", raw: "https://gate.example.com#x", prod: true, wantErr: true},

		{name: "开发用本机地址不警告", raw: "http://localhost:30834", prod: false,
			want: "http://localhost:30834"},
		{name: "开发指向外部地址要警告", raw: "https://gate.example.com", prod: false,
			want: "https://gate.example.com", warnHas: "拷过来忘了改"},

		{name: "两边空格要吃掉", raw: "  https://gate.example.com  ", prod: true,
			want: "https://gate.example.com"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, warns, err := ResolveIssuer(c.raw, c.prod)
			if c.wantErr {
				if err == nil {
					t.Fatalf("期望报错，却拿到 %q", got)
				}
				if !errors.Is(err, ErrIssuer) {
					t.Fatalf("错误没包 ErrIssuer: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("不该报错: %v", err)
			}
			if got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
			if c.warnHas != "" {
				var hit bool
				for _, w := range warns {
					if strings.Contains(w, c.warnHas) {
						hit = true
					}
				}
				if !hit {
					t.Fatalf("缺警告 %q，实际警告: %v", c.warnHas, warns)
				}
			}
		})
	}
}

// 生产模式下**任何**默认值都是错的：这条单独立一个用例，
// 因为「加个默认值方便一点」是最容易被后来者改回去的地方。
func TestProdNeverFallsBackToDefault(t *testing.T) {
	got, _, err := ResolveIssuer("", true)
	if err == nil {
		t.Fatalf("生产模式没配 OAP_ISSUER 竟然拿到了 %q —— 这会让下游拉一个指向 localhost 的 jwks", got)
	}
}
