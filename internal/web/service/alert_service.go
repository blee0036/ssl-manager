package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

// HTTPClient is an interface for making HTTP requests, allowing mocking in tests.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// NotificationSender is an interface for sending notifications through a specific channel.
type NotificationSender interface {
	Send(ctx context.Context, channel *model.NotificationChannel, title, content, level string) error
	SendTest(ctx context.Context, channel *model.NotificationChannel) error
}

// AlertService handles alert sending, suppression, and history management.
// It implements the AlertSender interface used by scheduler and domain monitor.
type AlertService struct {
	alertRepo   *repository.AlertRepository
	channelRepo *repository.NotificationChannelRepository
	senders     map[string]NotificationSender
}

// NewAlertService creates a new AlertService.
func NewAlertService(
	alertRepo *repository.AlertRepository,
	channelRepo *repository.NotificationChannelRepository,
) *AlertService {
	httpClient := &http.Client{Timeout: 10 * time.Second}

	senders := map[string]NotificationSender{
		"lark":     NewLarkSender(httpClient),
		"telegram": NewTelegramSender(httpClient),
	}

	return &AlertService{
		alertRepo:   alertRepo,
		channelRepo: channelRepo,
		senders:     senders,
	}
}

// NewAlertServiceWithSenders creates an AlertService with custom senders (for testing).
func NewAlertServiceWithSenders(
	alertRepo *repository.AlertRepository,
	channelRepo *repository.NotificationChannelRepository,
	senders map[string]NotificationSender,
) *AlertService {
	return &AlertService{
		alertRepo:   alertRepo,
		channelRepo: channelRepo,
		senders:     senders,
	}
}

// SendAlert implements the AlertSender interface used by scheduler and domain monitor.
func (s *AlertService) SendAlert(ctx context.Context, level, alertType, title, content, targetType, targetID string) error {
	alert := model.Alert{
		Level:      level,
		Type:       alertType,
		Title:      title,
		Content:    content,
		TargetType: targetType,
		TargetID:   targetID,
	}
	return s.Send(ctx, alert)
}

// Send sends an alert through all enabled channels, with suppression logic.
func (s *AlertService) Send(ctx context.Context, alert model.Alert) error {
	// Check suppression: if same event is already active, don't send again
	if s.ShouldSuppress(ctx, alert) {
		log.Printf("[Alert] Suppressed duplicate alert: type=%s target=%s/%s", alert.Type, alert.TargetType, alert.TargetID)
		return nil
	}

	// Get all enabled notification channels
	channels, err := s.channelRepo.ListEnabled(ctx)
	if err != nil {
		return fmt.Errorf("failed to list enabled channels: %w", err)
	}

	// Send through each enabled channel
	var sentChannels []string
	for _, ch := range channels {
		sender, ok := s.senders[ch.Type]
		if !ok {
			log.Printf("[Alert] No sender for channel type: %s", ch.Type)
			continue
		}

		if err := sender.Send(ctx, ch, alert.Title, alert.Content, alert.Level); err != nil {
			log.Printf("[Alert] Failed to send via %s (%s): %v", ch.Name, ch.Type, err)
			continue
		}
		sentChannels = append(sentChannels, ch.ID)
	}

	// Save alert record
	alert.SentChannels = sentChannels
	alert.Status = "active"
	if err := s.alertRepo.Create(ctx, &alert); err != nil {
		return fmt.Errorf("failed to save alert: %w", err)
	}

	return nil
}

// TestSend sends a test message to a specific notification channel.
func (s *AlertService) TestSend(ctx context.Context, channelID string) error {
	ch, err := s.channelRepo.GetByID(ctx, channelID)
	if err != nil {
		return fmt.Errorf("failed to get channel: %w", err)
	}

	sender, ok := s.senders[ch.Type]
	if !ok {
		return fmt.Errorf("no sender for channel type: %s", ch.Type)
	}

	return sender.SendTest(ctx, ch)
}

// GetByID returns a single alert by ID.
func (s *AlertService) GetByID(ctx context.Context, id string) (*model.Alert, error) {
	return s.alertRepo.GetByID(ctx, id)
}

// GetHistory returns alert history with optional filtering.
func (s *AlertService) GetHistory(ctx context.Context, filter model.AlertFilter) ([]*model.Alert, error) {
	return s.alertRepo.List(ctx, filter)
}

// ListChannels returns all notification channels.
func (s *AlertService) ListChannels(ctx context.Context) ([]*model.NotificationChannel, error) {
	return s.channelRepo.List(ctx)
}

// GetChannel returns a notification channel by ID.
func (s *AlertService) GetChannel(ctx context.Context, id string) (*model.NotificationChannel, error) {
	return s.channelRepo.GetByID(ctx, id)
}

// CreateChannel creates a new notification channel.
func (s *AlertService) CreateChannel(ctx context.Context, ch *model.NotificationChannel) error {
	return s.channelRepo.Create(ctx, ch)
}

// UpdateChannel updates a notification channel.
func (s *AlertService) UpdateChannel(ctx context.Context, id string, updates map[string]interface{}) error {
	// Verify channel exists
	_, err := s.channelRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	return s.channelRepo.Update(ctx, id, updates)
}

// DeleteChannel deletes a notification channel.
func (s *AlertService) DeleteChannel(ctx context.Context, id string) error {
	// Verify channel exists
	_, err := s.channelRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	return s.channelRepo.Delete(ctx, id)
}

// ShouldSuppress checks if the same event is already active (not resolved).
// Returns true if the alert should be suppressed.
func (s *AlertService) ShouldSuppress(ctx context.Context, alert model.Alert) bool {
	if alert.TargetType == "" || alert.TargetID == "" {
		return false
	}

	existing, err := s.alertRepo.FindActiveByTarget(ctx, alert.TargetType, alert.TargetID, alert.Type)
	if err != nil {
		log.Printf("[Alert] Error checking suppression: %v", err)
		return false
	}

	return existing != nil
}

// MarkResolved marks an alert as resolved and sends a recovery notification.
func (s *AlertService) MarkResolved(ctx context.Context, alertID string) error {
	// Get the alert
	alert, err := s.alertRepo.GetByID(ctx, alertID)
	if err != nil {
		return fmt.Errorf("failed to get alert: %w", err)
	}

	if alert.Status == "resolved" {
		return nil // Already resolved
	}

	// Update status to resolved
	now := time.Now().UTC()
	if err := s.alertRepo.UpdateStatus(ctx, alertID, "resolved", &now); err != nil {
		return fmt.Errorf("failed to update alert status: %w", err)
	}

	// Send recovery notification
	s.sendRecoveryNotification(ctx, alert)

	return nil
}

// SuppressActiveByTarget sets all active alerts for a given target to 'suppressed' status.
// Called when a domain is marked as alert_ignored to suppress its existing active alerts.
func (s *AlertService) SuppressActiveByTarget(ctx context.Context, targetType, targetID string) error {
	return s.alertRepo.SuppressActiveByTarget(ctx, targetType, targetID)
}

// AutoResolve resolves all active alerts matching the given target type, target ID, and alert type.
// Called automatically when the condition that triggered the alert is no longer present.
func (s *AlertService) AutoResolve(ctx context.Context, targetType, targetID, alertType string) {
	alert, err := s.alertRepo.FindActiveByTarget(ctx, targetType, targetID, alertType)
	if err != nil || alert == nil {
		return // No active alert to resolve
	}

	now := time.Now().UTC()
	if err := s.alertRepo.UpdateStatus(ctx, alert.ID, "resolved", &now); err != nil {
		log.Printf("[Alert] Failed to auto-resolve alert %s: %v", alert.ID, err)
		return
	}

	// Send recovery notification
	s.sendRecoveryNotification(ctx, alert)
	log.Printf("[Alert] Auto-resolved alert %s (type=%s, target=%s/%s)", alert.ID, alertType, targetType, targetID)
}

// sendRecoveryNotification sends a recovery notification through all enabled channels.
func (s *AlertService) sendRecoveryNotification(ctx context.Context, alert *model.Alert) {
	channels, err := s.channelRepo.ListEnabled(ctx)
	if err != nil {
		log.Printf("[Alert] Failed to list channels for recovery notification: %v", err)
		return
	}

	recoveryTitle := fmt.Sprintf("[Recovered] %s", alert.Title)
	recoveryContent := fmt.Sprintf("The following alert has been resolved:\n\n%s\n\nOriginal alert time: %s",
		alert.Content, alert.CreatedAt.Format(time.RFC3339))

	for _, ch := range channels {
		sender, ok := s.senders[ch.Type]
		if !ok {
			continue
		}

		if err := sender.Send(ctx, ch, recoveryTitle, recoveryContent, "info"); err != nil {
			log.Printf("[Alert] Failed to send recovery via %s (%s): %v", ch.Name, ch.Type, err)
		}
	}
}

// --- Lark Sender ---

// LarkSender sends messages via Lark/Feishu webhook.
type LarkSender struct {
	client HTTPClient
}

// NewLarkSender creates a new LarkSender.
func NewLarkSender(client HTTPClient) *LarkSender {
	return &LarkSender{client: client}
}

// larkConfig represents the configuration for a Lark webhook channel.
type larkConfig struct {
	WebhookURL string `json:"webhook_url"`
}

// larkMessage represents the message body sent to Lark webhook.
type larkMessage struct {
	MsgType string      `json:"msg_type"`
	Content larkContent `json:"content"`
}

type larkContent struct {
	Text string `json:"text"`
}

// Send sends a message via Lark webhook.
func (s *LarkSender) Send(ctx context.Context, channel *model.NotificationChannel, title, content, level string) error {
	var cfg larkConfig
	if err := json.Unmarshal([]byte(channel.ConfigJSON), &cfg); err != nil {
		return fmt.Errorf("failed to parse lark config: %w", err)
	}

	if cfg.WebhookURL == "" {
		return fmt.Errorf("lark webhook_url is empty")
	}

	// Format message text
	text := fmt.Sprintf("[%s] %s\n\n%s", level, title, content)

	msg := larkMessage{
		MsgType: "text",
		Content: larkContent{Text: text},
	}

	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal lark message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create lark request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send lark message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("lark webhook returned status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// SendTest sends a test message via Lark webhook.
func (s *LarkSender) SendTest(ctx context.Context, channel *model.NotificationChannel) error {
	return s.Send(ctx, channel, "Test Alert", "This is a test message from SSL Manager.", "info")
}

// --- Telegram Sender ---

// TelegramSender sends messages via Telegram Bot API.
type TelegramSender struct {
	client HTTPClient
}

// NewTelegramSender creates a new TelegramSender.
func NewTelegramSender(client HTTPClient) *TelegramSender {
	return &TelegramSender{client: client}
}

// telegramConfig represents the configuration for a Telegram bot channel.
type telegramConfig struct {
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
}

// telegramMessage represents the message body sent to Telegram Bot API.
type telegramMessage struct {
	ChatID    string `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode,omitempty"`
}

// Send sends a message via Telegram Bot API.
func (s *TelegramSender) Send(ctx context.Context, channel *model.NotificationChannel, title, content, level string) error {
	var cfg telegramConfig
	if err := json.Unmarshal([]byte(channel.ConfigJSON), &cfg); err != nil {
		return fmt.Errorf("failed to parse telegram config: %w", err)
	}

	if cfg.BotToken == "" {
		return fmt.Errorf("telegram bot_token is empty")
	}
	if cfg.ChatID == "" {
		return fmt.Errorf("telegram chat_id is empty")
	}

	// Format message text
	text := fmt.Sprintf("[%s] %s\n\n%s", level, title, content)

	msg := telegramMessage{
		ChatID: cfg.ChatID,
		Text:   text,
	}

	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal telegram message: %w", err)
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", cfg.BotToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create telegram request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send telegram message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// SendTest sends a test message via Telegram Bot API.
func (s *TelegramSender) SendTest(ctx context.Context, channel *model.NotificationChannel) error {
	return s.Send(ctx, channel, "Test Alert", "This is a test message from SSL Manager.", "info")
}
