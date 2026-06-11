package model

import "time"

// Domain 域名监控对象
type Domain struct {
	ID                         string    `json:"id"`
	Name                       string    `json:"name"`
	Source                     string    `json:"source"`
	ThirdpartDNSID             string    `json:"thirdpart_dns_id,omitempty"`
	DNSRecordType              string    `json:"dns_record_type"`
	DNSRecordValue             string    `json:"dns_record_value"`
	MonitorPort                int       `json:"monitor_port"`
	LinkedMachineID            string    `json:"linked_machine_id,omitempty"`
	LinkedCertificateID        string    `json:"linked_certificate_id,omitempty"`
	LinkedMachineCertificateID string    `json:"linked_machine_certificate_id,omitempty"`
	MonitorEnabled             bool      `json:"monitor_enabled"`
	AlertIgnored               bool      `json:"alert_ignored"`
	DNSRecordID                string    `json:"dns_record_id"`
	CreatedAt                  time.Time `json:"created_at"`
	UpdatedAt                  time.Time `json:"updated_at"`
}

// DomainMonitorResult 域名监控探测结果
type DomainMonitorResult struct {
	ID                           string     `json:"id"`
	DomainID                     string     `json:"domain_id"`
	CheckedPort                  int        `json:"checked_port"`
	ResolvedIPs                  []string   `json:"resolved_ips"`
	TLSSuccess                   bool       `json:"tls_success"`
	CertificateFingerprintSHA256 string     `json:"certificate_fingerprint_sha256"`
	Issuer                       string     `json:"issuer"`
	ExpireAt                     *time.Time `json:"expire_at"`
	DaysRemaining                *int       `json:"days_remaining"`
	DomainMatched                bool       `json:"domain_matched"`
	ChainValid                   bool       `json:"chain_valid"`
	ErrorMessage                 string     `json:"error_message"`
	CheckedAt                    time.Time  `json:"checked_at"`
}
