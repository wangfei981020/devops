package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"domain-platform/database"
	"domain-platform/models"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// timeoutContext creates a context with timeout
func timeoutContext(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

func makeHTTPClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}

// -----------------------------------------------------------------------
// GoDaddy API
// -----------------------------------------------------------------------

type godaddyDomain struct {
	Domain  string `json:"domain"`
	Expires string `json:"expires"`
}

type godaddyDNSRecord struct {
	Type     string `json:"type"`
	Name     string `json:"name"`
	Data     string `json:"data"`
	TTL      int    `json:"ttl"`
	Priority int    `json:"priority"`
}

func godaddyRequest(config *models.RegistrarConfig, method, path string) ([]byte, error) {
	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = "https://api.godaddy.com"
	}
	url := baseURL + path
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf("sso-key %s:%s", config.APIKey, config.APISecret))
	req.Header.Set("Accept", "application/json")

	client := makeHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GoDaddy API error %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

func godaddyListDomainsPage(config *models.RegistrarConfig, limit, offset int) ([]godaddyDomain, error) {
	path := fmt.Sprintf("/v1/domains?limit=%d&offset=%d&statuses=ACTIVE", limit, offset)
	body, err := godaddyRequest(config, "GET", path)
	if err != nil {
		return nil, err
	}
	var domains []godaddyDomain
	if err := json.Unmarshal(body, &domains); err != nil {
		return nil, err
	}
	return domains, nil
}

func godaddyListAllDomains(config *models.RegistrarConfig) ([]godaddyDomain, error) {
	var all []godaddyDomain
	offset := 0
	limit := 100
	for {
		page, err := godaddyListDomainsPage(config, limit, offset)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if len(page) < limit {
			break
		}
		offset += limit
		if offset > 0 {
			time.Sleep(1 * time.Second)
		}
	}
	return all, nil
}

func godaddyListDomains(config *models.RegistrarConfig, limit, offset int) ([]string, error) {
	domains, err := godaddyListDomainsPage(config, limit, offset)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, d := range domains {
		names = append(names, d.Domain)
	}
	return names, nil
}

func godaddyGetDNSRecords(config *models.RegistrarConfig, domain string) ([]godaddyDNSRecord, error) {
	path := fmt.Sprintf("/v1/domains/%s/records", domain)
	body, err := godaddyRequest(config, "GET", path)
	if err != nil {
		return nil, err
	}
	var records []godaddyDNSRecord
	if err := json.Unmarshal(body, &records); err != nil {
		return nil, err
	}
	return records, nil
}

// -----------------------------------------------------------------------
// Name.com API
// -----------------------------------------------------------------------

type namecomDomain struct {
	DomainName string `json:"domainName"`
	ExpireDate string `json:"expireDate"`
}

type namecomListResponse struct {
	Domains  []namecomDomain `json:"domains"`
	NextPage int             `json:"nextPage"`
}

type namecomDNSRecord struct {
	Type     string `json:"type"`
	Host     string `json:"host"`
	Answer   string `json:"answer"`
	TTL      int    `json:"ttl"`
	Priority int    `json:"priority"`
}

type namecomDNSResponse struct {
	Records []namecomDNSRecord `json:"records"`
}

func namecomRequest(config *models.RegistrarConfig, method, path string) ([]byte, error) {
	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = "https://api.name.com"
	}
	url := baseURL + path
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(config.APIUser, config.APIToken)
	req.Header.Set("Accept", "application/json")

	client := makeHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Name.com API error %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

func namecomListAllDomains(config *models.RegistrarConfig) ([]namecomDomain, error) {
	var all []namecomDomain
	page := 1
	for {
		path := fmt.Sprintf("/v4/domains?page=%d&perPage=100", page)
		body, err := namecomRequest(config, "GET", path)
		if err != nil {
			return nil, err
		}
		var resp namecomListResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, err
		}
		all = append(all, resp.Domains...)
		if resp.NextPage == 0 {
			break
		}
		page = resp.NextPage
	}
	return all, nil
}

func namecomListDomains(config *models.RegistrarConfig, limit int) ([]string, error) {
	path := fmt.Sprintf("/v4/domains?perPage=%d", limit)
	body, err := namecomRequest(config, "GET", path)
	if err != nil {
		return nil, err
	}
	var resp namecomListResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	var names []string
	for _, d := range resp.Domains {
		names = append(names, d.DomainName)
	}
	return names, nil
}

func namecomGetDNSRecords(config *models.RegistrarConfig, domain string) ([]namecomDNSRecord, error) {
	path := fmt.Sprintf("/v4/domains/%s/records", domain)
	body, err := namecomRequest(config, "GET", path)
	if err != nil {
		return nil, err
	}
	var resp namecomDNSResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return resp.Records, nil
}

// -----------------------------------------------------------------------
// Settings helpers
// -----------------------------------------------------------------------

func getGlobalExcludeHosts() []string {
	var val string
	row := database.DB.QueryRow("SELECT COALESCE(setting_value,'') FROM global_settings WHERE setting_key='filter_exclude_hosts'")
	row.Scan(&val)
	if val == "" {
		return nil
	}
	var hosts []string
	json.Unmarshal([]byte(val), &hosts)
	return hosts
}

func getDomainOverride(rootDomain string) *models.DomainOverride {
	row := database.DB.QueryRow(
		"SELECT id, root_domain, COALESCE(exclude_hosts,'') FROM domain_overrides WHERE root_domain=?",
		rootDomain)
	var o models.DomainOverride
	if err := row.Scan(&o.ID, &o.RootDomain, &o.ExcludeHosts); err != nil {
		return nil
	}
	return &o
}

func matchesExclude(host string, excludes []string) bool {
	for _, ex := range excludes {
		if strings.EqualFold(host, ex) {
			return true
		}
	}
	return false
}

// -----------------------------------------------------------------------
// Sync handler
// -----------------------------------------------------------------------

func HandleSyncConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	vars := mux.Vars(r)
	id := vars["id"]

	config, err := getConfigByID(id)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "配置不存在"})
		return
	}

	added, updated, syncErr := syncConfig(config)
	if syncErr != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "同步失败: " + syncErr.Error(),
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"added":   added,
		"updated": updated,
		"message": fmt.Sprintf("同步完成: 新增 %d 条, 更新 %d 条", added, updated),
	})
}

func syncConfig(config *models.RegistrarConfig) (int, int, error) {
	globalExcludes := getGlobalExcludeHosts()

	type domainEntry struct {
		name    string
		expires string
	}

	var domainList []domainEntry

	switch config.Provider {
	case "godaddy":
		domains, err := godaddyListAllDomains(config)
		if err != nil {
			return 0, 0, err
		}
		for _, d := range domains {
			domainList = append(domainList, domainEntry{d.Domain, d.Expires})
		}
	case "namecom":
		domains, err := namecomListAllDomains(config)
		if err != nil {
			return 0, 0, err
		}
		for _, d := range domains {
			domainList = append(domainList, domainEntry{d.DomainName, d.ExpireDate})
		}
	default:
		return 0, 0, fmt.Errorf("未知注册商类型: %s", config.Provider)
	}

	added := 0
	updated := 0
	now := time.Now().Format("2006-01-02 15:04:05")

	for i, entry := range domainList {
		rootDomain := entry.name
		expireDate := parseExpireDate(entry.expires)

		override := getDomainOverride(rootDomain)
		var domainExcludes []string
		if override != nil && override.ExcludeHosts != "" {
			json.Unmarshal([]byte(override.ExcludeHosts), &domainExcludes)
		}

		var records []struct {
			rType string
			host  string
			data  string
		}

		switch config.Provider {
		case "godaddy":
			if i > 0 {
				time.Sleep(1200 * time.Millisecond)
			}
			dnsRecords, err := godaddyGetDNSRecords(config, rootDomain)
			if err != nil {
				log.Printf("Failed to get DNS records for %s: %v", rootDomain, err)
				continue
			}
			for _, rec := range dnsRecords {
				records = append(records, struct {
					rType string
					host  string
					data  string
				}{rec.Type, rec.Name, rec.Data})
			}
		case "namecom":
			dnsRecords, err := namecomGetDNSRecords(config, rootDomain)
			if err != nil {
				log.Printf("Failed to get DNS records for %s: %v", rootDomain, err)
				continue
			}
			for _, rec := range dnsRecords {
				records = append(records, struct {
					rType string
					host  string
					data  string
				}{rec.Type, rec.Host, rec.Answer})
			}
		}

		for _, rec := range records {
			rType := strings.ToUpper(rec.rType)
			if rType != "A" && rType != "AAAA" && rType != "CNAME" {
				continue
			}

			host := rec.host
			if host == "*" {
				continue
			}
			if matchesExclude(host, globalExcludes) || matchesExclude(host, domainExcludes) {
				continue
			}

			var fullDomain string
			if host == "@" || host == "" {
				fullDomain = rootDomain
			} else {
				fullDomain = host + "." + rootDomain
			}

			// Check existing
			var existingID string
			row := database.DB.QueryRow(
				"SELECT id FROM registrar_domains WHERE full_domain=? AND config_id=?",
				fullDomain, config.ID)
			row.Scan(&existingID)

			if existingID != "" {
				// Update
				_, err := database.DB.Exec(
					`UPDATE registrar_domains SET record_type=?, record_value=?, expire_time=?, updated_at=? WHERE id=?`,
					rType, rec.data, expireDate, now, existingID,
				)
				if err == nil {
					updated++
				}
			} else {
				// Insert
				newID := uuid.New().String()
				_, err := database.DB.Exec(
					`INSERT INTO registrar_domains (id, config_id, provider, root_domain, host, full_domain, record_type, record_value, expire_time, status, created_at, updated_at)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'active', ?, ?)`,
					newID, config.ID, config.Provider, rootDomain, host, fullDomain, rType, rec.data, expireDate, now, now,
				)
				if err == nil {
					added++
				} else {
					log.Printf("Insert error for %s: %v", fullDomain, err)
				}
			}
		}
	}

	// Update last_sync
	database.DB.Exec("UPDATE registrar_configs SET last_sync=NOW() WHERE id=?", config.ID)

	return added, updated, nil
}

func parseExpireDate(s string) interface{} {
	if s == "" {
		return nil
	}
	// Try common formats
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05",
		"2006-01-02",
		"01/02/2006",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return nil
}

// -----------------------------------------------------------------------
// Auto sync
// -----------------------------------------------------------------------

func getSetting(key string) string {
	var val string
	database.DB.QueryRow("SELECT COALESCE(setting_value,'') FROM global_settings WHERE setting_key=?", key).Scan(&val)
	return val
}

func StartAutoSync() {
	go func() {
		for {
			time.Sleep(60 * time.Second)

			enabled := getSetting("auto_sync_enabled")
			if enabled != "true" {
				continue
			}

			syncTime := getSetting("auto_sync_time")
			if syncTime == "" {
				syncTime = "02:00"
			}

			now := time.Now()
			currentHHMM := now.Format("15:04")
			if currentHHMM != syncTime {
				continue
			}

			log.Println("[AutoSync] Starting auto sync for all active configs...")

			rows, err := database.DB.Query("SELECT id FROM registrar_configs WHERE status='active'")
			if err != nil {
				log.Printf("[AutoSync] Failed to query configs: %v", err)
				continue
			}

			var ids []string
			for rows.Next() {
				var id string
				rows.Scan(&id)
				ids = append(ids, id)
			}
			rows.Close()

			for _, id := range ids {
				config, err := getConfigByID(id)
				if err != nil {
					continue
				}
				added, updated, err := syncConfig(config)
				if err != nil {
					log.Printf("[AutoSync] Sync failed for %s: %v", id, err)
				} else {
					log.Printf("[AutoSync] Sync done for %s: added=%d, updated=%d", config.Name, added, updated)
				}
			}
		}
	}()
	log.Println("Auto sync goroutine started")
}
