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
			// 再等 10 秒让 DNS 记录传播
			time.Sleep(10 * time.Second)
			return nil
		}
		time.Sleep(5 * time.Second)
	}
	return fmt.Errorf("等待手动添加 DNS TXT 记录超时（20 分钟）")
}

func (p *ManualDNSProvider) CleanUp(domain, token, keyAuth string) error {
	_, _ = p.DB.Exec(`UPDATE certificates SET challenge_fqdn='', challenge_value='', dns_ready=0 WHERE ci_id=?`, p.CertCIID)
	return nil
}
