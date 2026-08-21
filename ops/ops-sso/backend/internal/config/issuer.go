// Package config 是启动期配置的解析与自检。
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// DevIssuer 本地开发时的默认签发方。**只在非生产模式下允许**。
const DevIssuer = "http://localhost:18095"

var ErrIssuer = errors.New("config: OAP_ISSUER 不可用")

// ResolveIssuer 解析并校验对外的 OIDC 签发方标识。
//
// # 为什么它不能有「静默默认值」
//
// 这个值会原样写进每一个 id_token 的 iss，也会写进 discovery 文档里的
// authorization/token/jwks 各个地址。配错了不会有任何报错 ——
// 服务照常启动、登录照常跳转，直到某个下游拿着 token 去校验 iss，
// 或者去拉一个指向 localhost 的 jwks_uri，才会失败。
// 而那时报错出现在**下游**，排查的人根本不会怀疑到 SSO 的一个环境变量上。
//
// 所以生产模式下不给默认值：没配就拒启，把一个"三天后在别人系统里爆炸"的问题
// 变成"现在就起不来"。这和初装弱口令拒启是同一条纪律。
//
// # 为什么不做成后台可改
//
// issuer 一变，**所有已接入的下游都要同步改配置**，否则它们校验 iss 会全部失败；
// 已经签出去的 id_token 也会立刻作废。这不是一个"配置项"，
// 而是一次需要协调所有接入方的变更 —— 放进后台表单，等于给了人一个
// 点一下就能让全公司登不进任何系统的按钮。
// 同类产品（Keycloak / Authentik / Okta）也都是部署期确定、不在管理界面改。
//
// 返回值第二项是**警告**：能用，但很可能不是你想要的，要喊出来。
func ResolveIssuer(raw string, prod bool) (string, []string, error) {
	raw = strings.TrimSpace(raw)

	if raw == "" {
		if prod {
			return "", nil, fmt.Errorf("%w: 生产模式必须显式配置。"+
				"它会写进每个 id_token 的 iss 和 discovery 里的所有地址，"+
				"配错了本服务一切正常、只有下游会失败", ErrIssuer)
		}
		return DevIssuer, []string{
			"未配置 OAP_ISSUER，使用本地默认值 " + DevIssuer + "。接入任何真实下游前必须配成对外可达的地址",
		}, nil
	}

	// 末尾斜杠必须去掉，而且要说出来。
	// iss 的比对是**逐字符相等**，`https://x/` 和 `https://x` 在下游看来是两个不同的签发方，
	// 而肉眼看这两个字符串是"一样的"—— 这类问题最难查。
	var warns []string
	if strings.HasSuffix(raw, "/") {
		raw = strings.TrimRight(raw, "/")
		warns = append(warns, "OAP_ISSUER 末尾的 / 已去掉：iss 是逐字符比对的，多一个斜杠下游就认不出来")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", nil, fmt.Errorf("%w: 解析不了 %q: %v", ErrIssuer, raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", nil, fmt.Errorf("%w: 必须是 http:// 或 https:// 开头的绝对地址，得到 %q", ErrIssuer, raw)
	}
	if u.Host == "" {
		return "", nil, fmt.Errorf("%w: 缺主机名：%q", ErrIssuer, raw)
	}
	// 查询串和片段会被原样拼进 discovery 里的各个地址，拼出来的东西根本不是合法 URL
	if u.RawQuery != "" || u.Fragment != "" {
		return "", nil, fmt.Errorf("%w: 不能带查询串或 #片段：%q", ErrIssuer, raw)
	}

	local := isLoopback(u.Hostname())

	if prod {
		if local {
			return "", nil, fmt.Errorf("%w: 生产模式不能用本机地址 %q —— "+
				"下游是从它自己的容器里去拉 jwks 的，localhost 指向的是它自己", ErrIssuer, u.Host)
		}
		if u.Scheme != "https" {
			// 不放行、也不给开关：OIDC 规范要求 issuer 是 https，
			// 而且这里但凡留个 ALLOW_HTTP，它就一定会被设上。
			// 内网 TLS 由网关终止也没关系 —— 对外那个地址仍然是 https。
			return "", nil, fmt.Errorf("%w: 生产模式必须是 https，得到 %q", ErrIssuer, raw)
		}
	} else if !local {
		// 开发模式指着一个真实域名，通常是把生产配置拷过来忘了改。
		// 不拦，但要喊：这会让本地签出来的 token 冒充生产签发方。
		warns = append(warns, "非生产模式却把 OAP_ISSUER 指向了外部地址 "+raw+"：确认这不是把生产配置拷过来忘了改")
	}

	return raw, warns, nil
}

func isLoopback(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsUnspecified()
	}
	return false
}
