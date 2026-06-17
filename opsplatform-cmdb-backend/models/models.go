package models

import "time"

// CIType 配置项类型（domain/certificate，可扩展）
type CIType struct {
	ID        int    `json:"id"`
	Code      string `json:"code"`
	Name      string `json:"name"`
	Icon      string `json:"icon"`
	SortOrder int    `json:"sort_order"`
}

// CI 配置项通用元数据
type CI struct {
	ID        int64             `json:"id"`
	Type      string            `json:"type"`
	Name      string            `json:"name"`
	Project   string            `json:"project"`
	Env       string            `json:"env"`
	Module    string            `json:"module"`
	Owner     string            `json:"owner"`
	Status    string            `json:"status"`
	Remark    string            `json:"remark"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
	Labels    map[string]string `json:"labels,omitempty"`
	Relations []Relation        `json:"relations,omitempty"`
}

// Relation CI 关系（含对端展示信息）
type Relation struct {
	ID        int64  `json:"id"`
	SrcCIID   int64  `json:"src_ci_id"`
	DstCIID   int64  `json:"dst_ci_id"`
	RelType   string `json:"rel_type"`
	PeerID    int64  `json:"peer_id"`
	PeerName  string `json:"peer_name"`
	PeerType  string `json:"peer_type"`
	Direction string `json:"direction"` // out(本CI是src) / in(本CI是dst)
}
