package model

import "time"

// MachineCertificate 机器证书部署配置
type MachineCertificate struct {
	ID                 string     `json:"id"`
	MachineID          string     `json:"machine_id"`
	CertificateID      string     `json:"certificate_id"`
	CertPath           string     `json:"cert_path"`
	PrivateKeyPath     string     `json:"private_key_path"`
	PostDeployCommands string     `json:"post_deploy_commands"`
	ConfigRevision     int        `json:"config_revision"`
	LastDeployStatus   string     `json:"last_deploy_status"`
	LastDeployAt       *time.Time `json:"last_deploy_at"`
	LastDeployMessage  string     `json:"last_deploy_message"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}
