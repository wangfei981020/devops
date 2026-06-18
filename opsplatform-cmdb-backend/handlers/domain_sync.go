package handlers

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/crypto"
	"opsplatform-cmdb-backend/dnsource"
)

// SyncHandler 域名数据源同步（厂商 → DB）+ DNS 记录缓存读取 + API 用量。
type SyncHandler struct {
	DB     *sql.DB
	Cipher *crypto.Cipher
}

func NewSyncHandler(db *sql.DB, cipher *crypto.Cipher) *SyncHandler {
	return &SyncHandler{DB: db, Cipher: cipher}
}

func (h *SyncHandler) Register(r *gin.RouterGroup) {
	r.POST("/sources/:id/sync", h.Sync)
	r.GET("/sources/:id/usage", h.Usage)
	r.GET("/domains/:ciid/dns-records", h.DNSRecords)
}

// Usage 某数据源的 API 客户端限流用量。
func (h *SyncHandler) Usage(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	c.JSON(http.StatusOK, dnsource.LimiterFor(id).Stats())
}

// Sync 同步某数据源：拉域名 + 每个域名的 DNS 记录，写入/更新 DB（受客户端限流）。
func (h *SyncHandler) Sync(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	provider, cred, err := LoadCredential(h.DB, h.Cipher, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "数据源不存在或凭据读取失败"})
		return
	}
	adapter, err := dnsource.NewAdapter(provider, cred, dnsource.LimiterFor(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	domains, err := adapter.ListDomains(ctx)
	if rle := asRateLimit(err); rle != nil {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": rle.Error(), "rate_limit": rle.Info})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	syncedDomains, syncedRecords := 0, 0
	for _, d := range domains {
		ciID, err := h.upsertDomainCI(d.Name, id, d.ExpiresAt)
		if err != nil {
			continue
		}
		syncedDomains++

		recs, err := adapter.ListRecords(ctx, d.Name)
		if rle := asRateLimit(err); rle != nil {
			// 撞客户端限流：停在这，返回已同步部分 + 限流信息
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": rle.Error(), "rate_limit": rle.Info,
				"synced_domains": syncedDomains, "synced_records": syncedRecords,
				"partial": true,
			})
			return
		}
		if err != nil {
			continue // 单域名 DNS 拉取失败跳过
		}
		h.refreshDNSRecords(ciID, id, recs)
		syncedRecords += len(recs)
	}
	WriteAudit(h.DB, c, "sync_source", c.Param("id"))
	c.JSON(http.StatusOK, gin.H{"synced_domains": syncedDomains, "synced_records": syncedRecords})
}

// DNSRecords 读某域名的厂商原始 DNS 记录（来自同步缓存 dns_records）。
func (h *SyncHandler) DNSRecords(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT id, type, name, data, ttl, priority, protected, synced_at
		FROM dns_records WHERE domain_ci_id=? ORDER BY type, name`, c.Param("ciid"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	type rec struct {
		ID        int64  `json:"id"`
		Type      string `json:"type"`
		Name      string `json:"name"`
		Data      string `json:"data"`
		TTL       int    `json:"ttl"`
		Priority  *int   `json:"priority"`
		Protected bool   `json:"protected"`
		SyncedAt  string `json:"synced_at"`
	}
	out := []rec{}
	for rows.Next() {
		var r rec
		var prio sql.NullInt64
		var prot int
		var synced time.Time
		if rows.Scan(&r.ID, &r.Type, &r.Name, &r.Data, &r.TTL, &prio, &prot, &synced) == nil {
			if prio.Valid {
				v := int(prio.Int64)
				r.Priority = &v
			}
			r.Protected = prot == 1
			r.SyncedAt = synced.Format("2006-01-02 15:04")
			out = append(out, r)
		}
	}
	c.JSON(http.StatusOK, out)
}

// ---- helpers ----

func (h *SyncHandler) upsertDomainCI(name string, sourceID int, expires *time.Time) (int64, error) {
	var ciID int64
	err := h.DB.QueryRow(`SELECT id FROM cis WHERE type='domain' AND name=?`, name).Scan(&ciID)
	if err == sql.ErrNoRows {
		res, e := h.DB.Exec(`INSERT INTO cis (type, name, status) VALUES ('domain', ?, 'active')`, name)
		if e != nil {
			return 0, e
		}
		ciID, _ = res.LastInsertId()
	} else if err != nil {
		return 0, err
	}
	var exp any
	if expires != nil {
		exp = *expires
	}
	_, _ = h.DB.Exec(`INSERT INTO domains (ci_id, registrar_id, expiry_at) VALUES (?, ?, ?)
		ON DUPLICATE KEY UPDATE registrar_id=VALUES(registrar_id), expiry_at=VALUES(expiry_at)`, ciID, sourceID, exp)
	return ciID, nil
}

func (h *SyncHandler) refreshDNSRecords(ciID int64, sourceID int, recs []dnsource.DNSRecord) {
	_, _ = h.DB.Exec(`DELETE FROM dns_records WHERE domain_ci_id=?`, ciID)
	for _, r := range recs {
		var prio any
		if r.Priority != nil {
			prio = *r.Priority
		}
		_, _ = h.DB.Exec(`INSERT INTO dns_records (domain_ci_id, type, name, data, ttl, priority, protected, source_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			ciID, r.Type, r.Name, r.Data, r.TTL, prio, boolToInt(isProtectedRecord(r)), sourceID)
	}
}

// isProtectedRecord 标记不可在 CMDB 误改的记录：CF 的 _acme-challenge 委托、NS 等。
func isProtectedRecord(r dnsource.DNSRecord) bool {
	if r.Type == "NS" || r.Type == "SOA" {
		return true
	}
	if strings.HasPrefix(r.Name, "_acme-challenge") {
		return true
	}
	return false
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func asRateLimit(err error) *dnsource.RateLimitError {
	var rle *dnsource.RateLimitError
	if errors.As(err, &rle) {
		return rle
	}
	return nil
}
