package service

import (
	"context"
	"sync"
	"time"
)

// mockWhoisResult is a single orchestrated WHOIS outcome for mockWhoisClient:
// either a successful expiry (err == nil) or a failure (err != nil, typically
// one of the ErrWhois* sentinels defined in whois_client.go).
type mockWhoisResult struct {
	expiry time.Time
	err    error
}

// mockWhoisClient is a concurrency-safe test double for the WhoisClient
// interface. It is injected into DomainExpiryService via SetWhoisClient by the
// service property/unit tests (tasks 6.5–6.14) so those tests never make real
// network requests.
//
// Capabilities (see the design's Testing Strategy):
//   - Success: return a caller-supplied expiry date. The expiry is returned
//     verbatim (NOT normalized to UTC), so callers may supply times in arbitrary
//     zones to exercise the downstream UTC normalization (requirement 4.3).
//   - Failure: return any of ErrWhoisQuery / ErrWhoisRateLimit / ErrWhoisParse /
//     ErrWhoisNoExpiry (or any other error) per domain or as the default.
//   - Observability: record every queried registrable domain (both an ordered
//     log and per-domain counts) so RefreshAll-style tests can assert that
//     queries are made for, and only for, the enabled domains (requirement 6.1 /
//     6.6) and that per-item success/failure combinations behave correctly
//     (requirement 6.4).
//   - Per-domain orchestration: results keyed by registrable domain plus a
//     single default outcome, making it easy to compose success/failure mixes
//     for RefreshAll.
//
// This file defines ONLY the test double; the actual test cases live alongside
// tasks 6.5–6.14.
type mockWhoisClient struct {
	mu          sync.Mutex
	results     map[string]mockWhoisResult // per-domain orchestrated outcomes
	def         mockWhoisResult            // default when a domain is not in results
	queried     []string                   // ordered log of queried domains
	queryCounts map[string]int             // per-domain query counts
}

// compile-time assertion that *mockWhoisClient satisfies WhoisClient.
var _ WhoisClient = (*mockWhoisClient)(nil)

// newMockWhoisClient creates an empty mock whose default outcome is
// ErrWhoisNoExpiry. This makes an unconfigured domain deterministically fail
// (rather than yielding an accidental zero-time "success"); callers that want a
// permissive default can override it with setDefaultSuccess.
func newMockWhoisClient() *mockWhoisClient {
	return &mockWhoisClient{
		results:     make(map[string]mockWhoisResult),
		queryCounts: make(map[string]int),
		def:         mockWhoisResult{err: ErrWhoisNoExpiry},
	}
}

// setSuccess orchestrates a successful expiry for the given registrable domain.
// The expiry is returned verbatim (any time zone) to exercise UTC normalization
// downstream. Returns the receiver for fluent configuration.
func (m *mockWhoisClient) setSuccess(domain string, expiry time.Time) *mockWhoisClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.results[domain] = mockWhoisResult{expiry: expiry}
	return m
}

// setError orchestrates a failure for the given registrable domain (e.g.
// ErrWhoisQuery / ErrWhoisRateLimit / ErrWhoisParse / ErrWhoisNoExpiry). Returns
// the receiver for fluent configuration.
func (m *mockWhoisClient) setError(domain string, err error) *mockWhoisClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.results[domain] = mockWhoisResult{err: err}
	return m
}

// setResult orchestrates an arbitrary outcome for the given registrable domain.
// Returns the receiver for fluent configuration.
func (m *mockWhoisClient) setResult(domain string, res mockWhoisResult) *mockWhoisClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.results[domain] = res
	return m
}

// setDefaultSuccess sets the fallback outcome to a successful expiry for any
// domain not explicitly configured. Returns the receiver for fluent
// configuration.
func (m *mockWhoisClient) setDefaultSuccess(expiry time.Time) *mockWhoisClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.def = mockWhoisResult{expiry: expiry}
	return m
}

// setDefaultError sets the fallback outcome to a failure for any domain not
// explicitly configured. Returns the receiver for fluent configuration.
func (m *mockWhoisClient) setDefaultError(err error) *mockWhoisClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.def = mockWhoisResult{err: err}
	return m
}

// LookupExpiry implements WhoisClient. It records the queried domain (ordered
// log + per-domain count) and returns the orchestrated result: the per-domain
// outcome when configured, otherwise the default. The expiry is returned
// verbatim (not normalized to UTC). The ctx is accepted to satisfy the
// interface but is intentionally not consulted, so query recording is
// deterministic for the service tests.
func (m *mockWhoisClient) LookupExpiry(ctx context.Context, registrableDomain string) (time.Time, error) {
	m.mu.Lock()
	m.queried = append(m.queried, registrableDomain)
	m.queryCounts[registrableDomain]++
	res, ok := m.results[registrableDomain]
	if !ok {
		res = m.def
	}
	m.mu.Unlock()

	if res.err != nil {
		return time.Time{}, res.err
	}
	return res.expiry, nil
}

// queriedDomains returns a copy of the ordered log of queried domains.
func (m *mockWhoisClient) queriedDomains() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.queried))
	copy(out, m.queried)
	return out
}

// queryCount returns how many times the given registrable domain was queried.
func (m *mockWhoisClient) queryCount(domain string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.queryCounts[domain]
}

// wasQueried reports whether the given registrable domain was queried at least
// once.
func (m *mockWhoisClient) wasQueried(domain string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.queryCounts[domain] > 0
}

// totalQueries returns the total number of LookupExpiry calls recorded.
func (m *mockWhoisClient) totalQueries() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.queried)
}

// reset clears the recorded query log and counts while keeping the orchestrated
// results and default outcome intact.
func (m *mockWhoisClient) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queried = nil
	m.queryCounts = make(map[string]int)
}
