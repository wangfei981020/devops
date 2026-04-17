package models

type RegistrarConfig struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Provider  string `json:"provider"` // godaddy, namecom
	APIKey    string `json:"api_key"`
	APISecret string `json:"api_secret"`
	APIUser   string `json:"api_user"`   // namecom
	APIToken  string `json:"api_token"`  // namecom
	BaseURL   string `json:"base_url"`
	Status    string `json:"status"`
	LastSync  string `json:"last_sync"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type RegistrarDomain struct {
	ID             string `json:"id"`
	ConfigID       string `json:"config_id"`
	Provider       string `json:"provider"`
	Source         string `json:"source"` // manual / godaddy / namecom
	Project        string `json:"project"`
	Env            string `json:"env"`
	RootDomain     string `json:"root_domain"`
	Host           string `json:"host"`
	FullDomain     string `json:"full_domain"`
	RecordType     string `json:"record_type"`
	RecordValue    string `json:"record_value"`
	ExpireTime     string `json:"expire_time"`
	CertExpireTime string `json:"cert_expire_time"`
	Module         string `json:"module"`
	Origin         string `json:"origin"`
	OriginIP       string `json:"origin_ip"`
	CDNProvider    string `json:"cdn_provider"`
	Status         string `json:"status"`
	Remark         string `json:"remark"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

type GlobalSetting struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	UpdatedAt string `json:"updated_at"`
}

type DomainOverride struct {
	ID           string `json:"id"`
	RootDomain   string `json:"root_domain"`
	ExcludeHosts string `json:"exclude_hosts"` // JSON array stored as string
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}
