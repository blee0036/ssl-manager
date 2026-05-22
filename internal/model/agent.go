package model

import "time"

// AgentConfig Agent 配置文件结构
type AgentConfig struct {
	ServerURL           string `yaml:"server_url" json:"server_url"`
	MachineID           string `yaml:"machine_id" json:"machine_id"`
	AgentToken          string `yaml:"agent_token" json:"agent_token"`
	PollIntervalSeconds int    `yaml:"poll_interval_seconds" json:"poll_interval_seconds"`
	LogLevel            string `yaml:"log_level" json:"log_level"`
}

// AgentLocalState Agent 本地状态文件（/etc/ssl-manager-agent/state.json），重启后恢复
type AgentLocalState struct {
	MachineCertStates map[string]*MachineCertState `json:"machine_cert_states"`
}

// MachineCertState 单个机器证书的本地同步状态
type MachineCertState struct {
	MachineCertificateID  string `json:"machine_certificate_id"`
	LastSyncedRevision    int    `json:"last_synced_revision"`
	LastSyncedFingerprint string `json:"last_synced_fingerprint"`
	LastDeployStatus      string `json:"last_deploy_status"`
	LastDeployAt          string `json:"last_deploy_at"`
}

// CertDeployConfig 证书部署配置（Agent 使用）
type CertDeployConfig struct {
	MachineCertificateID string `json:"machine_certificate_id"`
	CertificateID        string `json:"certificate_id"`
	FingerprintSHA256    string `json:"fingerprint_sha256"`
	CertPath             string `json:"cert_path"`
	PrivateKeyPath       string `json:"private_key_path"`
	PostDeployCommands   string `json:"post_deploy_commands"`
	ConfigRevision       int    `json:"config_revision"`
}

// DeployResult 部署执行结果
type DeployResult struct {
	Status         string          `json:"status"`
	CommandOutputs []CommandOutput `json:"command_outputs"`
	ErrorMessage   string          `json:"error_message"`
	StartedAt      time.Time       `json:"started_at"`
	FinishedAt     time.Time       `json:"finished_at"`
}
