package acme

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/go-acme/lego/v4/challenge/dns01"
)

// ManualDNSProvider 手动 DNS-01：把待添加的 TXT 记录写库展示给用户，
// 然后阻塞轮询，等用户在 DNS 控制台加好记录、在前端点「继续验证」(置 dns_ready=1) 后放行。
// 适用于 DNS 厂商无 API（如 GoDaddy API 受限）的场景。
type ManualDNSProvider struct {
	DB       *sql.DB
	CertCIID int64
}

func (p *ManualDNSProvider) Present(domain, token, keyAuth string) error {
	info := dns01.GetChallengeInfo(domain, keyAuth)
	if _, err := p.DB.Exec(
		`UPDATE certificates SET status='await_dns', challenge_fqdn=?, challenge_value=?, dns_ready=0 WHERE ci_id=?`,
		info.FQDN, info.Value, p.CertCIID); err != nil {
		return err
	}
	// 最多等 20 分钟（给用户去 DNS 控制台加记录 + DNS 生效）
	deadline := time.Now().Add(20 * time.Minute)
	for time.Now().Before(deadline) {
		var ready int
		_ = p.DB.QueryRow(`SELECT dns_ready FROM certificates WHERE ci_id=?`, p.CertCIID).Scan(&ready)
		if ready == 1 {
			// 再等 10 秒让 DNS 记录传播（真正的传播等待在 Timeout() 里，这里只是缓冲）
			time.Sleep(10 * time.Second)
			return nil
		}
		time.Sleep(5 * time.Second)
	}
	return fmt.Errorf("等待手动添加 DNS TXT 记录超时（20 分钟）")
}

// Timeout 传播等待。
//
//	🔴 不实现这个方法的话，lego 会退回**默认的 60 秒**——这正是手动模式
//	同样报 _acme-challenge NXDOMAIN 的原因：用户点完「继续验证」只缓冲 10 秒，
//	再给 60 秒探测就通知 CA 了，而 GoDaddy 托管区的负缓存 TTL 是 600 秒。
//	自动模式有 loggingProvider 兜着，手动模式没有，反而比自动更容易失败。
//	这里和自动路径用同一组参数，两条路行为一致。
func (p *ManualDNSProvider) Timeout() (timeout, interval time.Duration) {
	return dns01PropagationTimeout, dns01PollInterval
}

func (p *ManualDNSProvider) CleanUp(domain, token, keyAuth string) error {
	_, _ = p.DB.Exec(`UPDATE certificates SET challenge_fqdn='', challenge_value='', dns_ready=0 WHERE ci_id=?`, p.CertCIID)
	return nil
}
