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
}

// DomainFilter 域名过滤条件
type DomainFilter struct {
	Name           string `json:"name,omitempty"`
	Source         string `json:"source,omitempty"`
	MonitorEnabled *bool  `json:"monitor_enabled,omitempty"`
	ThirdpartDNSID string `json:"thirdpart_dns_id,omitempty"`
}

// === 第三方 DNS 上游相关 ===

// CreateThirdpartDNSInput 创建第三方 DNS 上游配置输入
type CreateThirdpartDNSInput struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	APIToken    string   `json:"api_token"`
	ConfigJSON  string   `json:"config_json"`
	MainDomains []string `json:"main_domains"`
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
	Level  string `json:"level,omitempty"`
	Type   string `json:"type,omitempty"`
	Status string `json:"status,omitempty"`
}
