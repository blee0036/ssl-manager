package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// DefaultConfigPath is the default path for the config file.
const DefaultConfigPath = "./data/config.json"

// Built-in defaults for the alert flap-damping knobs.
//
// These are exported and referenced both by DefaultConfig() and by the Effective*
// accessors below, so a config file that predates a field (or a client that omits it)
// resolves to exactly the same value the defaults would have produced.
const (
	// DefaultHeartbeatTimeoutSeconds is the silence window after which a machine is
	// marked offline in the machine list.
	DefaultHeartbeatTimeoutSeconds = 120

	// DefaultOfflineAlertAfterSeconds is the silence window after which an agent_offline
	// notification is pushed. Deliberately much larger than the offline threshold so a
	// brief agent hiccup never produces an alert/recovery message pair.
	DefaultOfflineAlertAfterSeconds = 600

	// DefaultDomainMonitorTimeoutSeconds is the per-attempt TLS probe timeout.
	DefaultDomainMonitorTimeoutSeconds = 15

	// DefaultDomainMonitorProbeRetries is the number of extra attempts within one probe
	// round after the first failure.
	DefaultDomainMonitorProbeRetries = 2

	// DefaultDomainMonitorRetryDelaySeconds is the wait between two probe attempts.
	DefaultDomainMonitorRetryDelaySeconds = 2

	// DefaultDomainMonitorAlertAfterFailures is how many consecutive failed probe rounds
	// are required before a domain monitor alert is pushed.
	DefaultDomainMonitorAlertAfterFailures = 2
)

// Config represents the global system configuration stored in config.json.
type Config struct {
	Server        ServerConfig        `json:"server"`
	Auth          AuthConfig          `json:"auth"`
	Agent         AgentConfig         `json:"agent"`
	Alert         AlertConfig         `json:"alert"`
	Certbot       CertbotConfig       `json:"certbot"`
	Readonly      ReadonlyConfig      `json:"readonly"`
	DomainMonitor DomainMonitorConfig `json:"domain_monitor"`
	Turnstile     TurnstileConfig     `json:"turnstile"`
	ThirdpartDNS  ThirdpartDNSConfig  `json:"thirdpart_dns"`
	Cleanup       CleanupConfig       `json:"cleanup"`
	DomainExpiry  DomainExpiryConfig  `json:"domain_expiry"`
}

// ServerConfig holds settings for the Web Backend server.
type ServerConfig struct {
	ExternalURL string `json:"external_url"` // Web 外部访问地址
	ListenAddr  string `json:"listen_addr"`  // 监听地址，默认 :8080
}

// AuthConfig holds settings for authentication/session behavior.
type AuthConfig struct {
	SessionExpiryHours int `json:"session_expiry_hours"` // 登录 session（JWT）有效期小时数，默认 24
}

// AgentConfig holds settings for Agent communication.
type AgentConfig struct {
	HeartbeatTimeoutSeconds int `json:"heartbeat_timeout_seconds"` // 心跳超时秒数，默认 120
	PollIntervalSeconds     int `json:"poll_interval_seconds"`     // Agent 轮询间隔，默认 60
	// OfflineAlertAfterSeconds 是推送 agent_offline 告警前要求的心跳静默时长（秒），默认 600。
	//
	// 它与 HeartbeatTimeoutSeconds 是两个独立的阈值，用于抑制告警抖动：
	//   - HeartbeatTimeoutSeconds 决定机器多久没心跳就在界面上标记为 offline（保持列表状态准确）。
	//   - OfflineAlertAfterSeconds 决定离线持续多久才真正推送告警通知。
	//
	// 只设一个阈值时，一次网络抖动（漏掉两次心跳）就会立刻推一条告警，
	// 紧接着下一次心跳恢复又推一条 [Recovered]，一次抖动产生两条噪声消息。
	// 实际生效的告警阈值是 max(HeartbeatTimeoutSeconds, OfflineAlertAfterSeconds)，
	// 因此 <=0 表示不额外延迟，退回到“标记离线即告警”的旧行为。
	OfflineAlertAfterSeconds int `json:"offline_alert_after_seconds"`
}

// EffectiveHeartbeatTimeoutSeconds returns the heartbeat silence window after which a
// machine is flipped to "offline" in the machine list. Falls back to the built-in
// default when unset, so a config written by an older client cannot disable the check.
func (c AgentConfig) EffectiveHeartbeatTimeoutSeconds() int {
	if c.HeartbeatTimeoutSeconds <= 0 {
		return DefaultHeartbeatTimeoutSeconds
	}
	return c.HeartbeatTimeoutSeconds
}

// EffectiveOfflineAlertAfterSeconds returns the heartbeat silence window after which an
// agent_offline notification is actually pushed.
//
// The result is never below EffectiveHeartbeatTimeoutSeconds: alerting earlier than the
// machine is even considered offline would be meaningless. A zero/unset
// OfflineAlertAfterSeconds falls back to the built-in default rather than to "no delay",
// so an older client that does not know this field cannot silently turn damping off.
func (c AgentConfig) EffectiveOfflineAlertAfterSeconds() int {
	after := c.OfflineAlertAfterSeconds
	if after <= 0 {
		after = DefaultOfflineAlertAfterSeconds
	}
	if timeout := c.EffectiveHeartbeatTimeoutSeconds(); after < timeout {
		return timeout
	}
	return after
}

// AlertConfig holds settings for alert notifications.
type AlertConfig struct {
	DefaultBeforeDays int `json:"default_before_days"` // 证书过期提前告警天数，默认 15
}

// CertbotConfig holds settings for Certbot integration.
type CertbotConfig struct {
	BinaryPath string `json:"binary_path"` // certbot 二进制路径，默认 "certbot"
	DataDir    string `json:"data_dir"`    // certbot 工作目录
	Email      string `json:"email"`       // certbot 注册邮箱
}

// ReadonlyConfig holds settings for the read-only access mode.
type ReadonlyConfig struct {
	Enabled      bool   `json:"enabled"`
	ViewPassword string `json:"view_password"`
}

// DomainMonitorConfig holds settings for domain SSL monitoring.
type DomainMonitorConfig struct {
	DefaultPort     int `json:"default_port"`     // 默认监控端口，默认 443
	IntervalMinutes int `json:"interval_minutes"` // 监控间隔分钟数，默认 60
	// TimeoutSeconds 是单次 TLS 探测（DNS 解析 + 握手）的超时秒数，默认 15。
	// 过短的超时会把源站的正常抖动误判成 "context deadline exceeded" 故障。
	TimeoutSeconds int `json:"timeout_seconds"`
	// ProbeRetries 是单轮探测中失败后的重试次数，默认 2；0 表示不重试。
	// 瞬时丢包和握手超时在当轮重试里就能消化掉，不会落成一条失败记录。
	ProbeRetries int `json:"probe_retries"`
	// RetryDelaySeconds 是两次探测尝试之间的等待秒数，默认 2；0 表示立即重试。
	RetryDelaySeconds int `json:"retry_delay_seconds"`
	// AlertAfterFailures 是推送域名监控告警前要求的连续失败探测轮数，默认 2；
	// <=1 表示首轮失败就告警（旧行为）。
	//
	// 判定依据是 domain_monitor_results 表里最近若干条记录，
	// 所以计数天然跨进程重启保留，不需要额外的内存状态。
	AlertAfterFailures int `json:"alert_after_failures"`
}

// EffectiveTimeout returns the per-attempt timeout for one TLS probe.
// Falls back to the built-in default when unset.
func (c DomainMonitorConfig) EffectiveTimeout() time.Duration {
	seconds := c.TimeoutSeconds
	if seconds <= 0 {
		seconds = DefaultDomainMonitorTimeoutSeconds
	}
	return time.Duration(seconds) * time.Second
}

// EffectiveProbeRetries returns the number of extra attempts after the first failure.
// Unlike the other knobs, an explicit 0 is honoured as "do not retry" — that is the
// natural encoding of the setting, and losing in-round retries still leaves the
// consecutive-failure gate in place.
func (c DomainMonitorConfig) EffectiveProbeRetries() int {
	if c.ProbeRetries < 0 {
		return 0
	}
	return c.ProbeRetries
}

// EffectiveRetryDelay returns the wait between two probe attempts.
// An explicit 0 is honoured as "retry immediately".
func (c DomainMonitorConfig) EffectiveRetryDelay() time.Duration {
	if c.RetryDelaySeconds <= 0 {
		return 0
	}
	return time.Duration(c.RetryDelaySeconds) * time.Second
}

// EffectiveAlertAfterFailures returns how many consecutive failed probe rounds are
// required before a domain monitor alert is pushed. Always >= 1; a zero/unset value
// falls back to the built-in default so damping stays on by default.
func (c DomainMonitorConfig) EffectiveAlertAfterFailures() int {
	if c.AlertAfterFailures <= 0 {
		return DefaultDomainMonitorAlertAfterFailures
	}
	return c.AlertAfterFailures
}

// TurnstileConfig holds settings for Cloudflare Turnstile human verification.
type TurnstileConfig struct {
	Enabled   bool   `json:"enabled"`    // 是否启用 Turnstile 验证，默认 false
	SiteKey   string `json:"site_key"`   // Turnstile site key（前端使用）
	SecretKey string `json:"secret_key"` // Turnstile secret key（仅后端使用，绝不下发前端）
}

// ThirdpartDNSConfig holds settings for third-party DNS sync scheduling.
type ThirdpartDNSConfig struct {
	SyncIntervalMinutes int `json:"sync_interval_minutes"` // 定时同步间隔分钟数，默认 360；<=0 禁用定时同步
}

// CleanupConfig holds settings for periodic data cleanup.
type CleanupConfig struct {
	RetentionDays int `json:"retention_days"` // 保留天数，超过此天数的旧记录会被清理，默认 7；<=0 禁用清理
	MinKeepCount  int `json:"min_keep_count"` // 每个表最少保留的记录数，默认 1000
}

// DomainExpiryConfig holds settings for domain registration expiry monitoring (WHOIS).
type DomainExpiryConfig struct {
	ExpiryThresholdDays    int `json:"expiry_threshold_days"`    // 到期预警阈值天数，默认 14
	RefreshIntervalMinutes int `json:"refresh_interval_minutes"` // 刷新间隔分钟数，默认 1440；<=0 停用周期刷新
	WhoisTimeoutSeconds    int `json:"whois_timeout_seconds"`    // 单次 WHOIS 查询超时秒数，默认 10
}

// MaxSessionExpiryHours caps auth.session_expiry_hours.
//
// It serves the same two purposes as MaxRefreshIntervalMinutes below: a sane
// business ceiling (a login session valid for more than a year is never
// useful) and a safety bound that keeps time.Duration(hours) * time.Hour far
// below the int64 nanosecond overflow point, so jwt.NewNumericDate never
// receives an overflowed, non-monotonic time.
//
// 8760 == 365 * 24 hours (365 days).
const MaxSessionExpiryHours = 8760

// MaxRefreshIntervalMinutes caps domain_expiry.refresh_interval_minutes.
//
// It serves two purposes:
//  1. A sane business ceiling — refreshing WHOIS registration expiry less often
//     than once every 365 days is never useful.
//  2. A safety bound that keeps the value far below the point where
//     time.Duration(minutes) * time.Minute overflows int64 nanoseconds (~153,722,867
//     minutes on 64-bit). An overflowed, non-positive duration passed to
//     time.NewTicker panics and would crash the process, so rejecting anything
//     above this cap during config validation prevents that failure mode.
//
// 525600 == 365 * 24 * 60 minutes (365 days).
const MaxRefreshIntervalMinutes = 525600

// Upper bounds for the alert flap-damping knobs.
//
// These are business ceilings rather than overflow guards — every value below is
// multiplied by time.Second and compared against a duration, so none of them can
// overflow int64 nanoseconds at these magnitudes. The caps exist so a typo like
// 6000000 cannot silently disable alerting altogether.
const (
	// MaxOfflineAlertAfterSeconds caps agent.offline_alert_after_seconds.
	// 86400 == 24 hours; delaying an offline alert by more than a day is never useful.
	MaxOfflineAlertAfterSeconds = 86400

	// MaxDomainMonitorTimeoutSeconds caps domain_monitor.timeout_seconds.
	// A single TLS probe that needs more than 5 minutes is indistinguishable from a failure.
	MaxDomainMonitorTimeoutSeconds = 300

	// MaxDomainMonitorProbeRetries caps domain_monitor.probe_retries.
	// Retries are serialized within one probe round, so a large value would stall ProbeAll.
	MaxDomainMonitorProbeRetries = 10

	// MaxDomainMonitorRetryDelaySeconds caps domain_monitor.retry_delay_seconds.
	MaxDomainMonitorRetryDelaySeconds = 60

	// MaxDomainMonitorAlertAfterFailures caps domain_monitor.alert_after_failures.
	// Requiring more than 10 consecutive failed rounds would delay real outage alerts
	// by 10 monitor intervals (10 hours at the default interval).
	MaxDomainMonitorAlertAfterFailures = 10
)

// DefaultConfig returns a Config with sensible default values.
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			ExternalURL: "http://localhost:8080",
			ListenAddr:  ":8080",
		},
		Auth: AuthConfig{
			SessionExpiryHours: 24,
		},
		Agent: AgentConfig{
			HeartbeatTimeoutSeconds:  DefaultHeartbeatTimeoutSeconds,
			PollIntervalSeconds:      60,
			OfflineAlertAfterSeconds: DefaultOfflineAlertAfterSeconds,
		},
		Alert: AlertConfig{
			DefaultBeforeDays: 15,
		},
		Certbot: CertbotConfig{
			BinaryPath: "certbot",
			DataDir:    "",
			Email:      "",
		},
		Readonly: ReadonlyConfig{
			Enabled:      false,
			ViewPassword: "",
		},
		DomainMonitor: DomainMonitorConfig{
			DefaultPort:        443,
			IntervalMinutes:    60,
			TimeoutSeconds:     DefaultDomainMonitorTimeoutSeconds,
			ProbeRetries:       DefaultDomainMonitorProbeRetries,
			RetryDelaySeconds:  DefaultDomainMonitorRetryDelaySeconds,
			AlertAfterFailures: DefaultDomainMonitorAlertAfterFailures,
		},
		Turnstile: TurnstileConfig{
			Enabled:   false,
			SiteKey:   "",
			SecretKey: "",
		},
		ThirdpartDNS: ThirdpartDNSConfig{
			SyncIntervalMinutes: 360,
		},
		Cleanup: CleanupConfig{
			RetentionDays: 7,
			MinKeepCount:  1000,
		},
		DomainExpiry: DomainExpiryConfig{
			ExpiryThresholdDays:    14,
			RefreshIntervalMinutes: 1440,
			WhoisTimeoutSeconds:    10,
		},
	}
}

// LoadConfig reads and parses a config.json file from the given path.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Note: LoadConfig unmarshals into DefaultConfig() which has SyncIntervalMinutes=360.
	// If the JSON file has "thirdpart_dns": {"sync_interval_minutes": 0}, that 0 is an
	// explicit "disabled" value and must be preserved. Old config files without the field
	// will keep the default 360 from DefaultConfig(). No normalization needed here.

	if err := ValidateConfig(cfg); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return cfg, nil
}

// SaveConfig writes the config to the given path with proper JSON formatting.
// It creates parent directories if they don't exist and sets file permissions to 0600.
func SaveConfig(path string, cfg *Config) error {
	if err := ValidateConfig(cfg); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Append newline for POSIX compliance
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// ValidateConfig checks that required fields are set and values are within valid ranges.
func ValidateConfig(cfg *Config) error {
	if cfg == nil {
		return errors.New("config is nil")
	}

	if cfg.Server.ExternalURL == "" {
		return errors.New("server.external_url is required")
	}

	if cfg.Server.ListenAddr == "" {
		return errors.New("server.listen_addr is required")
	}

	if cfg.Auth.SessionExpiryHours <= 0 {
		return errors.New("auth.session_expiry_hours must be positive")
	}
	if cfg.Auth.SessionExpiryHours > MaxSessionExpiryHours {
		return errors.New("auth.session_expiry_hours must not exceed 8760 (365 days)")
	}

	if cfg.Agent.HeartbeatTimeoutSeconds <= 0 {
		return errors.New("agent.heartbeat_timeout_seconds must be positive")
	}

	if cfg.Agent.PollIntervalSeconds <= 0 {
		return errors.New("agent.poll_interval_seconds must be positive")
	}

	// agent.offline_alert_after_seconds and the domain_monitor damping knobs below use
	// lenient validation on purpose: only negative values and values above the cap are
	// rejected, while 0 is accepted and resolved to the built-in default by the
	// Effective* accessors.
	//
	// The reason is the PUT /api/system/config contract — it is a full overwrite, so a
	// browser still running a bundle that predates these fields would send them as 0.
	// Rejecting 0 would lock such a client out of saving any setting at all, while
	// accepting it simply keeps damping at its default.
	if cfg.Agent.OfflineAlertAfterSeconds < 0 {
		return errors.New("agent.offline_alert_after_seconds must not be negative")
	}
	if cfg.Agent.OfflineAlertAfterSeconds > MaxOfflineAlertAfterSeconds {
		return errors.New("agent.offline_alert_after_seconds must not exceed 86400 (24 hours)")
	}

	if cfg.Alert.DefaultBeforeDays <= 0 {
		return errors.New("alert.default_before_days must be positive")
	}

	if cfg.Readonly.Enabled && cfg.Readonly.ViewPassword == "" {
		return errors.New("readonly.view_password is required when readonly mode is enabled")
	}

	if cfg.DomainMonitor.DefaultPort <= 0 || cfg.DomainMonitor.DefaultPort > 65535 {
		return errors.New("domain_monitor.default_port must be between 1 and 65535")
	}

	if cfg.DomainMonitor.IntervalMinutes <= 0 {
		return errors.New("domain_monitor.interval_minutes must be positive")
	}

	if cfg.DomainMonitor.TimeoutSeconds < 0 {
		return errors.New("domain_monitor.timeout_seconds must not be negative")
	}
	if cfg.DomainMonitor.TimeoutSeconds > MaxDomainMonitorTimeoutSeconds {
		return errors.New("domain_monitor.timeout_seconds must not exceed 300")
	}

	if cfg.DomainMonitor.ProbeRetries < 0 {
		return errors.New("domain_monitor.probe_retries must not be negative")
	}
	if cfg.DomainMonitor.ProbeRetries > MaxDomainMonitorProbeRetries {
		return errors.New("domain_monitor.probe_retries must not exceed 10")
	}

	if cfg.DomainMonitor.RetryDelaySeconds < 0 {
		return errors.New("domain_monitor.retry_delay_seconds must not be negative")
	}
	if cfg.DomainMonitor.RetryDelaySeconds > MaxDomainMonitorRetryDelaySeconds {
		return errors.New("domain_monitor.retry_delay_seconds must not exceed 60")
	}

	if cfg.DomainMonitor.AlertAfterFailures < 0 {
		return errors.New("domain_monitor.alert_after_failures must not be negative")
	}
	if cfg.DomainMonitor.AlertAfterFailures > MaxDomainMonitorAlertAfterFailures {
		return errors.New("domain_monitor.alert_after_failures must not exceed 10")
	}

	if cfg.Turnstile.Enabled {
		if cfg.Turnstile.SiteKey == "" {
			return errors.New("turnstile.site_key is required when turnstile is enabled")
		}
		if cfg.Turnstile.SecretKey == "" {
			return errors.New("turnstile.secret_key is required when turnstile is enabled")
		}
	}

	if cfg.DomainExpiry.ExpiryThresholdDays <= 0 {
		return errors.New("domain_expiry.expiry_threshold_days must be positive")
	}

	if cfg.DomainExpiry.WhoisTimeoutSeconds <= 0 {
		return errors.New("domain_expiry.whois_timeout_seconds must be positive")
	}
	// domain_expiry.refresh_interval_minutes has asymmetric validation:
	//   - A value <= 0 is a valid "disabled" state that stops periodic WHOIS refresh
	//     (mirrors thirdpart_dns.sync_interval_minutes semantics), so it is NOT rejected.
	//   - A positive value is capped at MaxRefreshIntervalMinutes. Beyond that cap the
	//     value would eventually overflow when multiplied by time.Minute, producing a
	//     non-positive time.Duration that makes time.NewTicker panic and crash the
	//     process. Rejecting it here stops such a config from being saved or loaded.
	if cfg.DomainExpiry.RefreshIntervalMinutes > MaxRefreshIntervalMinutes {
		return errors.New("domain_expiry.refresh_interval_minutes must not exceed 525600 (365 days)")
	}

	return nil
}

// CheckFilePermissions checks if the config file has secure permissions (0600).
// On non-Unix systems (Windows), this check is skipped with an info log.
// Returns nil if permissions are correct or if running on a non-Unix system.
// Logs a security warning if permissions are too open.
func CheckFilePermissions(path string) error {
	if runtime.GOOS == "windows" {
		log.Printf("[INFO] Skipping file permission check on Windows for: %s", path)
		return nil
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // File doesn't exist yet, no warning needed
		}
		return fmt.Errorf("failed to stat config file: %w", err)
	}

	mode := info.Mode().Perm()
	if mode != 0600 {
		log.Printf("[SECURITY WARNING] Config file %s has permissions %04o, expected 0600. "+
			"This file may contain sensitive data (passwords, tokens). "+
			"Please run: chmod 600 %s", path, mode, path)
	}

	return nil
}
