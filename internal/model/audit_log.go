package model

import "time"

// AuditLog 审计日志
type AuditLog struct {
	ID         string    `json:"id"`
	ActorType  string    `json:"actor_type"`
	ActorID    string    `json:"actor_id"`
	Action     string    `json:"action"`
	TargetType string    `json:"target_type"`
	TargetID   string    `json:"target_id"`
	Detail     string    `json:"detail"`
	IP         string    `json:"ip"`
	CreatedAt  time.Time `json:"created_at"`
}
