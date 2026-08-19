package repository

import (
	"context"
	"testing"

	"github.com/ssl-manager/ssl-manager/internal/model"
)

// Throwaway smoke test to verify the ON CONFLICT(LOWER(RTRIM(name, '.'))) DO NOTHING
// expression-index conflict target actually parses and works against the
// project's SQLite driver (github.com/glebarez/sqlite). This file is deleted
// after verification.
func TestSmoke_CreateIfNotExists_ExpressionIndexConflictTarget(t *testing.T) {
	repo := setupDomainTestDB(t)
	ctx := context.Background()

	// Mirror the expression index from migrate.go exactly.
	db := repo.db
	if _, err := db.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS idx_domains_name_normalized ON domains(LOWER(RTRIM(name, '.')))`); err != nil {
		t.Fatalf("failed to create expression unique index: %v", err)
	}

	d1 := &model.Domain{Name: "Example.COM.", Source: "manual", MonitorPort: 443, MonitorEnabled: true}
	created, err := repo.CreateIfNotExists(ctx, d1)
	if err != nil {
		t.Fatalf("first CreateIfNotExists failed: %v", err)
	}
	if !created {
		t.Fatalf("expected first insert to be created=true")
	}

	// Different case/trailing dot but same normalized name -> should conflict.
	d2 := &model.Domain{Name: "example.com", Source: "cloudflare", MonitorPort: 8443, MonitorEnabled: false}
	created2, err := repo.CreateIfNotExists(ctx, d2)
	if err != nil {
		t.Fatalf("second CreateIfNotExists failed (this would indicate the ON CONFLICT expression target doesn't match the index): %v", err)
	}
	if created2 {
		t.Fatalf("expected second insert to be created=false due to normalized-name conflict")
	}

	// Verify the original row is untouched (not overwritten).
	got, err := repo.GetByID(ctx, d1.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.Source != "manual" {
		t.Fatalf("expected original Source 'manual' to be preserved, got '%s'", got.Source)
	}

	// A genuinely distinct normalized name should insert fine.
	d3 := &model.Domain{Name: "other.example.com", Source: "cloudflare", MonitorPort: 443, MonitorEnabled: true}
	created3, err := repo.CreateIfNotExists(ctx, d3)
	if err != nil {
		t.Fatalf("third CreateIfNotExists failed: %v", err)
	}
	if !created3 {
		t.Fatalf("expected third insert (distinct name) to be created=true")
	}
}
