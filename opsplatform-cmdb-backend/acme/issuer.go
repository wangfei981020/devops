// Package acme 用 go-acme/lego 封装证书签发/续期（DNS-01 复用注册商凭据，HTTP-01 兜底）。
package acme

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"time"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/challenge/dns01"
	"github.com/go-acme/lego/v4/challenge/http01"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/registration"

	"opsplatform-cmdb-backend/logx"
)

// CA 目录 URL
const (
	CALetsEncryptProd    = lego.LEDirectoryProduction
	CALetsEncryptStaging = lego.LEDirectoryStaging
	CAZeroSSL            = "https://acme.zerossl.com/v2/DV90"
)

// CADir 按 ca 名 + 是否 staging 返回目录 URL。
func CADir(ca string, staging bool) string {
	if ca == "zerossl" {
		return CAZeroSSL
	}
	if staging {
		return CALetsEncryptStaging
	}
	return CALetsEncryptProd
}

type acmeUser struct {
	email string
	reg   *registration.Resource
	key   crypto.PrivateKey
}

func (u *acmeUser) GetEmail() string                        { return u.email }
func (u *acmeUser) GetRegistration() *registration.Resource { return u.reg }
func (u *acmeUser) GetPrivateKey() crypto.PrivateKey        { return u.key }

// IssueRequest 一次签发/续期请求
type IssueRequest struct {
	Domains       []string // CN + SANs
	Challenge     string   // dns-01 / http-01
	CADir         string
	AccountEmail  string
	AccountKeyPEM string // 已有账户私钥(续期复用)；空=新建
	DNSProvider   string
	DNSCred       map[string]string
	// 自定义 DNS-01 provider（如手动 DNS）。设置则优先于 DNSProvider/Challenge。
	ChallengeProvider challenge.Provider
}

// IssueResult 签发结果
type IssueResult struct {
	CertPEM       string // fullchain
	ChainPEM      string // issuer chain
	KeyPEM        string // 证书私钥
	AccountKeyPEM string // 账户私钥(回存复用)
	NotAfter      time.Time
}

// loggingProvider 包装真实 DNS-01 provider，把每次写/删 TXT 的目标 zone、
// EffectiveFQDN、TXT 值与底层写入返回打到日志，用于排查"自动签发 LE 查不到 TXT(403)"。
type loggingProvider struct {
	inner challenge.Provider
}

func (l *loggingProvider) Present(domain, token, keyAuth string) error {
	info := dns01.GetChallengeInfo(domain, keyAuth)
	zone, zerr := dns01.FindZoneByFqdn(info.EffectiveFQDN)
	logx.Line("acme", fmt.Sprintf("[acme][present] domain=%s effectiveFQDN=%s txtValue=%s -> FindZone=%q zoneErr=%v",
		domain, info.EffectiveFQDN, info.Value, zone, zerr))
	err := l.inner.Present(domain, token, keyAuth)
	logx.Line("acme", fmt.Sprintf("[acme][present] domain=%s 底层 provider 写入返回 err=%v", domain, err))
	return err
}

func (l *loggingProvider) CleanUp(domain, token, keyAuth string) error {
	err := l.inner.CleanUp(domain, token, keyAuth)
	logx.Line("acme", fmt.Sprintf("[acme][cleanup] domain=%s err=%v", domain, err))
	return err
}

func (l *loggingProvider) Timeout() (timeout, interval time.Duration) {
	// 强制 5 分钟传播等待 + 每 10s 轮询：GoDaddy 传播慢且各节点不均匀，
	// Let's Encrypt 正式环境多点验证对传播很敏感，给足时间让 TXT 同步到所有权威节点再让 CA 验证。
	// 不再沿用底层 provider 的默认(GoDaddy 仅 2 分钟)，避免 _acme-challenge NXDOMAIN。
	return 5 * time.Minute, 10 * time.Second
}

// Issue 执行一次 ACME 签发。
func Issue(req IssueRequest) (*IssueResult, error) {
	logx.Line("acme", fmt.Sprintf("[acme] 开始签发 domains=%v challenge=%q caDir=%s dnsProvider=%q", req.Domains, req.Challenge, req.CADir, req.DNSProvider))
	key, accountPEM, err := loadOrGenKey(req.AccountKeyPEM)
	if err != nil {
		return nil, err
	}
	user := &acmeUser{email: req.AccountEmail, key: key}

	cfg := lego.NewConfig(user)
	cfg.CADirURL = req.CADir
	cfg.Certificate.KeyType = certcrypto.RSA2048

	client, err := lego.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("lego client: %w", err)
	}

	switch {
	case req.ChallengeProvider != nil:
		if err := client.Challenge.SetDNS01Provider(req.ChallengeProvider); err != nil {
			return nil, err
		}
	case req.Challenge == "http-01":
		if err := client.Challenge.SetHTTP01Provider(http01.NewProviderServer("", "80")); err != nil {
			return nil, err
		}
	default:
		p, err := dnsProvider(req.DNSProvider, req.DNSCred)
		if err != nil {
			return nil, err
		}
		if err := client.Challenge.SetDNS01Provider(&loggingProvider{inner: p}); err != nil {
			return nil, err
		}
	}

	reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
	if err != nil {
		return nil, fmt.Errorf("acme register: %w", err)
	}
	user.reg = reg

	res, err := client.Certificate.Obtain(certificate.ObtainRequest{Domains: req.Domains, Bundle: true})
	if err != nil {
		logx.Line("acme", fmt.Sprintf("[acme] Obtain 失败 domains=%v: %+v", req.Domains, err))
		return nil, fmt.Errorf("obtain: %w", err)
	}

	notAfter := parseNotAfter(res.Certificate)
	return &IssueResult{
		CertPEM:       string(res.Certificate),
		ChainPEM:      string(res.IssuerCertificate),
		KeyPEM:        string(res.PrivateKey),
		AccountKeyPEM: accountPEM,
		NotAfter:      notAfter,
	}, nil
}

// Revoke 用账户私钥向 CA 真吊销证书。accountKeyPEM 为空则无法吊销（返回错误）。
func Revoke(accountKeyPEM, caDir, certPEM string) error {
	if accountKeyPEM == "" {
		return fmt.Errorf("该证书无关联 ACME 账户私钥，无法向 CA 吊销")
	}
	if certPEM == "" {
		return fmt.Errorf("无证书内容，无法吊销")
	}
	key, _, err := loadOrGenKey(accountKeyPEM)
	if err != nil {
		return fmt.Errorf("账户私钥解析失败: %w", err)
	}
	user := &acmeUser{key: key}
	cfg := lego.NewConfig(user)
	cfg.CADirURL = caDir
	client, err := lego.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("lego client: %w", err)
	}
	// 用已有账户私钥解析账户注册信息（吊销需账户身份签名）
	reg, err := client.Registration.ResolveAccountByKey()
	if err != nil {
		// 兜底：账户可能未在该目录注册过，尝试注册（对已存在账户幂等）
		reg, err = client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
		if err != nil {
			return fmt.Errorf("解析/注册 ACME 账户失败: %w", err)
		}
	}
	user.reg = reg
	if err := client.Certificate.Revoke([]byte(certPEM)); err != nil {
		return fmt.Errorf("CA 吊销失败: %w", err)
	}
	logx.Line("acme", fmt.Sprintf("[acme][revoke] 已向 CA 吊销证书 caDir=%s", caDir))
	return nil
}

func loadOrGenKey(existing string) (crypto.PrivateKey, string, error) {
	if existing != "" {
		blk, _ := pem.Decode([]byte(existing))
		if blk == nil {
			return nil, "", fmt.Errorf("bad account key pem")
		}
		k, err := x509.ParseECPrivateKey(blk.Bytes)
		if err != nil {
			return nil, "", err
		}
		return k, existing, nil
	}
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, "", err
	}
	der, _ := x509.MarshalECPrivateKey(k)
	pemStr := string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}))
	return k, pemStr, nil
}

func parseNotAfter(certPEM []byte) time.Time {
	blk, _ := pem.Decode(certPEM)
	if blk == nil {
		return time.Time{}
	}
	cert, err := x509.ParseCertificate(blk.Bytes)
	if err != nil {
		return time.Time{}
	}
	return cert.NotAfter
}
