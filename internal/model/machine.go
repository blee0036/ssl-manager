package model

import "time"

// Machine 机器实体
type Machine struct {
	ID                  string     `json:"id"`
	Name                string     `json:"name"`
	IP                  string     `json:"ip"`
	Hostname            string     `json:"hostname"`
	OS                  string     `json:"os"`
	Arch                string     `json:"arch"`
	Tags                []string   `json:"tags"`
	Remark              string     `json:"remark"`
	Status              string     `json:"status"`
	AgentVersion        string     `json:"agent_version"`
	AgentTokenHash      string     `json:"-"`
	AgentTokenRevokedAt *time.Time `json:"agent_token_revoked_at"`
	LastHeartbeatAt     *time.Time `json:"last_heartbeat_at"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}
