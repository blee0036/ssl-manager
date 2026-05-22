package model

import "time"

// DeploymentLog 部署日志
type DeploymentLog struct {
	ID                    string          `json:"id"`
	MachineCertificateID  string          `json:"machine_certificate_id"`
	MachineID             string          `json:"machine_id"`
	CertificateID         string          `json:"certificate_id"`
	Status                string          `json:"status"`
	CertFingerprintSHA256 string          `json:"cert_fingerprint_sha256"`
	CertPath              string          `json:"cert_path"`
	PrivateKeyPath        string          `json:"private_key_path"`
	CommandOutputs        []CommandOutput `json:"command_outputs"`
	ErrorMessage          string          `json:"error_message"`
	StartedAt             time.Time       `json:"started_at"`
	FinishedAt            time.Time       `json:"finished_at"`
	CreatedAt             time.Time       `json:"created_at"`
}

// CommandOutput 命令执行输出
type CommandOutput struct {
	Command  string `json:"command"`
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	TimedOut bool   `json:"timed_out,omitempty"`
}
