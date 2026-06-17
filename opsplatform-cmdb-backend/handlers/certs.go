package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/acme"
	"opsplatform-cmdb-backend/crypto"
)

type CertHandler struct {
	DB     *sql.DB
	Cipher *crypto.Cipher
}

func NewCertHandler(db *sql.DB, cipher *crypto.Cipher) *CertHandler {
	return &CertHandler{DB: db, Cipher: cipher}
}

func (h *CertHandler) Register(r *gin.RouterGroup) {
	r.GET("/acme-accounts", h.ListAccounts)
	r.POST("/acme-accounts", h.CreateAccount)
	r.DELETE("/acme-accounts/:id", h.DeleteAccount)
	r.GET("/certs", h.List)
	r.GET("/certs/:id", h.Get)
	r.POST("/certs", h.Apply)
	r.POST("/certs/:id/renew", h.Renew)
	r.POST("/certs/:id/dns-ready", h.DNSReady)
	r.DELETE("/certs/:id", h.Revoke)
	r.GET("/certs/:id/download", h.Download)
}

// DNSReady 手动 DNS 验证：用户加好 TXT 记录后点「继续验证」，置位放行签发流程。
func (h *CertHandler) DNSReady(c *gin.Context) {
	if _, err := h.DB.Exec(`UPDATE certificates SET dns_ready=1 WHERE ci_id=?`, c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// ---- ACME 账户 ----

func (h *CertHandler) ListAccounts(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT id, email, ca, CASE WHEN account_key_enc IS NULL OR account_key_enc='' THEN 0 ELSE 1 END, status FROM acme_accounts ORDER BY id`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	type acct struct {
		ID         int    `json:"id"`
		Email      string `json:"email"`
		CA         string `json:"ca"`
		Registered bool   `json:"registered"`
		Status     string `json:"status"`
	}
	out := []acct{}
	for rows.Next() {
		var a acct
		var reg int
		if rows.Scan(&a.ID, &a.Email, &a.CA, &reg, &a.Status) == nil {
			a.Registered = reg == 1
			out = append(out, a)
		}
	}
	c.JSON(http.StatusOK, out)
}

func (h *CertHandler) CreateAccount(c *gin.Context) {
	var in struct {
		Email string `json:"email"`
		CA    string `json:"ca"`
	}
	// 邮箱可选：Let's Encrypt 不强制；到期提醒走飞书。空邮箱时注册无 contact 的 ACME 账户。
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if in.CA == "" {
		in.CA = "letsencrypt"
	}
	res, err := h.DB.Exec(`INSERT INTO acme_accounts (email, ca) VALUES (?, ?)`, in.Email, in.CA)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	c.JSON(201, gin.H{"id": id})
}

func (h *CertHandler) DeleteAccount(c *gin.Context) {
	if _, err := h.DB.Exec(`DELETE FROM acme_accounts WHERE id=?`, c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// ---- 证书 ----

func (h *CertHandler) List(c *gin.Context) {
	rows, err := h.DB.Query(`
		SELECT c.id, c.name, c.project, c.env, c.module, c.owner,
		       t.cn, COALESCE(t.sans,'[]'), t.ca, t.challenge, t.status, t.expiry_at,
		       t.auto_renew, t.renew_days, t.last_error
		FROM cis c JOIN certificates t ON t.ci_id=c.id
		WHERE c.type='certificate' ORDER BY c.id DESC`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	type certOut struct {
		CIID      int64    `json:"ci_id"`
		Name      string   `json:"name"`
		Project   string   `json:"project"`
		Env       string   `json:"env"`
		Module    string   `json:"module"`
		Owner     string   `json:"owner"`
		CN        string   `json:"cn"`
		SANs      []string `json:"sans"`
		CA        string   `json:"ca"`
		Challenge string   `json:"challenge"`
		Status    string   `json:"status"`
		ExpiryAt  string   `json:"expiry_at"`
		AutoRenew int      `json:"auto_renew"`
		RenewDays int      `json:"renew_days"`
		LastError string   `json:"last_error"`
	}
	out := []certOut{}
	for rows.Next() {
		var o certOut
		var sans string
		var exp sql.NullTime
		if err := rows.Scan(&o.CIID, &o.Name, &o.Project, &o.Env, &o.Module, &o.Owner,
			&o.CN, &sans, &o.CA, &o.Challenge, &o.Status, &exp, &o.AutoRenew, &o.RenewDays, &o.LastError); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		_ = json.Unmarshal([]byte(sans), &o.SANs)
		if exp.Valid {
			o.ExpiryAt = exp.Time.Format("2006-01-02")
		}
		out = append(out, o)
	}
	c.JSON(http.StatusOK, out)
}

func (h *CertHandler) Get(c *gin.Context) {
	id := c.Param("id")
	var o struct {
		CIID      int64    `json:"ci_id"`
		CN        string   `json:"cn"`
		SANs      []string `json:"sans"`
		CA        string   `json:"ca"`
		Challenge string   `json:"challenge"`
		Status    string   `json:"status"`
		IssuedAt  string   `json:"issued_at"`
		ExpiryAt  string   `json:"expiry_at"`
		AutoRenew   int     `json:"auto_renew"`
		RenewDays   int     `json:"renew_days"`
		LastError      string  `json:"last_error"`
		DeployToken    string  `json:"deploy_token"`
		ChallengeFqdn  string  `json:"challenge_fqdn"`
		ChallengeValue string  `json:"challenge_value"`
		History        []gin.H `json:"history"`
	}
	var sans string
	var issued, exp sql.NullTime
	err := h.DB.QueryRow(`SELECT ci_id, cn, COALESCE(sans,'[]'), ca, challenge, status, issued_at, expiry_at, auto_renew, renew_days, last_error, deploy_token, challenge_fqdn, challenge_value
		FROM certificates WHERE ci_id=?`, id).
		Scan(&o.CIID, &o.CN, &sans, &o.CA, &o.Challenge, &o.Status, &issued, &exp, &o.AutoRenew, &o.RenewDays, &o.LastError, &o.DeployToken, &o.ChallengeFqdn, &o.ChallengeValue)
	if err == sql.ErrNoRows {
		c.JSON(404, gin.H{"error": "not found"})
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	_ = json.Unmarshal([]byte(sans), &o.SANs)
	if issued.Valid {
		o.IssuedAt = issued.Time.Format("2006-01-02 15:04")
	}
	if exp.Valid {
		o.ExpiryAt = exp.Time.Format("2006-01-02")
	}
	o.History = []gin.H{}
	hr, _ := h.DB.Query(`SELECT action, result, detail, at FROM cert_history WHERE cert_ci_id=? ORDER BY id DESC LIMIT 20`, id)
	if hr != nil {
		defer hr.Close()
		for hr.Next() {
			var a, r, d string
			var at time.Time
			if hr.Scan(&a, &r, &d, &at) == nil {
				o.History = append(o.History, gin.H{"action": a, "result": r, "detail": d, "at": at.Format("2006-01-02 15:04")})
			}
		}
	}
	c.JSON(http.StatusOK, o)
}

type certApplyIn struct {
	CN           string            `json:"cn"`
	SANs         []string          `json:"sans"`
	CA           string            `json:"ca"`
	Challenge    string            `json:"challenge"`
	DomainCIID   int64             `json:"domain_ci_id"`
	ACMEAccount  int               `json:"acme_account_id"`
	Project      string            `json:"project"`
	Env          string            `json:"env"`
	Module       string            `json:"module"`
	Owner        string            `json:"owner"`
	AutoRenew    int               `json:"auto_renew"`
	RenewDays    int               `json:"renew_days"`
	Staging      bool              `json:"staging"`
	Labels       map[string]string `json:"labels"`
}

func (h *CertHandler) Apply(c *gin.Context) {
	var in certApplyIn
	if err := c.ShouldBindJSON(&in); err != nil || in.CN == "" || in.ACMEAccount == 0 {
		c.JSON(400, gin.H{"error": "cn 与 acme_account_id 必填"})
		return
	}
	if in.CA == "" {
		in.CA = "letsencrypt"
	}
	if in.Challenge == "" {
		in.Challenge = "dns-01"
	}
	if in.RenewDays == 0 {
		in.RenewDays = 30
	}
	sansJSON, _ := json.Marshal(in.SANs)
	tx, err := h.DB.Begin()
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	res, err := tx.Exec(`INSERT INTO cis (type, name, project, env, module, owner, status) VALUES ('certificate', ?, ?, ?, ?, ?, 'active')`,
		in.CN, in.Project, in.Env, in.Module, in.Owner)
	if err != nil {
		tx.Rollback()
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	certCIID, _ := res.LastInsertId()
	if _, err := tx.Exec(`INSERT INTO certificates (ci_id, cn, sans, ca, challenge, status, auto_renew, renew_days, deploy_token, version)
		VALUES (?, ?, ?, ?, ?, 'pending', ?, ?, ?, 0)`,
		certCIID, in.CN, string(sansJSON), in.CA, in.Challenge, in.AutoRenew, in.RenewDays, randToken()); err != nil {
		tx.Rollback()
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if in.DomainCIID > 0 {
		_, _ = tx.Exec(`INSERT IGNORE INTO ci_relations (src_ci_id, dst_ci_id, rel_type) VALUES (?, ?, 'protects')`, certCIID, in.DomainCIID)
	}
	if err := tx.Commit(); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	replaceLabelsDB(h.DB, certCIID, in.Labels)
	WriteAudit(h.DB, c, "apply_cert", in.CN)
	go h.runIssue(certCIID, in.DomainCIID, in.ACMEAccount, in.Staging, "issue")
	c.JSON(202, gin.H{"ci_id": certCIID, "status": "pending"})
}

func (h *CertHandler) Renew(c *gin.Context) {
	certCIID, err := parseID(c.Param("id"))
	if err != nil {
		c.JSON(400, gin.H{"error": "bad id"})
		return
	}
	var domainCIID int64
	_ = h.DB.QueryRow(`SELECT dst_ci_id FROM ci_relations WHERE src_ci_id=? AND rel_type='protects' LIMIT 1`, certCIID).Scan(&domainCIID)
	var ca string
	_ = h.DB.QueryRow(`SELECT ca FROM certificates WHERE ci_id=?`, certCIID).Scan(&ca)
	var acctID int
	if err := h.DB.QueryRow(`SELECT id FROM acme_accounts WHERE ca=? ORDER BY id LIMIT 1`, ca).Scan(&acctID); err != nil {
		c.JSON(400, gin.H{"error": "找不到匹配的 ACME 账户"})
		return
	}
	_, _ = h.DB.Exec(`UPDATE certificates SET status='pending' WHERE ci_id=?`, certCIID)
	go h.runIssue(certCIID, domainCIID, acctID, false, "renew")
	WriteAudit(h.DB, c, "renew_cert", c.Param("id"))
	c.JSON(202, gin.H{"status": "pending"})
}

func (h *CertHandler) Revoke(c *gin.Context) {
	id := c.Param("id")
	tx, _ := h.DB.Begin()
	for _, stmt := range []string{
		`DELETE FROM ci_labels WHERE ci_id=?`,
		`DELETE FROM certificates WHERE ci_id=?`,
		`DELETE FROM cis WHERE id=? AND type='certificate'`,
	} {
		if _, err := tx.Exec(stmt, id); err != nil {
			tx.Rollback()
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
	}
	_, _ = tx.Exec(`DELETE FROM ci_relations WHERE src_ci_id=? OR dst_ci_id=?`, id, id)
	if err := tx.Commit(); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	WriteAudit(h.DB, c, "revoke_cert", id)
	c.JSON(200, gin.H{"ok": true})
}

// Download 登录态下载证书（fullchain + 解密私钥）。
func (h *CertHandler) Download(c *gin.Context) {
	id := c.Param("id")
	var cert, chain, keyEnc string
	err := h.DB.QueryRow(`SELECT COALESCE(cert_pem,''), COALESCE(chain_pem,''), COALESCE(key_pem_enc,'') FROM certificates WHERE ci_id=?`, id).
		Scan(&cert, &chain, &keyEnc)
	if err != nil {
		c.JSON(404, gin.H{"error": "not found"})
		return
	}
	key := ""
	if keyEnc != "" {
		key, _ = h.Cipher.Decrypt(keyEnc)
	}
	WriteAudit(h.DB, c, "download_cert", id)
	c.JSON(http.StatusOK, gin.H{"fullchain": cert, "chain": chain, "key": key})
}

func (h *CertHandler) runIssue(certCIID, domainCIID int64, acctID int, staging bool, action string) {
	issueCertCore(h.DB, h.Cipher, certCIID, domainCIID, acctID, staging, action)
}

// issueCertCore 执行签发/续期并更新证书状态（CertHandler 与 scheduler 共用）。返回错误信息（成功为空）。
func issueCertCore(db *sql.DB, cipher *crypto.Cipher, certCIID, domainCIID int64, acctID int, staging bool, action string) string {
	var cn, sansJSON, ca, challenge string
	if err := db.QueryRow(`SELECT cn, COALESCE(sans,'[]'), ca, challenge FROM certificates WHERE ci_id=?`, certCIID).
		Scan(&cn, &sansJSON, &ca, &challenge); err != nil {
		return err.Error()
	}
	var sans []string
	_ = json.Unmarshal([]byte(sansJSON), &sans)
	domains := append([]string{cn}, sans...)

	var provider string
	var cred map[string]string
	if challenge != "http-01" && challenge != "manual-dns" && domainCIID > 0 {
		var regID sql.NullInt64
		_ = db.QueryRow(`SELECT registrar_id FROM domains WHERE ci_id=?`, domainCIID).Scan(&regID)
		if regID.Valid {
			provider, cred, _ = LoadCredential(db, cipher, int(regID.Int64))
		}
	}

	var email, acctKeyEnc string
	_ = db.QueryRow(`SELECT email, COALESCE(account_key_enc,'') FROM acme_accounts WHERE id=?`, acctID).Scan(&email, &acctKeyEnc)
	acctKey := ""
	if acctKeyEnc != "" {
		acctKey, _ = cipher.Decrypt(acctKeyEnc)
	}

	req := acme.IssueRequest{
		Domains: domains, Challenge: challenge, CADir: acme.CADir(ca, staging),
		AccountEmail: email, AccountKeyPEM: acctKey, DNSProvider: provider, DNSCred: cred,
	}
	if challenge == "manual-dns" {
		req.ChallengeProvider = &acme.ManualDNSProvider{DB: db, CertCIID: certCIID}
	}
	res, err := acme.Issue(req)
	if err != nil {
		_, _ = db.Exec(`UPDATE certificates SET status='error', last_error=? WHERE ci_id=?`, truncate(err.Error(), 500), certCIID)
		_, _ = db.Exec(`INSERT INTO cert_history (cert_ci_id, action, result, detail) VALUES (?, ?, 'fail', ?)`, certCIID, action, truncate(err.Error(), 500))
		return err.Error()
	}
	keyEnc, _ := cipher.Encrypt(res.KeyPEM)
	_, _ = db.Exec(`UPDATE certificates SET status='active', cert_pem=?, chain_pem=?, key_pem_enc=?, issued_at=NOW(), expiry_at=?, version=version+1, last_error='' WHERE ci_id=?`,
		res.CertPEM, res.ChainPEM, keyEnc, res.NotAfter, certCIID)
	if acctKeyEnc == "" && res.AccountKeyPEM != "" {
		enc, _ := cipher.Encrypt(res.AccountKeyPEM)
		_, _ = db.Exec(`UPDATE acme_accounts SET account_key_enc=? WHERE id=?`, enc, acctID)
	}
	_, _ = db.Exec(`INSERT INTO cert_history (cert_ci_id, action, result, detail) VALUES (?, ?, 'success', ?)`, certCIID, action, "到期 "+res.NotAfter.Format("2006-01-02"))
	return ""
}

func randToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
