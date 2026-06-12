package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

// mockHTTPClient is a mock HTTP client for testing notification senders.
type mockHTTPClient struct {
	DoFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return m.DoFunc(req)
}

// mockNotificationSender is a mock notification sender for testing AlertService.
type mockNotificationSender struct {
	SendFunc     func(ctx context.Context, ch *model.NotificationChannel, title, content, level string) error
	SendTestFunc func(ctx context.Context, ch *model.NotificationChannel) error
	SendCalls    []mockSendCall
}

type mockSendCall struct {
	Channel *model.NotificationChannel
	Title   string
	Content string
	Level   string
}

func (m *mockNotificationSender) Send(ctx context.Context, ch *model.NotificationChannel, title, content, level string) error {
	m.SendCalls = append(m.SendCalls, mockSendCall{Channel: ch, Title: title, Content: content, Level: level})
	if m.SendFunc != nil {
		return m.SendFunc(ctx, ch, title, content, level)
	}
	return nil
}

func (m *mockNotificationSender) SendTest(ctx context.Context, ch *model.NotificationChannel) error {
	if m.SendTestFunc != nil {
		return m.SendTestFunc(ctx, ch)
	}
	return nil
}

func setupAlertTestDB(t *testing.T) (*repository.AlertRepository, *repository.NotificationChannelRepository) {
	t.Helper()
	db := setupTestDB(t)

	// Create alerts and notification_channels tables
	tables := []string{
		`CREATE TABLE IF NOT EXISTS alerts (
			id TEXT PRIMARY KEY,
			level TEXT NOT NULL CHECK(level IN ('info', 'warning', 'critical')),
			type TEXT NOT NULL,
			title TEXT NOT NULL,
			content TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active', 'resolved', 'suppressed')),
			target_type TEXT DEFAULT '',
			target_id TEXT DEFAULT '',
			sent_channels TEXT DEFAULT '',
			created_at TEXT NOT NULL,
			resolved_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS notification_channels (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL CHECK(type IN ('lark', 'telegram')),
			name TEXT NOT NULL,
			config_json TEXT NOT NULL DEFAULT '{}',
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
	}

	for _, stmt := range tables {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("failed to create table: %v", err)
		}
	}

	alertRepo := repository.NewAlertRepository(db)
	channelRepo := repository.NewNotificationChannelRepository(db)
	return alertRepo, channelRepo
}

func TestAlertService_Send_Success(t *testing.T) {
	alertRepo, channelRepo := setupAlertTestDB(t)
	ctx := context.Background()

	// Create an enabled notification channel
	ch := &model.NotificationChannel{
		Type:       "lark",
		Name:       "Test Lark",
		ConfigJSON: `{"webhook_url":"https://example.com/hook"}`,
		Enabled:    true,
	}
	if err := channelRepo.Create(ctx, ch); err != nil {
		t.Fatalf("failed to create channel: %v", err)
	}

	mockSender := &mockNotificationSender{}
	senders := map[string]NotificationSender{"lark": mockSender}
	svc := NewAlertServiceWithSenders(alertRepo, channelRepo, senders)

	alert := model.Alert{
		Level:      "warning",
		Type:       "cert_expiring",
		Title:      "Certificate Expiring",
		Content:    "Certificate xyz expires in 10 days",
		TargetType: "certificate",
		TargetID:   "cert-123",
	}

	err := svc.Send(ctx, alert)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	// Verify sender was called
	if len(mockSender.SendCalls) != 1 {
		t.Fatalf("expected 1 send call, got %d", len(mockSender.SendCalls))
	}
	if mockSender.SendCalls[0].Title != "Certificate Expiring" {
		t.Errorf("expected title 'Certificate Expiring', got %q", mockSender.SendCalls[0].Title)
	}

	// Verify alert was saved
	alerts, _, err := alertRepo.List(ctx, model.AlertFilter{})
	if err != nil {
		t.Fatalf("failed to list alerts: %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	if alerts[0].Status != "active" {
		t.Errorf("expected status 'active', got %q", alerts[0].Status)
	}
}

func TestAlertService_Send_Suppression(t *testing.T) {
	alertRepo, channelRepo := setupAlertTestDB(t)
	ctx := context.Background()

	// Create an enabled notification channel
	ch := &model.NotificationChannel{
		Type:       "lark",
		Name:       "Test Lark",
		ConfigJSON: `{"webhook_url":"https://example.com/hook"}`,
		Enabled:    true,
	}
	if err := channelRepo.Create(ctx, ch); err != nil {
		t.Fatalf("failed to create channel: %v", err)
	}

	mockSender := &mockNotificationSender{}
	senders := map[string]NotificationSender{"lark": mockSender}
	svc := NewAlertServiceWithSenders(alertRepo, channelRepo, senders)

	alert := model.Alert{
		Level:      "warning",
		Type:       "cert_expiring",
		Title:      "Certificate Expiring",
		Content:    "Certificate xyz expires in 10 days",
		TargetType: "certificate",
		TargetID:   "cert-123",
	}

	// First send should succeed
	if err := svc.Send(ctx, alert); err != nil {
		t.Fatalf("first Send failed: %v", err)
	}

	// Second send with same target should be suppressed
	if err := svc.Send(ctx, alert); err != nil {
		t.Fatalf("second Send failed: %v", err)
	}

	// Verify sender was only called once (second was suppressed)
	if len(mockSender.SendCalls) != 1 {
		t.Fatalf("expected 1 send call (suppressed duplicate), got %d", len(mockSender.SendCalls))
	}

	// Verify only one alert was saved
	alerts, _, err := alertRepo.List(ctx, model.AlertFilter{})
	if err != nil {
		t.Fatalf("failed to list alerts: %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert (suppressed duplicate), got %d", len(alerts))
	}
}

func TestAlertService_MarkResolved_SendsRecovery(t *testing.T) {
	alertRepo, channelRepo := setupAlertTestDB(t)
	ctx := context.Background()

	// Create an enabled notification channel
	ch := &model.NotificationChannel{
		Type:       "lark",
		Name:       "Test Lark",
		ConfigJSON: `{"webhook_url":"https://example.com/hook"}`,
		Enabled:    true,
	}
	if err := channelRepo.Create(ctx, ch); err != nil {
		t.Fatalf("failed to create channel: %v", err)
	}

	mockSender := &mockNotificationSender{}
	senders := map[string]NotificationSender{"lark": mockSender}
	svc := NewAlertServiceWithSenders(alertRepo, channelRepo, senders)

	// Send an alert first
	alert := model.Alert{
		Level:      "critical",
		Type:       "agent_offline",
		Title:      "Agent Offline",
		Content:    "Machine xyz is offline",
		TargetType: "machine",
		TargetID:   "machine-123",
	}
	if err := svc.Send(ctx, alert); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	// Get the saved alert
	alerts, _, err := alertRepo.List(ctx, model.AlertFilter{})
	if err != nil {
		t.Fatalf("failed to list alerts: %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}

	// Mark as resolved
	if err := svc.MarkResolved(ctx, alerts[0].ID); err != nil {
		t.Fatalf("MarkResolved failed: %v", err)
	}

	// Verify recovery notification was sent
	if len(mockSender.SendCalls) != 2 {
		t.Fatalf("expected 2 send calls (alert + recovery), got %d", len(mockSender.SendCalls))
	}
	if !strings.Contains(mockSender.SendCalls[1].Title, "[Recovered]") {
		t.Errorf("expected recovery title to contain '[Recovered]', got %q", mockSender.SendCalls[1].Title)
	}

	// Verify alert status is resolved
	resolved, err := alertRepo.GetByID(ctx, alerts[0].ID)
	if err != nil {
		t.Fatalf("failed to get alert: %v", err)
	}
	if resolved.Status != "resolved" {
		t.Errorf("expected status 'resolved', got %q", resolved.Status)
	}
	if resolved.ResolvedAt == nil {
		t.Error("expected resolved_at to be set")
	}
}

func TestAlertService_TestSend(t *testing.T) {
	alertRepo, channelRepo := setupAlertTestDB(t)
	ctx := context.Background()

	// Create a notification channel
	ch := &model.NotificationChannel{
		Type:       "telegram",
		Name:       "Test Telegram",
		ConfigJSON: `{"bot_token":"123:ABC","chat_id":"456"}`,
		Enabled:    true,
	}
	if err := channelRepo.Create(ctx, ch); err != nil {
		t.Fatalf("failed to create channel: %v", err)
	}

	mockSender := &mockNotificationSender{}
	senders := map[string]NotificationSender{"telegram": mockSender}
	svc := NewAlertServiceWithSenders(alertRepo, channelRepo, senders)

	if err := svc.TestSend(ctx, ch.ID); err != nil {
		t.Fatalf("TestSend failed: %v", err)
	}
}

func TestAlertService_ShouldSuppress_NoTarget(t *testing.T) {
	alertRepo, channelRepo := setupAlertTestDB(t)
	ctx := context.Background()

	svc := NewAlertServiceWithSenders(alertRepo, channelRepo, nil)

	// Alert without target should not be suppressed
	alert := model.Alert{
		Level:   "info",
		Type:    "test",
		Title:   "Test",
		Content: "Test content",
	}

	if svc.ShouldSuppress(ctx, alert) {
		t.Error("expected no suppression for alert without target")
	}
}

func TestAlertService_SendAlert_Interface(t *testing.T) {
	alertRepo, channelRepo := setupAlertTestDB(t)
	ctx := context.Background()

	// Create an enabled notification channel
	ch := &model.NotificationChannel{
		Type:       "lark",
		Name:       "Test Lark",
		ConfigJSON: `{"webhook_url":"https://example.com/hook"}`,
		Enabled:    true,
	}
	if err := channelRepo.Create(ctx, ch); err != nil {
		t.Fatalf("failed to create channel: %v", err)
	}

	mockSender := &mockNotificationSender{}
	senders := map[string]NotificationSender{"lark": mockSender}
	svc := NewAlertServiceWithSenders(alertRepo, channelRepo, senders)

	// Use the AlertSender interface method
	var sender AlertSender = svc
	err := sender.SendAlert(ctx, "warning", "test_type", "Test Title", "Test Content", "machine", "m-1")
	if err != nil {
		t.Fatalf("SendAlert failed: %v", err)
	}

	if len(mockSender.SendCalls) != 1 {
		t.Fatalf("expected 1 send call, got %d", len(mockSender.SendCalls))
	}
}

func TestAlertService_MarkResolved_AlreadyResolved(t *testing.T) {
	alertRepo, channelRepo := setupAlertTestDB(t)
	ctx := context.Background()

	mockSender := &mockNotificationSender{}
	senders := map[string]NotificationSender{"lark": mockSender}
	svc := NewAlertServiceWithSenders(alertRepo, channelRepo, senders)

	// Create an enabled channel
	ch := &model.NotificationChannel{
		Type:       "lark",
		Name:       "Test Lark",
		ConfigJSON: `{"webhook_url":"https://example.com/hook"}`,
		Enabled:    true,
	}
	if err := channelRepo.Create(ctx, ch); err != nil {
		t.Fatalf("failed to create channel: %v", err)
	}

	// Send an alert
	alert := model.Alert{
		Level:      "warning",
		Type:       "test",
		Title:      "Test",
		Content:    "Test content",
		TargetType: "machine",
		TargetID:   "m-1",
	}
	if err := svc.Send(ctx, alert); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	alerts, _, _ := alertRepo.List(ctx, model.AlertFilter{})
	alertID := alerts[0].ID

	// Mark resolved first time
	if err := svc.MarkResolved(ctx, alertID); err != nil {
		t.Fatalf("first MarkResolved failed: %v", err)
	}

	// Mark resolved second time should be no-op
	if err := svc.MarkResolved(ctx, alertID); err != nil {
		t.Fatalf("second MarkResolved failed: %v", err)
	}

	// Recovery notification should only be sent once
	recoveryCount := 0
	for _, call := range mockSender.SendCalls {
		if strings.Contains(call.Title, "[Recovered]") {
			recoveryCount++
		}
	}
	if recoveryCount != 1 {
		t.Errorf("expected 1 recovery notification, got %d", recoveryCount)
	}
}

// --- Lark Sender Tests ---

func TestLarkSender_Send_Success(t *testing.T) {
	client := &mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			// Verify request
			if req.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", req.Method)
			}
			if req.URL.String() != "https://open.lark.com/hook/test123" {
				t.Errorf("unexpected URL: %s", req.URL.String())
			}
			if req.Header.Get("Content-Type") != "application/json" {
				t.Errorf("expected Content-Type application/json, got %s", req.Header.Get("Content-Type"))
			}

			// Verify body
			body, _ := io.ReadAll(req.Body)
			var msg larkMessage
			if err := json.Unmarshal(body, &msg); err != nil {
				t.Fatalf("failed to unmarshal request body: %v", err)
			}
			if msg.MsgType != "text" {
				t.Errorf("expected msg_type 'text', got %q", msg.MsgType)
			}
			if !strings.Contains(msg.Content.Text, "Test Title") {
				t.Errorf("expected text to contain 'Test Title', got %q", msg.Content.Text)
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"code":0}`)),
			}, nil
		},
	}

	sender := NewLarkSender(client)
	ch := &model.NotificationChannel{
		Type:       "lark",
		ConfigJSON: `{"webhook_url":"https://open.lark.com/hook/test123"}`,
	}

	err := sender.Send(context.Background(), ch, "Test Title", "Test Content", "warning")
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}
}

func TestLarkSender_Send_EmptyWebhook(t *testing.T) {
	sender := NewLarkSender(&mockHTTPClient{})
	ch := &model.NotificationChannel{
		Type:       "lark",
		ConfigJSON: `{"webhook_url":""}`,
	}

	err := sender.Send(context.Background(), ch, "Test", "Content", "info")
	if err == nil {
		t.Fatal("expected error for empty webhook URL")
	}
	if !strings.Contains(err.Error(), "webhook_url is empty") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLarkSender_Send_HTTPError(t *testing.T) {
	client := &mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(strings.NewReader(`{"error":"internal"}`)),
			}, nil
		},
	}

	sender := NewLarkSender(client)
	ch := &model.NotificationChannel{
		Type:       "lark",
		ConfigJSON: `{"webhook_url":"https://open.lark.com/hook/test123"}`,
	}

	err := sender.Send(context.Background(), ch, "Test", "Content", "info")
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
	if !strings.Contains(err.Error(), "status 500") {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- Telegram Sender Tests ---

func TestTelegramSender_Send_Success(t *testing.T) {
	client := &mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			// Verify request
			if req.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", req.Method)
			}
			expectedURL := "https://api.telegram.org/bot123:ABC/sendMessage"
			if req.URL.String() != expectedURL {
				t.Errorf("expected URL %q, got %q", expectedURL, req.URL.String())
			}
			if req.Header.Get("Content-Type") != "application/json" {
				t.Errorf("expected Content-Type application/json")
			}

			// Verify body
			body, _ := io.ReadAll(req.Body)
			var msg telegramMessage
			if err := json.Unmarshal(body, &msg); err != nil {
				t.Fatalf("failed to unmarshal request body: %v", err)
			}
			if msg.ChatID != "456789" {
				t.Errorf("expected chat_id '456789', got %q", msg.ChatID)
			}
			if !strings.Contains(msg.Text, "Test Title") {
				t.Errorf("expected text to contain 'Test Title', got %q", msg.Text)
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			}, nil
		},
	}

	sender := NewTelegramSender(client)
	ch := &model.NotificationChannel{
		Type:       "telegram",
		ConfigJSON: `{"bot_token":"123:ABC","chat_id":"456789"}`,
	}

	err := sender.Send(context.Background(), ch, "Test Title", "Test Content", "critical")
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}
}

func TestTelegramSender_Send_EmptyBotToken(t *testing.T) {
	sender := NewTelegramSender(&mockHTTPClient{})
	ch := &model.NotificationChannel{
		Type:       "telegram",
		ConfigJSON: `{"bot_token":"","chat_id":"123"}`,
	}

	err := sender.Send(context.Background(), ch, "Test", "Content", "info")
	if err == nil {
		t.Fatal("expected error for empty bot_token")
	}
	if !strings.Contains(err.Error(), "bot_token is empty") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTelegramSender_Send_EmptyChatID(t *testing.T) {
	sender := NewTelegramSender(&mockHTTPClient{})
	ch := &model.NotificationChannel{
		Type:       "telegram",
		ConfigJSON: `{"bot_token":"123:ABC","chat_id":""}`,
	}

	err := sender.Send(context.Background(), ch, "Test", "Content", "info")
	if err == nil {
		t.Fatal("expected error for empty chat_id")
	}
	if !strings.Contains(err.Error(), "chat_id is empty") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTelegramSender_Send_HTTPError(t *testing.T) {
	client := &mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Body:       io.NopCloser(strings.NewReader(`{"ok":false,"description":"Unauthorized"}`)),
			}, nil
		},
	}

	sender := NewTelegramSender(client)
	ch := &model.NotificationChannel{
		Type:       "telegram",
		ConfigJSON: `{"bot_token":"invalid","chat_id":"123"}`,
	}

	err := sender.Send(context.Background(), ch, "Test", "Content", "info")
	if err == nil {
		t.Fatal("expected error for HTTP 401")
	}
	if !strings.Contains(err.Error(), "status 401") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLarkSender_SendTest(t *testing.T) {
	client := &mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(req.Body)
			var msg larkMessage
			json.Unmarshal(body, &msg)
			if !strings.Contains(msg.Content.Text, "test message") {
				t.Errorf("expected test message content, got %q", msg.Content.Text)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"code":0}`)),
			}, nil
		},
	}

	sender := NewLarkSender(client)
	ch := &model.NotificationChannel{
		Type:       "lark",
		ConfigJSON: `{"webhook_url":"https://open.lark.com/hook/test"}`,
	}

	err := sender.SendTest(context.Background(), ch)
	if err != nil {
		t.Fatalf("SendTest failed: %v", err)
	}
}

func TestTelegramSender_SendTest(t *testing.T) {
	client := &mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(req.Body)
			var msg telegramMessage
			json.Unmarshal(body, &msg)
			if !strings.Contains(msg.Text, "test message") {
				t.Errorf("expected test message content, got %q", msg.Text)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			}, nil
		},
	}

	sender := NewTelegramSender(client)
	ch := &model.NotificationChannel{
		Type:       "telegram",
		ConfigJSON: `{"bot_token":"123:ABC","chat_id":"456"}`,
	}

	err := sender.SendTest(context.Background(), ch)
	if err != nil {
		t.Fatalf("SendTest failed: %v", err)
	}
}
