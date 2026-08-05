package handlers

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/acme"
	"opsplatform-cmdb-backend/crypto"
	"opsplatform-cmdb-backend/logx"
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
	AuditCreated(c, "acme_accounts", id)
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
		AutoRenew int      `json:"auto_renew"`
		RenewDays int      `json:"renew_days"`
		LastError string   `json:"last_error"`
		// ErrorReason 把上面那串 ACME 原始报文归纳成「是什么问题 + 该怎么办 + 值不值得重试」。
		// 原始报文照旧留着（追溯要用），这是加一层解释而不是替换。
		ErrorReason    *AcmeReason `json:"error_reason,omitempty"`
		DeployToken    string      `json:"deploy_token"`
		ChallengeFqdn  string      `json:"challenge_fqdn"`
		ChallengeValue string      `json:"challenge_value"`
		History        []gin.H     `json:"history"`
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
	// ⚠️ deploy_token 是**证书私钥的通行证**，不是普通字段。
	//
	//	它给目标机自助拉证书用（GET /certs/:id/bundle 拿它自鉴权，那个路由
	//	刻意不走登录中间件）。所以谁看见了这个 token，谁就能免登录下载私钥；
	//	而证书列表能枚举 ci_id，逐个翻即可把全部私钥导出。
	//
	//	原来它对任何有 menu:cmdb_certs 的人可见——包括只读账号。只读账号能
	//	导出生产证书私钥，这是实测出来的 P0（CMDB-033）。
	//	现在只发给能管证书的人；其余人拿到掩码值，界面照样能显示"已生成"。
	if !HasPerm(c, "cmdb:manage_certs") {
		o.DeployToken = maskToken(o.DeployToken)
	}
	o.ErrorReason = classifyAcmeError(o.LastError, o.Challenge, o.CN)
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
	CN          string            `json:"cn"`
	SANs        []string          `json:"sans"`
	CA          string            `json:"ca"`
	Challenge   string            `json:"challenge"`
	DomainCIID  int64             `json:"domain_ci_id"`
	ACMEAccount int               `json:"acme_account_id"`
	Project     string            `json:"project"`
	Env         string            `json:"env"`
	Module      string            `json:"module"`
	Owner       string            `json:"owner"`
	AutoRenew   int               `json:"auto_renew"`
	RenewDays   int               `json:"renew_days"`
	Staging     bool              `json:"staging"`
	Labels      map[string]string `json:"labels"`
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
	SetAuditTarget(c, in.CN)
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
	SetAuditTarget(c, c.Param("id"))
	c.JSON(202, gin.H{"status": "pending"})
}

// Revoke 吊销证书：先尽力向 CA 真吊销（用账户私钥），再删除 CMDB 记录。
// CA 吊销失败（无账户私钥/网络等）不阻断删除，但响应里明确告警——避免"以为作废实际仍有效"。
func (h *CertHandler) Revoke(c *gin.Context) {
	id := c.Param("id")
	// 先取证书内容 + CA + 账户私钥，尝试真吊销
	var certPEM, ca string
	_ = h.DB.QueryRow(`SELECT COALESCE(cert_pem,''), ca FROM certificates WHERE ci_id=?`, id).Scan(&certPEM, &ca)
	revokeWarning := ""
	if certPEM != "" {
		var acctKeyEnc string
		_ = h.DB.QueryRow(`SELECT COALESCE(account_key_enc,'') FROM acme_accounts WHERE ca=? ORDER BY id LIMIT 1`, ca).Scan(&acctKeyEnc)
		acctKey := ""
		if acctKeyEnc != "" {
			var e error
			if acctKey, e = h.Cipher.Decrypt(acctKeyEnc); e != nil {
				acctKey = ""
			}
		}
		if err := acme.Revoke(acctKey, acme.CADir(ca, false), certPEM); err != nil {
			revokeWarning = err.Error()
			logx.J("cert", "revoke_ca_fail", map[string]any{"ci_id": id, "ca": ca, "error": err.Error()})
		} else {
			logx.J("cert", "revoke_ca_ok", map[string]any{"ci_id": id, "ca": ca})
		}
	}
	// 删除 CMDB 记录（无论 CA 吊销成败，记录都删；成败在响应里如实告知）
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
	SetAuditTarget(c, id)
	out := gin.H{"ok": true, "ca_revoked": revokeWarning == ""}
	if revokeWarning != "" {
		out["warning"] = "CMDB 记录已删除，但向 CA 吊销失败（证书在 CA 侧仍有效至到期）：" + revokeWarning
	}
	c.JSON(200, out)
}

// Download 登录态下载证书：打包成 zip（fullchain.pem + chain.pem + privkey.pem），用户自行解压。
func (h *CertHandler) Download(c *gin.Context) {
	id := c.Param("id")
	var cn, cert, chain, keyEnc string
	err := h.DB.QueryRow(`SELECT cn, COALESCE(cert_pem,''), COALESCE(chain_pem,''), COALESCE(key_pem_enc,'') FROM certificates WHERE ci_id=?`, id).
		Scan(&cn, &cert, &chain, &keyEnc)
	if err != nil {
		c.JSON(404, gin.H{"error": "not found"})
		return
	}
	key := ""
	if keyEnc != "" {
		var derr error
		if key, derr = h.Cipher.Decrypt(keyEnc); derr != nil {
			logx.J("cert", "key_decrypt_fail", map[string]any{"ci_id": id, "op": "download", "error": derr.Error()})
			c.JSON(500, gin.H{"error": "私钥解密失败（主密钥可能已变更），请联系管理员，切勿下发空私钥"})
			return
		}
	}

	// 文件名前缀 = CN 去掉通配 *.，如 *.k8s-g32-uat.com -> k8s-g32-uat.com
	base := strings.NewReplacer("/", "_", " ", "_").Replace(strings.TrimPrefix(cn, "*."))

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range []struct{ name, body string }{
		{base + ".crt", cert}, // fullchain：证书 + 中间 CA 链
		{base + ".key", key},  // 私钥
		{"chain.pem", chain},  // 中间 CA 链（单独文件，沿用原名）
	} {
		if f.body == "" {
			continue
		}
		w, werr := zw.Create(f.name)
		if werr != nil {
			c.JSON(500, gin.H{"error": werr.Error()})
			return
		}
		_, _ = w.Write([]byte(f.body))
	}
	if err := zw.Close(); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	SetAuditTarget(c, id)
	c.Header("Content-Disposition", `attachment; filename="`+base+`.zip"`)
	c.Data(http.StatusOK, "application/zip", buf.Bytes())
}

func (h *CertHandler) runIssue(certCIID, domainCIID int64, acctID int, staging bool, action string) {
	// panic recover：后台签发是 fire-and-forget，panic 会崩整个进程；捕获后置证书为 error 状态。
	defer func() {
		if r := recover(); r != nil {
			logx.J("cert", "issue_panic", map[string]any{"ci_id": certCIID, "action": action, "panic": fmt.Sprint(r)})
			_, _ = h.DB.Exec(`UPDATE certificates SET status='error', last_error=? WHERE ci_id=?`, truncate(fmt.Sprintf("签发内部错误(panic): %v", r), 500), certCIID)
		}
	}()
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
		logx.J("cert", "issue_fail", map[string]any{"ci_id": certCIID, "cn": cn, "action": action, "challenge": challenge, "error": err.Error()})
		_, _ = db.Exec(`UPDATE certificates SET status='error', last_error=? WHERE ci_id=?`, truncate(err.Error(), 500), certCIID)
		_, _ = db.Exec(`INSERT INTO cert_history (cert_ci_id, action, result, detail) VALUES (?, ?, 'fail', ?)`, certCIID, action, truncate(err.Error(), 500))
		return err.Error()
	}
	keyEnc, encErr := cipher.Encrypt(res.KeyPEM)
	if encErr != nil {
		// 私钥加密失败：CA 已签发但无法安全入库，明确报错置 error，别静默成功丢私钥
		msg := "签发成功但私钥加密失败: " + encErr.Error()
		logx.J("cert", "issue_key_encrypt_fail", map[string]any{"ci_id": certCIID, "cn": cn, "error": encErr.Error()})
		_, _ = db.Exec(`UPDATE certificates SET status='error', last_error=? WHERE ci_id=?`, truncate(msg, 500), certCIID)
		_, _ = db.Exec(`INSERT INTO cert_history (cert_ci_id, action, result, detail) VALUES (?, ?, 'fail', ?)`, certCIID, action, truncate(msg, 500))
		return msg
	}
	// 成功入库判错：CA 已签发、私钥仅在内存，这条 UPDATE 失败会静默丢私钥——必须捕获
	if _, e := db.Exec(`UPDATE certificates SET status='active', cert_pem=?, chain_pem=?, key_pem_enc=?, issued_at=NOW(), expiry_at=?, version=version+1, last_error='' WHERE ci_id=?`,
		res.CertPEM, res.ChainPEM, keyEnc, res.NotAfter, certCIID); e != nil {
		msg := "签发成功但入库失败(私钥可能丢失，需重签): " + e.Error()
		logx.J("cert", "issue_save_fail", map[string]any{"ci_id": certCIID, "cn": cn, "error": e.Error()})
		_, _ = db.Exec(`UPDATE certificates SET status='error', last_error=? WHERE ci_id=?`, truncate(msg, 500), certCIID)
		_, _ = db.Exec(`INSERT INTO cert_history (cert_ci_id, action, result, detail) VALUES (?, ?, 'fail', ?)`, certCIID, action, truncate(msg, 500))
		return msg
	}
	if acctKeyEnc == "" && res.AccountKeyPEM != "" {
		if enc, e := cipher.Encrypt(res.AccountKeyPEM); e == nil {
			if _, e2 := db.Exec(`UPDATE acme_accounts SET account_key_enc=? WHERE id=?`, enc, acctID); e2 != nil {
				logx.J("cert", "acct_key_save_fail", map[string]any{"acct_id": acctID, "error": e2.Error()})
			}
		}
	}
	if _, e := db.Exec(`INSERT INTO cert_history (cert_ci_id, action, result, detail) VALUES (?, ?, 'success', ?)`, certCIID, action, "到期 "+res.NotAfter.Format("2006-01-02")); e != nil {
		logx.J("cert", "history_save_fail", map[string]any{"ci_id": certCIID, "error": e.Error()})
	}
	logx.J("cert", "issue_success", map[string]any{"ci_id": certCIID, "cn": cn, "action": action, "not_after": res.NotAfter.Format("2006-01-02")})
	return ""
}

func randToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// truncate 截断到最多 n 字节，但回退到合法 UTF-8 边界，避免切在多字节字符(如中文)中间产生非法串
// （非法 UTF-8 会让 MySQL(utf8mb4) 的 UPDATE 报 Incorrect string value 失败）。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	t := s[:n]
	for len(t) > 0 && !utf8.ValidString(t) {
		t = t[:len(t)-1]
	}
	return t
}
