package model

// === 证书相关 ===

// CreateCertInput 创建证书输入
type CreateCertInput struct {
	Name           string `json:"name"`
	CertPEM        []byte `json:"cert_pem"`
	KeyPEM         []byte `json:"key_pem"`
	ChainPEM       []byte `json:"chain_pem,omitempty"`
	AutoRenew      bool   `json:"auto_renew"`
	ThirdpartDNSID string `json:"thirdpart_dns_id,omitempty"`
	Source         string `json:"source,omitempty"` // If empty, defaults to "upload"
}

// UpdateCertInput 更新证书输入
type UpdateCertInput struct {
	Name      *string `json:"name,omitempty"`
	CertPEM   []byte  `json:"cert_pem,omitempty"`
	KeyPEM    []byte  `json:"key_pem,omitempty"`
	ChainPEM  []byte  `json:"chain_pem,omitempty"`
	AutoRenew *bool   `json:"auto_renew,omitempty"`
}

// CertFilter 证书过滤条件
type CertFilter struct {
	Source       string `json:"source,omitempty"`
	AutoRenew    *bool  `json:"auto_renew,omitempty"`
	ExpiringSoon bool   `json:"expiring_soon,omitempty"`
}

// CertbotIssueInput Certbot 签发证书输入
type CertbotIssueInput struct {
	Domains        []string `json:"domains"`
	ThirdpartDNSID string   `json:"thirdpart_dns_id"`
	AutoRenew      bool     `json:"auto_renew"`
	Name           string   `json:"name"`
}

// CertContent 证书内容（用于 Agent 下载）
type CertContent struct {
	FullchainPEM      []byte `json:"fullchain_pem"`
	PrivateKeyPEM     []byte `json:"private_key_pem"`
	FingerprintSHA256 string `json:"fingerprint_sha256"`
}

// === 机器相关 ===

// CreateMachineInput 创建机器输入
type CreateMachineInput struct {
	Name   string   `json:"name"`
	IP     string   `json:"ip"`
	Tags   []string `json:"tags,omitempty"`
	Remark string   `json:"remark,omitempty"`
}

// UpdateMachineInput 更新机器输入
type UpdateMachineInput struct {
	Name   *string  `json:"name,omitempty"`
	IP     *string  `json:"ip,omitempty"`
	Tags   []string `json:"tags,omitempty"`
	Remark *string  `json:"remark,omitempty"`
}

// MachineFilter 机器过滤条件
type MachineFilter struct {
	Status string `json:"status,omitempty"`
	Search string `json:"search,omitempty"`
}

// HeartbeatInfo Agent 心跳信息
type HeartbeatInfo struct {
	MachineID    string `json:"machine_id"`
	AgentVersion string `json:"agent_version"`
	Hostname     string `json:"hostname"`
	IP           string `json:"ip"`
	OS           string `json:"os"`
	Arch         string `json:"arch"`
}

// === 机器证书部署配置相关 ===

// CreateMachineCertInput 创建机器证书部署配置输入
type CreateMachineCertInput struct {
	MachineID          string `json:"machine_id"`
	CertificateID      string `json:"certificate_id"`
	CertPath           string `json:"cert_path"`
	PrivateKeyPath     string `json:"private_key_path"`
	PostDeployCommands string `json:"post_deploy_commands,omitempty"`
}

// UpdateMachineCertInput 更新机器证书部署配置输入
type UpdateMachineCertInput struct {
	CertPath           *string `json:"cert_path,omitempty"`
	PrivateKeyPath     *string `json:"private_key_path,omitempty"`
	PostDeployCommands *string `json:"post_deploy_commands,omitempty"`
}

// === 域名监控相关 ===

// CreateDomainInput 创建域名监控输入
type CreateDomainInput struct {
	Name                       string `json:"name"`
	MonitorPort                int    `json:"monitor_port,omitempty"`
	LinkedMachineID            string `json:"linked_machine_id,omitempty"`
	LinkedCertificateID        string `json:"linked_certificate_id,omitempty"`
	LinkedMachineCertificateID string `json:"linked_machine_certificate_id,omitempty"`
}

// UpdateDomainInput 更新域名监控输入
type UpdateDomainInput struct {
	MonitorPort                *int    `json:"monitor_port,omitempty"`
	LinkedMachineID            *string `json:"linked_machine_id,omitempty"`
	LinkedCertificateID        *string `json:"linked_certificate_id,omitempty"`
	LinkedMachineCertificateID *string `json:"linked_machine_certificate_id,omitempty"`
	MonitorEnabled             *bool   `json:"monitor_enabled,omitempty"`
	AlertIgnored               *bool   `json:"alert_ignored,omitempty"`
}

// DomainFilter 域名过滤条件
type DomainFilter struct {
	Name           string `json:"name,omitempty"`
	Source         string `json:"source,omitempty"`
	MonitorEnabled *bool  `json:"monitor_enabled,omitempty"`
	ThirdpartDNSID string `json:"thirdpart_dns_id,omitempty"`
}

// DomainListParams 域名列表查询参数（含筛选、排序、分页）
type DomainListParams struct {
	// Filtering
	Name           string `json:"name,omitempty"`
	Source         string `json:"source,omitempty"`
	ThirdpartDNSID string `json:"thirdpart_dns_id,omitempty"`
	MonitorEnabled *bool  `json:"monitor_enabled,omitempty"`
	AlertIgnored   *bool  `json:"alert_ignored,omitempty"`
	FilterStatus   string `json:"filter_status,omitempty"`

	// Sorting
	SortBy    string `json:"sort_by,omitempty"`
	SortOrder string `json:"sort_order,omitempty"`

	// Pagination
	Page    int `json:"page,omitempty"`
	PerPage int `json:"per_page,omitempty"`
}

// === 第三方 DNS 上游相关 ===

// CreateThirdpartDNSInput 创建第三方 DNS 上游配置输入
type CreateThirdpartDNSInput struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	APIToken    string   `json:"api_token"`
	ConfigJSON  string   `json:"config_json"`
	MainDomains []string `json:"main_domains"`
	Enabled     *bool    `json:"enabled,omitempty"`
}

// UpdateThirdpartDNSInput 更新第三方 DNS 上游配置输入
type UpdateThirdpartDNSInput struct {
	Name        *string  `json:"name,omitempty"`
	APIToken    *string  `json:"api_token,omitempty"`
	ConfigJSON  *string  `json:"config_json,omitempty"`
	MainDomains []string `json:"main_domains,omitempty"`
	Enabled     *bool    `json:"enabled,omitempty"`
}

// === 告警相关 ===

// AlertFilter 告警过滤条件
type AlertFilter struct {
	Level   string `json:"level,omitempty"`
	Type    string `json:"type,omitempty"`
	Status  string `json:"status,omitempty"`
	Page    int    `json:"page,omitempty"`
	PerPage int    `json:"per_page,omitempty"`
}

// === 根域名到期监控相关 ===

// CreateRootDomainInput 手动添加根域名输入
type CreateRootDomainInput struct {
	Name string `json:"name"`
}

// UpdateRootDomainInput 更新根域名（仅监控开关与忽略告警）
type UpdateRootDomainInput struct {
	MonitorEnabled *bool `json:"monitor_enabled,omitempty"`
	AlertIgnored   *bool `json:"alert_ignored,omitempty"`
}

// ImportRootDomainsInput 从 Cloudflare 导入根域名输入（api_token 或 config_id 二选一）
type ImportRootDomainsInput struct {
	APIToken string `json:"api_token,omitempty"`
	ConfigID string `json:"config_id,omitempty"`
}

// RootDomainImportResult 导入结果
type RootDomainImportResult struct {
	Imported []string `json:"imported"` // 新登记的 registrable domain
	Skipped  []string `json:"skipped"`  // 已存在被跳过的 registrable domain
	Total    int      `json:"total"`    // 扫描到的 zone 数
}

// NewRootDomainImportResult 创建 RootDomainImportResult，并将切片初始化为空数组，
// 确保 JSON 输出 [] 而非 null（沿用 NewDNSSyncResult 的做法）。
func NewRootDomainImportResult() *RootDomainImportResult {
	return &RootDomainImportResult{
		Imported: []string{},
		Skipped:  []string{},
		Total:    0,
	}
}

// RootDomainFilter 简单过滤条件（供 reconcile / 内部列举使用）
type RootDomainFilter struct {
	Source         string `json:"source,omitempty"`
	MonitorEnabled *bool  `json:"monitor_enabled,omitempty"`
}

// RootDomainListParams 列表查询参数（筛选/排序/分页）
type RootDomainListParams struct {
	// Filtering
	Name           string `json:"name,omitempty"`
	Source         string `json:"source,omitempty"`
	FilterStatus   string `json:"filter_status,omitempty"` // expiring|expired|unknown|ok|enabled|disabled|ignored
	MonitorEnabled *bool  `json:"monitor_enabled,omitempty"`
	AlertIgnored   *bool  `json:"alert_ignored,omitempty"`

	// Sorting
	SortBy    string `json:"sort_by,omitempty"`    // name|source|expiry_date|last_checked_at|created_at
	SortOrder string `json:"sort_order,omitempty"` // asc|desc

	// Pagination
	Page    int `json:"page,omitempty"`
	PerPage int `json:"per_page,omitempty"`
}
