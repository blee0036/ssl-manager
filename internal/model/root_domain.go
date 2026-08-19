package model

import "time"

// RootDomain 根域名注册到期监控对象。
// 与既有 Domain（TLS 证书监控）相互独立：本模型监控的是 WHOIS 注册到期日，
// 而非 TLS 证书到期。持久化于独立的 root_domains 表。
type RootDomain struct {
	ID                string     `json:"id"`
	Name              string     `json:"name"`               // 原始来源名（cloudflare=Zone.Name / manual=规范化输入）
	Source            string     `json:"source"`             // "manual" | "cloudflare"
	RegistrableDomain string     `json:"registrable_domain"` // eTLD+1，WHOIS 目标与去重键
	ExpiryDate        *time.Time `json:"expiry_date"`        // 注册到期日（UTC）；null = 未知
	ExpirySource      string     `json:"expiry_source"`      // "whois"（默认，自动查询） | "manual"（人工手动设置，跳过周期刷新）
	DaysRemaining     *int       `json:"days_remaining"`     // 读取时计算（非持久化）；null = 未知
	LastCheckedAt     *time.Time `json:"last_checked_at"`    // 最近一次检查时间；null = 从未检查
	LastStatus        string     `json:"last_status"`        // "success" | "failed" | "manual" | ""（未检查）
	LastError         string     `json:"last_error"`         // 最近失败原因
	MonitorEnabled    bool       `json:"monitor_enabled"`
	AlertIgnored      bool       `json:"alert_ignored"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}
