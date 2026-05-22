package model

import "time"

// Certificate 证书元数据（PEM 内容以文件形式存储在 ./data/certificates/<id>/）
type Certificate struct {
	ID                string     `json:"id"`
	Name              string     `json:"name"`
	Domains           []string   `json:"domains"`
	Source            string     `json:"source"`
	ExpireAt          time.Time  `json:"expire_at"`
	AutoRenew         bool       `json:"auto_renew"`
	Issuer            string     `json:"issuer"`
	FingerprintSHA256 string     `json:"fingerprint_sha256"`
	ChainValid        bool       `json:"chain_valid"`
	CertDirPath       string     `json:"-"` // 文件存储路径，不暴露给 API
	ThirdpartDNSID    string     `json:"thirdpart_dns_id,omitempty"`
	LastRenewAt       *time.Time `json:"last_renew_at"`
	RenewStatus       string     `json:"renew_status"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// CertificateResponse 用于普通 Web API 响应，不包含私钥和文件路径
type CertificateResponse struct {
	ID                string     `json:"id"`
	Name              string     `json:"name"`
	Domains           []string   `json:"domains"`
	Source            string     `json:"source"`
	ExpireAt          time.Time  `json:"expire_at"`
	AutoRenew         bool       `json:"auto_renew"`
	Issuer            string     `json:"issuer"`
	FingerprintSHA256 string     `json:"fingerprint_sha256"`
	ChainValid        bool       `json:"chain_valid"`
	HasPrivateKey     bool       `json:"has_private_key"`
	MachineCount      int        `json:"machine_count"`
	LastRenewAt       *time.Time `json:"last_renew_at"`
	RenewStatus       string     `json:"renew_status"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// AgentCertDownloadResponse 仅用于 Agent 下载接口，从文件系统读取 PEM 内容
type AgentCertDownloadResponse struct {
	CertificateID     string `json:"certificate_id"`
	FingerprintSHA256 string `json:"fingerprint_sha256"`
	FullchainPEM      string `json:"fullchain_pem"`
	PrivateKeyPEM     string `json:"private_key_pem"`
}

// CertMetadata 证书解析后的元数据
type CertMetadata struct {
	Domains           []string  `json:"domains"`
	ExpireAt          time.Time `json:"expire_at"`
	Issuer            string    `json:"issuer"`
	FingerprintSHA256 string    `json:"fingerprint_sha256"`
	ChainValid        bool      `json:"chain_valid"`
}
