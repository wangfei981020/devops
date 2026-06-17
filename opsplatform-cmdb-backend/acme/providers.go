package acme

import (
	"fmt"

	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/providers/dns/alidns"
	"github.com/go-acme/lego/v4/providers/dns/cloudflare"
	"github.com/go-acme/lego/v4/providers/dns/dnspod"
	"github.com/go-acme/lego/v4/providers/dns/godaddy"
	"github.com/go-acme/lego/v4/providers/dns/tencentcloud"
)

// dnsProvider 把注册商凭据映射成 lego DNS-01 provider。
// cred 是解密后的厂商凭据键值（来自 registrars.credential_enc）。
func dnsProvider(provider string, cred map[string]string) (challenge.Provider, error) {
	switch provider {
	case "dnspod":
		cfg := dnspod.NewDefaultConfig()
		cfg.LoginToken = cred["id"] + "," + cred["token"] // dnspod 格式 "id,token"
		return dnspod.NewDNSProviderConfig(cfg)
	case "aliyun", "alidns":
		cfg := alidns.NewDefaultConfig()
		cfg.APIKey = cred["ak"]
		cfg.SecretKey = cred["sk"]
		return alidns.NewDNSProviderConfig(cfg)
	case "cloudflare":
		cfg := cloudflare.NewDefaultConfig()
		cfg.AuthToken = cred["token"]
		return cloudflare.NewDNSProviderConfig(cfg)
	case "godaddy":
		cfg := godaddy.NewDefaultConfig()
		cfg.APIKey = cred["api_key"]
		cfg.APISecret = cred["api_secret"]
		return godaddy.NewDNSProviderConfig(cfg)
	case "tencent", "tencentcloud":
		cfg := tencentcloud.NewDefaultConfig()
		cfg.SecretID = cred["secret_id"]
		cfg.SecretKey = cred["secret_key"]
		return tencentcloud.NewDNSProviderConfig(cfg)
	}
	return nil, fmt.Errorf("暂不支持的 DNS provider: %q（已支持 dnspod/aliyun/cloudflare/tencent）", provider)
}
