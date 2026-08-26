package repository

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/ssl-manager/ssl-manager/internal/model"
)

// Feature: ux-improvements-batch1, Property 3: Server-Side Sort Correctness
// **Validates: Requirements 5.1, 5.5**

func TestProperty_ServerSideSortCorrectness(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	// Generators
	sortByGen := gen.OneConstOf(
		"name", "source", "monitor_port", "tls_success",
		"domain_matched", "expire_at", "checked_at",
		"monitor_enabled", "alert_ignored",
	)
	sortOrderGen := gen.OneConstOf("asc", "desc")

	// Generate a list of 3-8 random domain names
	domainCountGen := gen.IntRange(3, 8)

	properties.Property("ListWithSort returns domains ordered by the requested sort column", prop.ForAll(
		func(sortBy string, sortOrder string, domainCount int) bool {
			repo := setupPropertyTestDB(t)
			ctx := context.Background()

			// Insert random domains with varied field values
			for i := 0; i < domainCount; i++ {
				domain := &model.Domain{
					ID:             uuid.New().String(),
					Name:           fmt.Sprintf("domain-%d-%s.example.com", i, uuid.New().String()[:4]),
					Source:         []string{"manual", "cloudflare", "certificate"}[i%3],
					MonitorPort:    443 + (i * 100) + (i%3)*10,
					MonitorEnabled: i%2 == 0,
					AlertIgnored:   i%4 == 0,
				}
				if err := repo.Create(ctx, domain); err != nil {
					t.Logf("Create domain failed: %v", err)
					return false
				}

				// Create monitor results for some domains to test sort by dmr fields
				if i%2 == 0 {
					expireAt := time.Now().UTC().Add(time.Duration(90-i*10) * 24 * time.Hour).Truncate(time.Second)
					daysRemaining := 90 - i*10
					result := &model.DomainMonitorResult{
						ID:            uuid.New().String(),
						DomainID:      domain.ID,
						CheckedPort:   domain.MonitorPort,
						TLSSuccess:    i%3 != 0,
						DomainMatched: i%2 == 0,
						ChainValid:    true,
						ExpireAt:      &expireAt,
						DaysRemaining: &daysRemaining,
						CheckedAt:     time.Now().UTC().Add(-time.Duration(i) * time.Hour).Truncate(time.Second),
					}
					if err := repo.SaveMonitorResult(ctx, result); err != nil {
						t.Logf("SaveMonitorResult failed: %v", err)
						return false
					}
				}
			}

			// Call ListWithSort
			params := model.DomainListParams{
				SortBy:    sortBy,
				SortOrder: sortOrder,
				Page:      1,
				PerPage:   100,
			}
			domains, total, err := repo.ListWithSort(ctx, params)
			if err != nil {
				t.Logf("ListWithSort failed: %v", err)
				return false
			}

			if total != domainCount {
				t.Logf("expected total %d, got %d", domainCount, total)
				return false
			}

			if len(domains) != domainCount {
				t.Logf("expected %d results, got %d", domainCount, len(domains))
				return false
			}

			// Verify ordering by comparing adjacent pairs
			return verifyOrdering(ctx, repo, domains, sortBy, sortOrder)
		},
		sortByGen,
		sortOrderGen,
		domainCountGen,
	))

	properties.TestingRun(t)
}

// setupPropertyTestDB creates an in-memory test DB suitable for property tests.
// Unlike setupDomainTestDB, it does NOT call t.Fatal on failure (returns error-safe setup).
func setupPropertyTestDB(t *testing.T) *DomainRepository {
	t.Helper()
	db := setupTestDB(t)

	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS domains (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		source TEXT DEFAULT 'manual' CHECK(source IN ('manual', 'certificate', 'cloudflare')),
		thirdpart_dns_id TEXT DEFAULT '',
		dns_record_id TEXT DEFAULT '',
		dns_record_type TEXT DEFAULT '',
		dns_record_value TEXT DEFAULT '',
		monitor_port INTEGER NOT NULL DEFAULT 443,
		linked_machine_id TEXT,
		linked_certificate_id TEXT,
		linked_machine_certificate_id TEXT,
		monitor_enabled INTEGER NOT NULL DEFAULT 1,
		alert_ignored INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatalf("failed to create domains table: %v", err)
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS domain_monitor_results (
		id TEXT PRIMARY KEY,
		domain_id TEXT NOT NULL REFERENCES domains(id),
		checked_port INTEGER NOT NULL,
		resolved_ips TEXT DEFAULT '',
		tls_success INTEGER NOT NULL DEFAULT 0,
		certificate_fingerprint_sha256 TEXT DEFAULT '',
		issuer TEXT DEFAULT '',
		expire_at TEXT,
		days_remaining INTEGER,
		domain_matched INTEGER NOT NULL DEFAULT 0,
		chain_valid INTEGER NOT NULL DEFAULT 0,
		error_message TEXT DEFAULT '',
		checked_at TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatalf("failed to create domain_monitor_results table: %v", err)
	}

	return NewDomainRepository(db)
}

// verifyOrdering checks that adjacent domain pairs satisfy the ordering constraint
// for the given sort_by column and sort_order direction.
func verifyOrdering(ctx context.Context, repo *DomainRepository, domains []*model.Domain, sortBy, sortOrder string) bool {
	if len(domains) <= 1 {
		return true
	}

	// For domain-only fields, we can check directly
	for i := 0; i < len(domains)-1; i++ {
		a := domains[i]
		b := domains[i+1]
		cmp := compareDomainsByField(ctx, repo, a, b, sortBy)
		if sortOrder == "desc" {
			// In descending order, each element should be >= the next
			if cmp < 0 {
				return false
			}
		} else {
			// In ascending order, each element should be <= the next
			if cmp > 0 {
				return false
			}
		}
	}
	return true
}

// compareDomainsByField compares two domains by the given sort_by field.
// Returns -1, 0, or 1 like a standard comparator.
// For fields from domain_monitor_results, queries the DB for the latest result.
func compareDomainsByField(ctx context.Context, repo *DomainRepository, a, b *model.Domain, sortBy string) int {
	switch sortBy {
	case "name":
		aName := strings.ToLower(strings.TrimRight(a.Name, "."))
		bName := strings.ToLower(strings.TrimRight(b.Name, "."))
		return strings.Compare(aName, bName)
	case "source":
		return strings.Compare(a.Source, b.Source)
	case "monitor_port":
		return compareInt(a.MonitorPort, b.MonitorPort)
	case "monitor_enabled":
		return compareInt(boolToInt(a.MonitorEnabled), boolToInt(b.MonitorEnabled))
	case "alert_ignored":
		return compareInt(boolToInt(a.AlertIgnored), boolToInt(b.AlertIgnored))
	case "tls_success":
		aVal := getDmrIntField(ctx, repo, a.ID, "tls_success")
		bVal := getDmrIntField(ctx, repo, b.ID, "tls_success")
		return compareInt(aVal, bVal)
	case "domain_matched":
		aVal := getDmrIntField(ctx, repo, a.ID, "domain_matched")
		bVal := getDmrIntField(ctx, repo, b.ID, "domain_matched")
		return compareInt(aVal, bVal)
	case "expire_at":
		aVal := getDmrTimeField(ctx, repo, a.ID, "expire_at")
		bVal := getDmrTimeField(ctx, repo, b.ID, "expire_at")
		return compareInt64(aVal, bVal)
	case "checked_at":
		aVal := getDmrTimeField(ctx, repo, a.ID, "checked_at")
		bVal := getDmrTimeField(ctx, repo, b.ID, "checked_at")
		return compareInt64(aVal, bVal)
	default:
		return 0
	}
}

// getDmrIntField gets a COALESCE'd int field from the latest domain_monitor_results for a domain.
// Returns -1 if no result exists (matches the COALESCE(..., -1) in sortByWhitelist).
func getDmrIntField(ctx context.Context, repo *DomainRepository, domainID, field string) int {
	result, err := repo.GetLatestMonitorResult(ctx, domainID)
	if err != nil {
		return -1 // No result → COALESCE default
	}
	switch field {
	case "tls_success":
		return boolToInt(result.TLSSuccess)
	case "domain_matched":
		return boolToInt(result.DomainMatched)
	default:
		return -1
	}
}

// getDmrTimeField gets a COALESCE'd time field (as Unix seconds) from the latest dmr.
// Returns 0 if no result exists or the field is nil (matches COALESCE(..., 0) in sortByWhitelist).
func getDmrTimeField(ctx context.Context, repo *DomainRepository, domainID, field string) int64 {
	result, err := repo.GetLatestMonitorResult(ctx, domainID)
	if err != nil {
		return 0
	}
	switch field {
	case "expire_at":
		if result.ExpireAt == nil {
			return 0
		}
		return result.ExpireAt.Unix()
	case "checked_at":
		return result.CheckedAt.Unix()
	default:
		return 0
	}
}

func compareInt(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func compareInt64(a, b int64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// Feature: ux-improvements-batch1, Property 5: Default Multi-Level Sort Comparator
// **Validates: Requirements 5.7, 5.8**

func TestProperty_DefaultMultiLevelSortComparator(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	// Generator for the number of domains per group (1-3 each)
	countPerGroupGen := gen.IntRange(1, 3)

	properties.Property("ListWithSort with no sort_by returns domains in correct default multi-level order", prop.ForAll(
		func(anomalyCount, normalCount, disabledCount, ignoredCount int) bool {
			repo := setupPropertyTestDB(t)
			ctx := context.Background()

			type domainInfo struct {
				domain *model.Domain
				group  int // 0=anomaly, 1=normal, 2=disabled, 3=ignored
			}

			var allDomains []domainInfo

			// Group 0: Anomaly domains (various anomaly types)
			anomalyTypes := []string{"expired", "tls_fail", "unchecked"}
			for i := 0; i < anomalyCount; i++ {
				anomalyType := anomalyTypes[i%len(anomalyTypes)]
				domain := &model.Domain{
					ID:             uuid.New().String(),
					Name:           fmt.Sprintf("anomaly-%d-%s.example.com", i, anomalyType),
					Source:         "manual",
					MonitorPort:    443,
					MonitorEnabled: true,
					AlertIgnored:   false,
				}
				if err := repo.Create(ctx, domain); err != nil {
					t.Logf("Create anomaly domain failed: %v", err)
					return false
				}

				// Create monitor results based on anomaly type
				switch anomalyType {
				case "expired":
					expireAt := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Second)
					daysRemaining := -2
					result := &model.DomainMonitorResult{
						ID:            uuid.New().String(),
						DomainID:      domain.ID,
						CheckedPort:   443,
						TLSSuccess:    true,
						DomainMatched: true,
						ChainValid:    true,
						ExpireAt:      &expireAt,
						DaysRemaining: &daysRemaining,
						CheckedAt:     time.Now().UTC().Truncate(time.Second),
					}
					if err := repo.SaveMonitorResult(ctx, result); err != nil {
						t.Logf("SaveMonitorResult failed: %v", err)
						return false
					}
				case "tls_fail":
					result := &model.DomainMonitorResult{
						ID:            uuid.New().String(),
						DomainID:      domain.ID,
						CheckedPort:   443,
						TLSSuccess:    false,
						DomainMatched: false,
						ChainValid:    false,
						ErrorMessage:  "connection refused",
						CheckedAt:     time.Now().UTC().Truncate(time.Second),
					}
					if err := repo.SaveMonitorResult(ctx, result); err != nil {
						t.Logf("SaveMonitorResult failed: %v", err)
						return false
					}
				case "unchecked":
					// No monitor result → unchecked → anomaly group
				}

				allDomains = append(allDomains, domainInfo{domain: domain, group: 0})
			}

			// Group 1: Normal domains (TLS success, domain matched, not expired, not expiring_30d)
			for i := 0; i < normalCount; i++ {
				domain := &model.Domain{
					ID:             uuid.New().String(),
					Name:           fmt.Sprintf("normal-%d.example.com", i),
					Source:         "manual",
					MonitorPort:    443,
					MonitorEnabled: true,
					AlertIgnored:   false,
				}
				if err := repo.Create(ctx, domain); err != nil {
					t.Logf("Create normal domain failed: %v", err)
					return false
				}

				// Create a healthy monitor result (expire 90+ days out)
				expireAt := time.Now().UTC().Add(time.Duration(90+i*30) * 24 * time.Hour).Truncate(time.Second)
				daysRemaining := 90 + i*30
				result := &model.DomainMonitorResult{
					ID:            uuid.New().String(),
					DomainID:      domain.ID,
					CheckedPort:   443,
					TLSSuccess:    true,
					DomainMatched: true,
					ChainValid:    true,
					ExpireAt:      &expireAt,
					DaysRemaining: &daysRemaining,
					CheckedAt:     time.Now().UTC().Truncate(time.Second),
				}
				if err := repo.SaveMonitorResult(ctx, result); err != nil {
					t.Logf("SaveMonitorResult failed: %v", err)
					return false
				}

				allDomains = append(allDomains, domainInfo{domain: domain, group: 1})
			}

			// Group 2: Disabled domains (monitor_enabled=false, not ignored)
			for i := 0; i < disabledCount; i++ {
				domain := &model.Domain{
					ID:             uuid.New().String(),
					Name:           fmt.Sprintf("disabled-%d.example.com", i),
					Source:         "manual",
					MonitorPort:    443,
					MonitorEnabled: false,
					AlertIgnored:   false,
				}
				if err := repo.Create(ctx, domain); err != nil {
					t.Logf("Create disabled domain failed: %v", err)
					return false
				}
				allDomains = append(allDomains, domainInfo{domain: domain, group: 2})
			}

			// Group 3: Ignored domains (alert_ignored=true)
			for i := 0; i < ignoredCount; i++ {
				domain := &model.Domain{
					ID:             uuid.New().String(),
					Name:           fmt.Sprintf("ignored-%d.example.com", i),
					Source:         "manual",
					MonitorPort:    443,
					MonitorEnabled: true,
					AlertIgnored:   true,
				}
				if err := repo.Create(ctx, domain); err != nil {
					t.Logf("Create ignored domain failed: %v", err)
					return false
				}
				allDomains = append(allDomains, domainInfo{domain: domain, group: 3})
			}

			// Call ListWithSort with NO sort_by → triggers default sort
			params := model.DomainListParams{
				Page:    1,
				PerPage: 100,
			}
			domains, total, err := repo.ListWithSort(ctx, params)
			if err != nil {
				t.Logf("ListWithSort failed: %v", err)
				return false
			}

			expectedTotal := anomalyCount + normalCount + disabledCount + ignoredCount
			if total != expectedTotal {
				t.Logf("expected total %d, got %d", expectedTotal, total)
				return false
			}
			if len(domains) != expectedTotal {
				t.Logf("expected %d results, got %d", expectedTotal, len(domains))
				return false
			}

			// Build a map from domain ID to its expected group
			groupMap := make(map[string]int)
			for _, di := range allDomains {
				groupMap[di.domain.ID] = di.group
			}

			// Verify adjacent pairs: group ordering is correct (0 ≤ 1 ≤ 2 ≤ 3)
			for i := 0; i < len(domains)-1; i++ {
				aGroup := groupMap[domains[i].ID]
				bGroup := groupMap[domains[i+1].ID]
				if aGroup > bGroup {
					t.Logf("group ordering violated at position %d: domain %s (group %d) before domain %s (group %d)",
						i, domains[i].Name, aGroup, domains[i+1].Name, bGroup)
					return false
				}
			}

			// Verify within group 1 (normal): sorted by expire_at ASC
			var normalDomainIDs []string
			for _, d := range domains {
				if groupMap[d.ID] == 1 {
					normalDomainIDs = append(normalDomainIDs, d.ID)
				}
			}
			if len(normalDomainIDs) > 1 {
				for i := 0; i < len(normalDomainIDs)-1; i++ {
					aExpire := getDmrTimeField(ctx, repo, normalDomainIDs[i], "expire_at")
					bExpire := getDmrTimeField(ctx, repo, normalDomainIDs[i+1], "expire_at")
					if aExpire > bExpire {
						t.Logf("normal group expire_at ordering violated: domain at pos %d (expire=%d) > domain at pos %d (expire=%d)",
							i, aExpire, i+1, bExpire)
						return false
					}
				}
			}

			// Verify within same group and same sub-priority: name ordering (lowercase, trimmed trailing dot)
			// Check within group 2 (disabled) and group 3 (ignored) where sub-sort is just by name
			for _, targetGroup := range []int{2, 3} {
				var groupDomains []*model.Domain
				for _, d := range domains {
					if groupMap[d.ID] == targetGroup {
						groupDomains = append(groupDomains, d)
					}
				}
				if len(groupDomains) > 1 {
					for i := 0; i < len(groupDomains)-1; i++ {
						aName := strings.ToLower(strings.TrimRight(groupDomains[i].Name, "."))
						bName := strings.ToLower(strings.TrimRight(groupDomains[i+1].Name, "."))
						if aName > bName {
							t.Logf("group %d name ordering violated: %s > %s", targetGroup, aName, bName)
							return false
						}
					}
				}
			}

			return true
		},
		countPerGroupGen, // anomalyCount
		countPerGroupGen, // normalCount
		countPerGroupGen, // disabledCount
		countPerGroupGen, // ignoredCount
	))

	properties.TestingRun(t)
}

// Feature: ux-improvements-batch1, Property 4: Server-Side Filter Correctness
// **Validates: Requirements 5.2, 5.5**

func TestProperty_ServerSideFilterCorrectness(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	// Valid filter_status values
	filterStatusGen := gen.OneConstOf(
		"enabled", "disabled", "ignored",
		"tls_ok", "tls_error", "unchecked",
		"matched", "unmatched",
		"expiring_30d", "expired",
	)

	domainCountGen := gen.IntRange(5, 12)

	properties.Property("ListWithSort with filter_status returns only domains satisfying the filter predicate", prop.ForAll(
		func(filterStatus string, domainCount int) bool {
			repo := setupPropertyTestDB(t)
			ctx := context.Background()

			// Insert random domains with varied states
			for i := 0; i < domainCount; i++ {
				domain := &model.Domain{
					ID:             uuid.New().String(),
					Name:           fmt.Sprintf("filter-%d-%s.example.com", i, uuid.New().String()[:4]),
					Source:         []string{"manual", "cloudflare", "certificate"}[i%3],
					MonitorPort:    443 + (i%5)*100,
					MonitorEnabled: i%3 != 1, // ~2/3 enabled, 1/3 disabled
					AlertIgnored:   i%5 == 0, // ~1/5 ignored
				}
				if err := repo.Create(ctx, domain); err != nil {
					t.Logf("Create domain failed: %v", err)
					return false
				}

				// Create varied monitor results for some domains
				switch {
				case i%4 == 0:
					// TLS success, domain matched, valid expiry (far future)
					expireAt := time.Now().UTC().Add(90 * 24 * time.Hour).Truncate(time.Second)
					days := 90
					result := &model.DomainMonitorResult{
						ID:            uuid.New().String(),
						DomainID:      domain.ID,
						CheckedPort:   domain.MonitorPort,
						TLSSuccess:    true,
						DomainMatched: true,
						ChainValid:    true,
						ExpireAt:      &expireAt,
						DaysRemaining: &days,
						CheckedAt:     time.Now().UTC().Truncate(time.Second),
					}
					if err := repo.SaveMonitorResult(ctx, result); err != nil {
						t.Logf("SaveMonitorResult failed: %v", err)
						return false
					}
				case i%4 == 1:
					// TLS error (connection failed)
					result := &model.DomainMonitorResult{
						ID:            uuid.New().String(),
						DomainID:      domain.ID,
						CheckedPort:   domain.MonitorPort,
						TLSSuccess:    false,
						DomainMatched: false,
						ChainValid:    false,
						ErrorMessage:  "connection refused",
						CheckedAt:     time.Now().UTC().Truncate(time.Second),
					}
					if err := repo.SaveMonitorResult(ctx, result); err != nil {
						t.Logf("SaveMonitorResult failed: %v", err)
						return false
					}
				case i%4 == 2:
					// TLS success but domain unmatched, expiring within 30 days
					expireAt := time.Now().UTC().Add(15 * 24 * time.Hour).Truncate(time.Second)
					days := 15
					result := &model.DomainMonitorResult{
						ID:            uuid.New().String(),
						DomainID:      domain.ID,
						CheckedPort:   domain.MonitorPort,
						TLSSuccess:    true,
						DomainMatched: false,
						ChainValid:    true,
						ExpireAt:      &expireAt,
						DaysRemaining: &days,
						CheckedAt:     time.Now().UTC().Truncate(time.Second),
					}
					if err := repo.SaveMonitorResult(ctx, result); err != nil {
						t.Logf("SaveMonitorResult failed: %v", err)
						return false
					}
				case i%4 == 3:
					// No monitor result → "unchecked"
					// (intentionally leave no dmr record)
				}
			}

			// Also insert some expired domains to cover the "expired" filter
			for j := 0; j < 2; j++ {
				domain := &model.Domain{
					ID:             uuid.New().String(),
					Name:           fmt.Sprintf("expired-%d-%s.example.com", j, uuid.New().String()[:4]),
					Source:         "manual",
					MonitorPort:    443,
					MonitorEnabled: true,
					AlertIgnored:   false,
				}
				if err := repo.Create(ctx, domain); err != nil {
					t.Logf("Create expired domain failed: %v", err)
					return false
				}
				expireAt := time.Now().UTC().Add(-10 * 24 * time.Hour).Truncate(time.Second)
				days := -10
				result := &model.DomainMonitorResult{
					ID:            uuid.New().String(),
					DomainID:      domain.ID,
					CheckedPort:   domain.MonitorPort,
					TLSSuccess:    true,
					DomainMatched: true,
					ChainValid:    true,
					ExpireAt:      &expireAt,
					DaysRemaining: &days,
					CheckedAt:     time.Now().UTC().Truncate(time.Second),
				}
				if err := repo.SaveMonitorResult(ctx, result); err != nil {
					t.Logf("SaveMonitorResult (expired) failed: %v", err)
					return false
				}
			}

			// Call ListWithSort with the random filter
			params := model.DomainListParams{
				FilterStatus: filterStatus,
				Page:         1,
				PerPage:      100,
			}
			domains, _, err := repo.ListWithSort(ctx, params)
			if err != nil {
				t.Logf("ListWithSort failed for filter=%s: %v", filterStatus, err)
				return false
			}

			// Verify each returned domain satisfies the filter predicate
			for _, d := range domains {
				if !verifyFilterPredicate(ctx, repo, d, filterStatus) {
					t.Logf("Domain %s (id=%s) does not satisfy filter predicate %q",
						d.Name, d.ID, filterStatus)
					return false
				}
			}

			return true
		},
		filterStatusGen,
		domainCountGen,
	))

	properties.TestingRun(t)
}

// verifyFilterPredicate checks whether a domain satisfies the given filter_status predicate.
// This mirrors the SQL predicates defined in filterStatusPredicates at the Go level.
func verifyFilterPredicate(ctx context.Context, repo *DomainRepository, d *model.Domain, filterStatus string) bool {
	switch filterStatus {
	case "enabled":
		return d.MonitorEnabled
	case "disabled":
		return !d.MonitorEnabled
	case "ignored":
		return d.AlertIgnored
	case "tls_ok":
		dmr, err := repo.GetLatestMonitorResult(ctx, d.ID)
		if err != nil {
			return false // no result means predicate not satisfied
		}
		return dmr.TLSSuccess
	case "tls_error":
		dmr, err := repo.GetLatestMonitorResult(ctx, d.ID)
		if err != nil {
			return false
		}
		return !dmr.TLSSuccess
	case "unchecked":
		_, err := repo.GetLatestMonitorResult(ctx, d.ID)
		return err != nil // no result means "unchecked"
	case "matched":
		dmr, err := repo.GetLatestMonitorResult(ctx, d.ID)
		if err != nil {
			return false
		}
		return dmr.DomainMatched
	case "unmatched":
		dmr, err := repo.GetLatestMonitorResult(ctx, d.ID)
		if err != nil {
			return false
		}
		return !dmr.DomainMatched
	case "expiring_30d":
		dmr, err := repo.GetLatestMonitorResult(ctx, d.ID)
		if err != nil || dmr.ExpireAt == nil {
			return false
		}
		now := time.Now()
		return dmr.ExpireAt.After(now) && dmr.ExpireAt.Before(now.Add(30*24*time.Hour))
	case "expired":
		dmr, err := repo.GetLatestMonitorResult(ctx, d.ID)
		if err != nil || dmr.ExpireAt == nil {
			return false
		}
		return !dmr.ExpireAt.After(time.Now())
	default:
		return true
	}
}

// Feature: ux-improvements-batch1, Property 6: alert_ignored Round-Trip Persistence
// **Validates: Requirements 6.1, 6.2, 6.3, 6.4**

func TestProperty_AlertIgnoredRoundTripPersistence(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("Creating a domain with alert_ignored preserves the value on read", prop.ForAll(
		func(alertIgnored bool) bool {
			repo := setupPropertyTestDB(t)
			ctx := context.Background()

			domain := &model.Domain{
				ID:             uuid.New().String(),
				Name:           fmt.Sprintf("test-%s.example.com", uuid.New().String()[:8]),
				Source:         "manual",
				MonitorPort:    443,
				MonitorEnabled: true,
				AlertIgnored:   alertIgnored,
			}

			if err := repo.Create(ctx, domain); err != nil {
				t.Logf("Create failed: %v", err)
				return false
			}

			got, err := repo.GetByID(ctx, domain.ID)
			if err != nil {
				t.Logf("GetByID failed: %v", err)
				return false
			}

			if got.AlertIgnored != alertIgnored {
				t.Logf("Create round-trip: expected AlertIgnored=%v, got %v", alertIgnored, got.AlertIgnored)
				return false
			}

			return true
		},
		gen.Bool(),
	))

	properties.Property("Updating alert_ignored preserves the new value on read", prop.ForAll(
		func(initialValue bool, updatedValue bool) bool {
			repo := setupPropertyTestDB(t)
			ctx := context.Background()

			// Create domain with initial alert_ignored value
			domain := &model.Domain{
				ID:             uuid.New().String(),
				Name:           fmt.Sprintf("test-%s.example.com", uuid.New().String()[:8]),
				Source:         "manual",
				MonitorPort:    443,
				MonitorEnabled: true,
				AlertIgnored:   initialValue,
			}

			if err := repo.Create(ctx, domain); err != nil {
				t.Logf("Create failed: %v", err)
				return false
			}

			// Update alert_ignored to the new value
			updates := map[string]interface{}{
				"alert_ignored": updatedValue,
			}
			if err := repo.Update(ctx, domain.ID, updates); err != nil {
				t.Logf("Update failed: %v", err)
				return false
			}

			// Read back and verify
			got, err := repo.GetByID(ctx, domain.ID)
			if err != nil {
				t.Logf("GetByID after update failed: %v", err)
				return false
			}

			if got.AlertIgnored != updatedValue {
				t.Logf("Update round-trip: expected AlertIgnored=%v, got %v", updatedValue, got.AlertIgnored)
				return false
			}

			return true
		},
		gen.Bool(),
		gen.Bool(),
	))

	properties.TestingRun(t)
}
