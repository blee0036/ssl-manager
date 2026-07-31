package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/ssl-manager/ssl-manager/internal/config"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
	"github.com/ssl-manager/ssl-manager/internal/web/service"

	_ "github.com/glebarez/sqlite"
)

// Feature: domain-expiry-monitor, Property 13: 列表响应包含必需字段
//
// **Validates: Requirements 8.1**
//
// This file is self-contained and uses the rdprop* prefix on every top-level
// identifier so it never collides with the sibling root_domain_handler_test.go
// (task 7.3) that lives in the same package.

// rdpropLastStatuses enumerates the allowed last_status values (index -> value).
// "" (never checked), "success", "failed".
var rdpropLastStatuses = []string{"", "success", "failed"}

// rdpropEntry is a generated specification for one seeded root domain. It varies
// every field that could plausibly influence JSON serialization of a list item —
// crucially whether expiry_date / last_checked_at are known (so their JSON value
// is a timestamp) or unknown (so their JSON value is null). days_remaining is
// computed by the service and is null exactly when expiry_date is null.
type rdpropEntry struct {
	SourceCloudflare bool
	ExpiryKnown      bool
	ExpiryOffsetDays int // days from now (may be negative = already expired)
	LastCheckedKnown bool
	StatusIdx        int // index into rdpropLastStatuses
	MonitorEnabled   bool
	AlertIgnored     bool
}

// rdpropScenario is a full generated scenario: a set of root domains to seed.
type rdpropScenario struct {
	Entries []rdpropEntry
}

// rdpropGenScenario generates a scenario of 1..25 root domains with randomized
// attributes. At least one entry is always produced so the property is never
// vacuous; the count stays <= 25 so every seeded domain fits on a single
// per_page=100 page and is therefore actually inspected.
func rdpropGenScenario() gopter.Gen {
	return gopter.CombineGens(
		gen.IntRange(1, 25), // numDomains (>=1 so the property is non-vacuous)
		gen.Int64(),         // random seed for sub-generation
	).Map(func(values []interface{}) rdpropScenario {
		numDomains := values[0].(int)
		seed := values[1].(int64)
		rng := rand.New(rand.NewSource(seed))

		scenario := rdpropScenario{}
		for i := 0; i < numDomains; i++ {
			scenario.Entries = append(scenario.Entries, rdpropEntry{
				SourceCloudflare: rng.Intn(2) == 1,
				ExpiryKnown:      rng.Intn(2) == 1,
				ExpiryOffsetDays: rng.Intn(521) - 120, // -120 .. +400 days
				LastCheckedKnown: rng.Intn(2) == 1,
				StatusIdx:        rng.Intn(len(rdpropLastStatuses)),
				MonitorEnabled:   rng.Intn(2) == 1,
				AlertIgnored:     rng.Intn(2) == 1,
			})
		}
		return scenario
	})
}

// rdpropSetupDB opens a fresh in-memory SQLite DB, creates the root_domains table
// (mirroring internal/database/migrate.go, including the UNIQUE index on
// registrable_domain), and seeds it with the scenario's root domains via the real
// RootDomainRepository.Create path (no WHOIS is involved). Each entry gets a
// unique registrable domain (example{i}.com) so the UNIQUE index is satisfied.
func rdpropSetupDB(scenario rdpropScenario) (*sql.DB, error) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if _, err := db.Exec(`CREATE TABLE root_domains (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		source TEXT NOT NULL DEFAULT 'manual' CHECK(source IN ('manual', 'cloudflare')),
		registrable_domain TEXT NOT NULL,
		expiry_date TEXT,
		last_checked_at TEXT,
		last_status TEXT NOT NULL DEFAULT '',
		last_error TEXT NOT NULL DEFAULT '',
		monitor_enabled INTEGER NOT NULL DEFAULT 1,
		alert_ignored INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create root_domains table: %w", err)
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX idx_root_domains_registrable ON root_domains(registrable_domain)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create unique index: %w", err)
	}

	repo := repository.NewRootDomainRepository(db)
	ctx := context.Background()
	base := time.Now().UTC()

	for i, e := range scenario.Entries {
		reg := fmt.Sprintf("example%d.com", i) // unique -> satisfies the UNIQUE index
		source := "manual"
		if e.SourceCloudflare {
			source = "cloudflare"
		}
		rd := &model.RootDomain{
			Name:              reg,
			Source:            source,
			RegistrableDomain: reg,
			LastStatus:        rdpropLastStatuses[e.StatusIdx],
			MonitorEnabled:    e.MonitorEnabled,
			AlertIgnored:      e.AlertIgnored,
		}
		if e.ExpiryKnown {
			exp := base.AddDate(0, 0, e.ExpiryOffsetDays)
			rd.ExpiryDate = &exp
		}
		if e.LastCheckedKnown {
			lc := base.AddDate(0, 0, -1)
			rd.LastCheckedAt = &lc
		}
		if err := repo.Create(ctx, rd); err != nil {
			db.Close()
			return nil, fmt.Errorf("seed create %d: %w", i, err)
		}
	}

	return db, nil
}

// TestProperty_RootDomainListResponseRequiredFields verifies Property 13: for any
// set of root domains, every item in the LIST response contains the required
// fields — name, source, expiry_date, days_remaining, last_checked_at and
// last_status — even when expiry_date / days_remaining / last_checked_at are null
// (the unknown state), since those json tags are not omitempty and are always
// emitted.
//
// Presence is checked with the comma-ok form on a map[string]interface{}: a JSON
// null value still creates the map key (with a nil value), so ok==true means the
// key is present, while a genuinely absent key yields ok==false. This
// distinguishes "present but null" from "missing", which is exactly the property.
//
// The List handler is exercised end-to-end through a chi router (no auth
// middleware, matching the existing handler tests) over a real
// DomainExpiryService backed by an in-memory SQLite DB. zoneScanner and alerter
// are nil because the List path uses neither, and the DNSConfigResolver is nil
// because config_id import is irrelevant here.
//
// **Validates: Requirements 8.1**
func TestProperty_RootDomainListResponseRequiredFields(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	properties.Property("list response items always contain the required fields", prop.ForAll(
		func(scenario rdpropScenario) bool {
			db, err := rdpropSetupDB(scenario)
			if err != nil {
				t.Logf("failed to setup db: %v", err)
				return false
			}
			defer db.Close()

			repo := repository.NewRootDomainRepository(db)
			// nil zoneScanner + nil alerter: the List path uses neither.
			svc := service.NewDomainExpiryService(repo, nil, nil, config.NewRuntimeConfig(config.DefaultConfig()))
			// nil DNSConfigResolver: config_id import is unused on the List path.
			h := NewRootDomainHandler(svc, nil)

			r := chi.NewRouter()
			r.Route("/api/root-domains", func(r chi.Router) {
				r.Get("/", h.List)
			})

			// per_page=100 ensures all seeded domains (<=25) fit on one page, so
			// every seeded item is actually returned and inspected.
			req := httptest.NewRequest(http.MethodGet, "/api/root-domains?per_page=100", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Logf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
				return false
			}

			// Decode data.items as generic maps so a present-but-null field is
			// still observed as an existing key (json null -> map entry).
			var resp struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
				Data    struct {
					Items   []map[string]interface{} `json:"items"`
					Total   int                      `json:"total"`
					Page    int                      `json:"page"`
					PerPage int                      `json:"per_page"`
				} `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Logf("failed to unmarshal response: %v; body: %s", err, w.Body.String())
				return false
			}

			// Sanity: every seeded domain is returned, so the required-field check
			// below actually covers all of them.
			if resp.Data.Total != len(scenario.Entries) || len(resp.Data.Items) != len(scenario.Entries) {
				t.Logf("count mismatch: total=%d items=%d want=%d",
					resp.Data.Total, len(resp.Data.Items), len(scenario.Entries))
				return false
			}

			requiredKeys := []string{
				"name",
				"source",
				"expiry_date",
				"days_remaining",
				"last_checked_at",
				"last_status",
			}
			for idx, item := range resp.Data.Items {
				for _, key := range requiredKeys {
					if _, ok := item[key]; !ok {
						t.Logf("item %d missing required key %q; item=%v", idx, key, item)
						return false
					}
				}
			}
			return true
		},
		rdpropGenScenario(),
	))

	properties.TestingRun(t)
}
