package model

import "time"

// ThirdpartDNS 第三方 DNS 上游配置
type ThirdpartDNS struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Type        string    `json:"type"`
	APIToken    string    `json:"-"`
	ConfigJSON  string    `json:"config_json"`
	MainDomains []string  `json:"main_domains"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ThirdpartDNSSyncLog 第三方 DNS 同步日志
type ThirdpartDNSSyncLog struct {
	ID             string    `json:"id"`
	ThirdpartDNSID string    `json:"thirdpart_dns_id"`
	RecordsCount   int       `json:"records_count"`
	Status         string    `json:"status"`
	ErrorMessage   string    `json:"error_message"`
	SyncedAt       time.Time `json:"synced_at"`
}

// DNSSyncResult DNS 同步结果
type DNSSyncResult struct {
	RecordsCount   int      `json:"records_count"`
	NewDomains     []string `json:"new_domains"`
	UpdatedDomains []string `json:"updated_domains"`
}
