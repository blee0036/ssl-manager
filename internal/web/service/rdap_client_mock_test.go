package service

import (
	"context"
	"sync"
	"time"
)

// mockRDAPResult is a single orchestrated RDAP outcome for mockRDAPClient: either
// a successful expiry (err == nil) or a failure (err != nil, typically one of the
// ErrRDAP* sentinels defined in rdap_client.go).
type mockRDAPResult struct {
	expiry time.Time
	err    error
}

// mockRDAPClient is a concurrency-safe test double for the RDAPClient interface.
// It is injected into DomainExpiryService via SetRDAPClient by the service
// property/unit tests so those tests never make real network requests. It mirrors
// mockWhoisClient (whois_client_mock_test.go) exactly.
//
// CRITICAL DEFAULT: the default outcome is a FAILURE (ErrRDAPNoServer). This is
// required so the existing WHOIS-failure property tests — Property 7 ("refresh
// failure preserves expiry") and Property 11 (RefreshAll failures), which force
// WHOIS to fail — see BOTH the WHOIS primary AND the RDAP fallback fail, and thus
// still record last_status="failed" with a non-empty error exactly as before this
// fallback existed, WITHOUT any real network call. A permissive default would
// silently "rescue" those forced failures and break the properties.
//
// Capabilities (mirroring mockWhoisClient):
//   - Success: return a caller-supplied expiry date, verbatim (NOT normalized to
//     UTC), so callers may supply times in arbitrary zones to exercise downstream
//     UTC normalization.
//   - Failure: return any of the ErrRDAP* sentinels (or any other error) per
//     domain or as the default.
//   - Observability: record every queried registrable domain (ordered log +
//     per-domain counts) so tests can assert the fallback was (or was NOT)
//     consulted.
//   - Per-domain orchestration: results keyed by registrable domain plus a single
//     default outcome.
type mockRDAPClient struct {
	mu          sync.Mutex
	results     map[string]mockRDAPResult // per-domain orchestrated outcomes
	def         mockRDAPResult            // default when a domain is not in results
	queried     []string                  // ordered log of queried domains
	queryCounts map[string]int            // per-domain query counts
}

// compile-time assertion that *mockRDAPClient satisfies RDAPClient.
var _ RDAPClient = (*mockRDAPClient)(nil)

// newMockRDAPClient creates an empty mock whose default outcome is a FAILURE
// (ErrRDAPNoServer). See the type doc for why the default MUST fail.
func newMockRDAPClient() *mockRDAPClient {
	return &mockRDAPClient{
		results:     make(map[string]mockRDAPResult),
		queryCounts: make(map[string]int),
		def:         mockRDAPResult{err: ErrRDAPNoServer},
	}
}

// setSuccess orchestrates a successful expiry for the given registrable domain.
// The expiry is returned verbatim (any time zone) to exercise UTC normalization
// downstream. Returns the receiver for fluent configuration.
func (m *mockRDAPClient) setSuccess(domain string, expiry time.Time) *mockRDAPClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.results[domain] = mockRDAPResult{expiry: expiry}
	return m
}

// setError orchestrates a failure for the given registrable domain (e.g.
// ErrRDAPNoServer / ErrRDAPQuery / ErrRDAPNotFound / ErrRDAPParse /
// ErrRDAPNoExpiry). Returns the receiver for fluent configuration.
func (m *mockRDAPClient) setError(domain string, err error) *mockRDAPClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.results[domain] = mockRDAPResult{err: err}
	return m
}

// setResult orchestrates an arbitrary outcome for the given registrable domain.
// Returns the receiver for fluent configuration.
func (m *mockRDAPClient) setResult(domain string, res mockRDAPResult) *mockRDAPClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.results[domain] = res
	return m
}

// setDefaultSuccess sets the fallback outcome to a successful expiry for any
// domain not explicitly configured. Returns the receiver for fluent
// configuration.
func (m *mockRDAPClient) setDefaultSuccess(expiry time.Time) *mockRDAPClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.def = mockRDAPResult{expiry: expiry}
	return m
}

// setDefaultError sets the fallback outcome to a failure for any domain not
// explicitly configured. Returns the receiver for fluent configuration.
func (m *mockRDAPClient) setDefaultError(err error) *mockRDAPClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.def = mockRDAPResult{err: err}
	return m
}

// LookupExpiry implements RDAPClient. It records the queried domain (ordered log
// + per-domain count) and returns the orchestrated result: the per-domain outcome
// when configured, otherwise the default. The expiry is returned verbatim (not
// normalized to UTC). The ctx is accepted to satisfy the interface but is
// intentionally not consulted, so query recording is deterministic.
func (m *mockRDAPClient) LookupExpiry(ctx context.Context, registrableDomain string) (time.Time, error) {
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
func (m *mockRDAPClient) queriedDomains() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.queried))
	copy(out, m.queried)
	return out
}

// queryCount returns how many times the given registrable domain was queried.
func (m *mockRDAPClient) queryCount(domain string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.queryCounts[domain]
}

// wasQueried reports whether the given registrable domain was queried at least
// once.
func (m *mockRDAPClient) wasQueried(domain string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.queryCounts[domain] > 0
}

// totalQueries returns the total number of LookupExpiry calls recorded.
func (m *mockRDAPClient) totalQueries() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.queried)
}

// reset clears the recorded query log and counts while keeping the orchestrated
// results and default outcome intact.
func (m *mockRDAPClient) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queried = nil
	m.queryCounts = make(map[string]int)
}
