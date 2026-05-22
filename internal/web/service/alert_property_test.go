package service

import (
	"context"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

// --- Generators ---

// genAlertType generates random alert type strings.
func genAlertType() gopter.Gen {
	return gen.OneConstOf(
		"cert_expiring",
		"cert_expired",
		"cert_renew_failed",
		"agent_offline",
		"agent_token_revoked",
		"deploy_failed",
		"domain_probe_failed",
		"domain_cert_mismatch",
		"cloudflare_sync_failed",
	)
}

// genTargetType generates random target type strings.
func genTargetType() gopter.Gen {
	return gen.OneConstOf(
		"certificate",
		"machine",
		"domain",
		"thirdpart_dns",
		"machine_certificate",
	)
}

// genTargetID generates random non-empty target ID strings.
func genTargetID() gopter.Gen {
	return gen.AlphaString().SuchThat(func(s string) bool {
		return len(s) > 0 && len(s) <= 50
	})
}

// genRepeatCount generates a random number of duplicate sends (2-10).
func genRepeatCount() gopter.Gen {
	return gen.IntRange(2, 10)
}

// genAlertLevel generates a valid alert level.
func genAlertLevel() gopter.Gen {
	return gen.OneConstOf("info", "warning", "critical")
}

// --- Helper ---

// setupAlertPropertyTestDB creates an in-memory SQLite database with alert and notification tables.
func setupAlertPropertyTestDB(t *testing.T) (*repository.AlertRepository, *repository.NotificationChannelRepository) {
	t.Helper()
	db := setupTestDB(t)

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

// --- Property Tests ---

// TestProperty20_DuplicateAlertSuppression verifies that while the same alert event
// is unresolved, the system should suppress duplicate alerts and not send them again.
// After resolving the alert, a new alert of the same type can be sent again.
//
// **Validates: Requirements 12.6**
func TestProperty20_DuplicateAlertSuppression(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	// Property: Sending the same alert N times (2-10) while unresolved results in only 1 notification sent
	properties.Property("duplicate alerts are suppressed while unresolved", prop.ForAll(
		func(alertType string, targetType string, targetID string, repeatCount int, level string) bool {
			alertRepo, channelRepo := setupAlertPropertyTestDB(t)
			ctx := context.Background()

			// Create an enabled notification channel
			ch := &model.NotificationChannel{
				Type:       "lark",
				Name:       "Test Channel",
				ConfigJSON: `{"webhook_url":"https://example.com/hook"}`,
				Enabled:    true,
			}
			if err := channelRepo.Create(ctx, ch); err != nil {
				t.Logf("Failed to create channel: %v", err)
				return false
			}

			mockSender := &mockNotificationSender{}
			senders := map[string]NotificationSender{"lark": mockSender}
			svc := NewAlertServiceWithSenders(alertRepo, channelRepo, senders)

			alert := model.Alert{
				Level:      level,
				Type:       alertType,
				Title:      "Test Alert",
				Content:    "Test content for property test",
				TargetType: targetType,
				TargetID:   targetID,
			}

			// Send the same alert repeatCount times
			for i := 0; i < repeatCount; i++ {
				if err := svc.Send(ctx, alert); err != nil {
					t.Logf("Send failed on attempt %d: %v", i+1, err)
					return false
				}
			}

			// Only the first alert should have been sent through notification channels
			if len(mockSender.SendCalls) != 1 {
				t.Logf("Expected 1 send call, got %d (repeatCount=%d, type=%s, target=%s/%s)",
					len(mockSender.SendCalls), repeatCount, alertType, targetType, targetID)
				return false
			}

			// Only one alert record should exist in the database
			alerts, err := alertRepo.List(ctx, model.AlertFilter{})
			if err != nil {
				t.Logf("Failed to list alerts: %v", err)
				return false
			}
			if len(alerts) != 1 {
				t.Logf("Expected 1 alert record, got %d", len(alerts))
				return false
			}

			// The saved alert should be active
			if alerts[0].Status != "active" {
				t.Logf("Expected alert status 'active', got %q", alerts[0].Status)
				return false
			}

			return true
		},
		genAlertType(),
		genTargetType(),
		genTargetID(),
		genRepeatCount(),
		genAlertLevel(),
	))

	// Property: After resolving an alert, a new alert of the same type can be sent again
	properties.Property("after resolving, same alert type can be sent again", prop.ForAll(
		func(alertType string, targetType string, targetID string, level string) bool {
			alertRepo, channelRepo := setupAlertPropertyTestDB(t)
			ctx := context.Background()

			// Create an enabled notification channel
			ch := &model.NotificationChannel{
				Type:       "lark",
				Name:       "Test Channel",
				ConfigJSON: `{"webhook_url":"https://example.com/hook"}`,
				Enabled:    true,
			}
			if err := channelRepo.Create(ctx, ch); err != nil {
				t.Logf("Failed to create channel: %v", err)
				return false
			}

			mockSender := &mockNotificationSender{}
			senders := map[string]NotificationSender{"lark": mockSender}
			svc := NewAlertServiceWithSenders(alertRepo, channelRepo, senders)

			alert := model.Alert{
				Level:      level,
				Type:       alertType,
				Title:      "Test Alert",
				Content:    "Test content",
				TargetType: targetType,
				TargetID:   targetID,
			}

			// First send should succeed
			if err := svc.Send(ctx, alert); err != nil {
				t.Logf("First send failed: %v", err)
				return false
			}

			// Verify first send went through
			if len(mockSender.SendCalls) != 1 {
				t.Logf("Expected 1 send call after first send, got %d", len(mockSender.SendCalls))
				return false
			}

			// Get the saved alert and resolve it
			alerts, err := alertRepo.List(ctx, model.AlertFilter{})
			if err != nil {
				t.Logf("Failed to list alerts: %v", err)
				return false
			}
			if len(alerts) != 1 {
				t.Logf("Expected 1 alert, got %d", len(alerts))
				return false
			}

			// Resolve the alert
			if err := svc.MarkResolved(ctx, alerts[0].ID); err != nil {
				t.Logf("MarkResolved failed: %v", err)
				return false
			}

			// Send the same alert again - should NOT be suppressed now
			if err := svc.Send(ctx, alert); err != nil {
				t.Logf("Second send after resolve failed: %v", err)
				return false
			}

			// The sender should have been called again:
			// 1st call = original alert, 2nd call = recovery notification, 3rd call = new alert
			if len(mockSender.SendCalls) != 3 {
				t.Logf("Expected 3 send calls (alert + recovery + new alert), got %d", len(mockSender.SendCalls))
				return false
			}

			// Verify the third call is the new alert (not recovery)
			lastCall := mockSender.SendCalls[2]
			if lastCall.Title != "Test Alert" {
				t.Logf("Expected last send to be 'Test Alert', got %q", lastCall.Title)
				return false
			}

			return true
		},
		genAlertType(),
		genTargetType(),
		genTargetID(),
		genAlertLevel(),
	))

	// Property: Different alert types for the same target are NOT suppressed
	properties.Property("different alert types for same target are not suppressed", prop.ForAll(
		func(targetType string, targetID string, level string) bool {
			alertRepo, channelRepo := setupAlertPropertyTestDB(t)
			ctx := context.Background()

			// Create an enabled notification channel
			ch := &model.NotificationChannel{
				Type:       "lark",
				Name:       "Test Channel",
				ConfigJSON: `{"webhook_url":"https://example.com/hook"}`,
				Enabled:    true,
			}
			if err := channelRepo.Create(ctx, ch); err != nil {
				t.Logf("Failed to create channel: %v", err)
				return false
			}

			mockSender := &mockNotificationSender{}
			senders := map[string]NotificationSender{"lark": mockSender}
			svc := NewAlertServiceWithSenders(alertRepo, channelRepo, senders)

			// Send two different alert types for the same target
			alert1 := model.Alert{
				Level:      level,
				Type:       "cert_expiring",
				Title:      "Alert Type 1",
				Content:    "Content 1",
				TargetType: targetType,
				TargetID:   targetID,
			}
			alert2 := model.Alert{
				Level:      level,
				Type:       "deploy_failed",
				Title:      "Alert Type 2",
				Content:    "Content 2",
				TargetType: targetType,
				TargetID:   targetID,
			}

			if err := svc.Send(ctx, alert1); err != nil {
				t.Logf("First alert send failed: %v", err)
				return false
			}
			if err := svc.Send(ctx, alert2); err != nil {
				t.Logf("Second alert send failed: %v", err)
				return false
			}

			// Both alerts should have been sent (different types are not suppressed)
			if len(mockSender.SendCalls) != 2 {
				t.Logf("Expected 2 send calls for different alert types, got %d", len(mockSender.SendCalls))
				return false
			}

			return true
		},
		genTargetType(),
		genTargetID(),
		genAlertLevel(),
	))

	properties.TestingRun(t)
}
