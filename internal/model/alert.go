package model

import "time"

// Alert 告警记录
type Alert struct {
	ID           string     `json:"id"`
	Level        string     `json:"level"`
	Type         string     `json:"type"`
	Title        string     `json:"title"`
	Content      string     `json:"content"`
	Status       string     `json:"status"`
	TargetType   string     `json:"target_type"`
	TargetID     string     `json:"target_id"`
	SentChannels []string   `json:"sent_channels"`
	CreatedAt    time.Time  `json:"created_at"`
	ResolvedAt   *time.Time `json:"resolved_at"`
}

// NotificationChannel 通知渠道配置
type NotificationChannel struct {
	ID         string    `json:"id"`
	Type       string    `json:"type"`
	Name       string    `json:"name"`
	ConfigJSON string    `json:"config_json"`
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}
