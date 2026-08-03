package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RDAP (Registration Data Access Protocol, RFC 9082/9083) is the ICANN-mandated
// structured-JSON successor to WHOIS. Newer gTLD registries (e.g. .app / .dev,
// operated by Google Registry) expose NO legacy WHOIS server, so a plain WHOIS
// lookup for a domain like "iterflor.app" fails with "no whois server found for
// domain". Those registries are, however, required to serve RDAP.
//
// This file implements RDAPClient, the universal FALLBACK used by
// DomainExpiryService when the PRIMARY WHOIS lookup returns ANY error: it
// discovers the domain's RDAP base URL via the IANA bootstrap registry
// (https://data.iana.org/rdap/dns.json, RFC 9224), queries
// {base}/domain/{registrableDomain}, and reads the registration "expiration"
// event (RFC 9083). It uses ONLY the Go standard library (net/http,
// encoding/json) and reuses domain_expiry.whois_timeout_seconds for its HTTP
// timeout (no new config).

// RDAPClient abstracts an RDAP query plus registration-expiry parsing so the
// dependency can be mocked in tests (no real network requests in unit tests). It
// mirrors WhoisClient.
type RDAPClient interface {
	// LookupExpiry queries the RDAP endpoint for registrableDomain (an eTLD+1),
	// discovered via the IANA bootstrap registry, and returns the parsed
	// registration expiry date normalized to UTC. On failure it returns one of
	// the ErrRDAP* sentinels (wrapped) below.
	LookupExpiry(ctx context.Context, registrableDomain string) (time.Time, error)
}

// RDAP failure classification sentinels. These let the caller record a
// descriptive last_error and distinguish transient from terminal failures.
var (
	// ErrRDAPBootstrap indicates the IANA bootstrap registry could not be fetched
	// or parsed (and no usable cached copy was available).
	ErrRDAPBootstrap = errors.New("rdap bootstrap failed")
	// ErrRDAPNoServer indicates no RDAP base URL is published for the domain's TLD
	// (or the domain has no TLD to look up).
	ErrRDAPNoServer = errors.New("rdap no server for tld")
	// ErrRDAPQuery indicates the RDAP HTTP query failed: a transport error or a
	// non-2xx / non-404 status.
	ErrRDAPQuery = errors.New("rdap query failed")
	// ErrRDAPNotFound indicates the registry responded 404 — the domain is not
	// found at that RDAP server.
	ErrRDAPNotFound = errors.New("rdap domain not found")
	// ErrRDAPParse indicates the RDAP JSON response could not be parsed (malformed
	// body, or an expiration event whose date is unparsable).
	ErrRDAPParse = errors.New("rdap parse failed")
	// ErrRDAPNoExpiry indicates the RDAP response parsed successfully but carries
	// no "expiration" registration event.
	ErrRDAPNoExpiry = errors.New("rdap has no expiry")
)

// defaultRDAPTimeout is used when a non-positive timeout is supplied (mirrors the
// WHOIS client's defaultWhoisTimeout).
const defaultRDAPTimeout = 10 * time.Second

// rdapBootstrapTTL is how long a fetched bootstrap map is considered fresh before
// a refetch is attempted. The IANA registry changes rarely, so a daily refresh is
// ample.
const rdapBootstrapTTL = 24 * time.Hour

// defaultRDAPBootstrapURL is the IANA RDAP DNS bootstrap registry (RFC 9224).
const defaultRDAPBootstrapURL = "https://data.iana.org/rdap/dns.json"

// rdapMaxBodyBytes caps how much of any RDAP/bootstrap response body is read, to
// bound memory use against a hostile or misbehaving server (2 MiB).
const rdapMaxBodyBytes int64 = 2 << 20

// defaultRDAPClient is the default RDAPClient implementation built on net/http +
// encoding/json (no third-party dependency). It caches the IANA bootstrap map
// (tld -> RDAP base URLs) guarded by a mutex, refreshing it at most once per
// rdapBootstrapTTL.
type defaultRDAPClient struct {
	// httpClient issues bootstrap and domain requests. The default client follows
	// redirects, which some registries use for their RDAP base URLs.
	httpClient *http.Client
	// timeoutFn, when set, supplies the per-request timeout dynamically: it is
	// read on EVERY request (see resolveTimeout), so a change to the underlying
	// source (e.g. runtime config's whois_timeout_seconds) takes effect without
	// reconstructing the client.
	timeoutFn func() time.Duration
	// bootstrapURL is the IANA bootstrap registry URL. Overridable in tests (e.g.
	// pointed at an httptest server) to avoid real network access.
	bootstrapURL string

	// mu guards the cached bootstrap map and its fetch timestamp.
	mu           sync.Mutex
	bootstrapMap map[string][]string // tld -> RDAP base URLs (lowercased keys)
	fetchedAt    time.Time
}

// compile-time assertion that *defaultRDAPClient satisfies RDAPClient.
var _ RDAPClient = (*defaultRDAPClient)(nil)

// NewRDAPClient creates the default RDAPClient whose per-request timeout is
// resolved dynamically by timeoutFn on EVERY request (mirroring
// NewWhoisClientFunc), so a runtime config change takes effect on the next
// request with no reconstruction. When timeoutFn is nil or returns a non-positive
// duration, resolveTimeout falls back to defaultRDAPTimeout. The returned client
// uses http.DefaultClient semantics (a fresh &http.Client{} that follows
// redirects) and the default IANA bootstrap URL.
func NewRDAPClient(timeoutFn func() time.Duration) RDAPClient {
	return &defaultRDAPClient{
		httpClient:   &http.Client{},
		timeoutFn:    timeoutFn,
		bootstrapURL: defaultRDAPBootstrapURL,
	}
}

// resolveTimeout returns the per-request RDAP timeout, resolved fresh on every
// call so a dynamic timeoutFn reflects the current source value. Precedence:
//  1. timeoutFn's value, when the func is set AND returns a positive duration;
//  2. otherwise defaultRDAPTimeout.
func (c *defaultRDAPClient) resolveTimeout() time.Duration {
	if c.timeoutFn != nil {
		if d := c.timeoutFn(); d > 0 {
			return d
		}
	}
	return defaultRDAPTimeout
}

// rdapBootstrapFile mirrors the IANA RDAP bootstrap file shape (RFC 9224):
//
//	{ "services": [ [ [tld...], [baseURL...] ], ... ] }
//
// Each service entry is a two-element array: element 0 is the list of TLDs, and
// element 1 is the list of RDAP base URLs serving them.
type rdapBootstrapFile struct {
	Services [][][]string `json:"services"`
}

// parseRDAPBootstrap parses an IANA RDAP bootstrap document into a
// tld -> []baseURL map with lowercased TLD keys. It is a pure function (no I/O)
// so it is directly unit-testable. Malformed JSON yields ErrRDAPBootstrap;
// well-formed but empty/odd service entries are skipped rather than failing.
func parseRDAPBootstrap(data []byte) (map[string][]string, error) {
	var file rdapBootstrapFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRDAPBootstrap, err)
	}

	out := make(map[string][]string)
	for _, entry := range file.Services {
		// A valid entry has at least [tlds, baseURLs].
		if len(entry) < 2 {
			continue
		}
		tlds := entry[0]
		urls := entry[1]
		if len(urls) == 0 {
			continue
		}
		for _, tld := range tlds {
			key := strings.ToLower(strings.TrimSpace(tld))
			if key == "" {
				continue
			}
			// Defensive copy so callers can't mutate the parsed slice's backing
			// array through the map.
			cp := make([]string, len(urls))
			copy(cp, urls)
			out[key] = cp
		}
	}
	return out, nil
}

// bootstrap returns the cached tld -> baseURLs map, (re)fetching the IANA
// registry when the cache is empty or older than rdapBootstrapTTL. It is
// concurrency-safe (guarded by c.mu). On a refetch FAILURE it prefers a
// previously cached map (stale is better than nothing); only when there is no
// prior cache does it return the wrapped ErrRDAPBootstrap.
//
// The lock is intentionally held across the network fetch so concurrent callers
// don't stampede the IANA server; in practice DomainExpiryService serializes
// refreshes per registrable domain and runs RefreshAll sequentially, so
// contention here is minimal.
func (c *defaultRDAPClient) bootstrap(ctx context.Context) (map[string][]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Fresh cache hit.
	if len(c.bootstrapMap) > 0 && time.Since(c.fetchedAt) < rdapBootstrapTTL {
		return c.bootstrapMap, nil
	}

	m, err := c.fetchBootstrap(ctx)
	if err != nil {
		// Stale-but-usable fallback: prefer a previously cached map over failing.
		if len(c.bootstrapMap) > 0 {
			return c.bootstrapMap, nil
		}
		return nil, err
	}

	c.bootstrapMap = m
	c.fetchedAt = time.Now()
	return m, nil
}

// fetchBootstrap performs a single GET of the bootstrap URL (bounded by
// resolveTimeout and the ctx) and parses it via parseRDAPBootstrap. All failure
// paths are wrapped in ErrRDAPBootstrap.
func (c *defaultRDAPClient) fetchBootstrap(ctx context.Context) (map[string][]string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.resolveTimeout())
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, c.bootstrapURL, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRDAPBootstrap, err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRDAPBootstrap, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: unexpected status %d", ErrRDAPBootstrap, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, rdapMaxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRDAPBootstrap, err)
	}
	return parseRDAPBootstrap(body)
}

// LookupExpiry implements RDAPClient. It derives the TLD, resolves the RDAP base
// URL from the (cached) IANA bootstrap map, queries {base}/domain/{domain} with
// an "Accept: application/rdap+json" header, and parses the registration
// expiration event.
func (c *defaultRDAPClient) LookupExpiry(ctx context.Context, registrableDomain string) (time.Time, error) {
	// 1. TLD = substring after the LAST '.' (e.g. iterflor.app -> app,
	//    example.co.uk -> uk). No dot (or trailing dot) -> no server to consult.
	idx := strings.LastIndex(registrableDomain, ".")
	if idx < 0 || idx == len(registrableDomain)-1 {
		return time.Time{}, fmt.Errorf("%w: %q has no TLD", ErrRDAPNoServer, registrableDomain)
	}
	tld := strings.ToLower(registrableDomain[idx+1:])

	// 2. Resolve base URLs for the TLD from the bootstrap registry.
	bmap, err := c.bootstrap(ctx)
	if err != nil {
		return time.Time{}, err
	}
	baseURLs := bmap[tld]
	if len(baseURLs) == 0 {
		return time.Time{}, fmt.Errorf("%w: no rdap base url for tld %q", ErrRDAPNoServer, tld)
	}
	base := preferHTTPSBase(baseURLs)

	// 3. Build {base}/domain/{registrableDomain} and GET it.
	target := strings.TrimSuffix(base, "/") + "/domain/" + registrableDomain
	reqCtx, cancel := context.WithTimeout(ctx, c.resolveTimeout())
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, target, nil)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %v", ErrRDAPQuery, err)
	}
	req.Header.Set("Accept", "application/rdap+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %v", ErrRDAPQuery, err)
	}
	defer resp.Body.Close()

	// 4. Classify the status: 200-range -> parse; 404 -> not found; else -> query.
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return time.Time{}, fmt.Errorf("%w: %s", ErrRDAPNotFound, registrableDomain)
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return time.Time{}, fmt.Errorf("%w: unexpected status %d", ErrRDAPQuery, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, rdapMaxBodyBytes))
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %v", ErrRDAPQuery, err)
	}

	// 5. Parse the registration expiration event.
	return parseRDAPExpiry(body)
}

// preferHTTPSBase returns the first https:// base URL in urls, or the first URL
// when none is https. urls is guaranteed non-empty by the caller.
func preferHTTPSBase(urls []string) string {
	for _, u := range urls {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(u)), "https://") {
			return u
		}
	}
	return urls[0]
}

// rdapEvent mirrors an RFC 9083 event object: eventAction identifies the event
// kind ("registration", "expiration", "last changed", ...) and eventDate carries
// its timestamp.
type rdapEvent struct {
	EventAction string `json:"eventAction"`
	EventDate   string `json:"eventDate"`
}

// rdapDomainResponse is the subset of an RFC 9083 domain object this client
// needs: the events array from which the expiration timestamp is read.
type rdapDomainResponse struct {
	Events []rdapEvent `json:"events"`
}

// parseRDAPExpiry parses an RDAP domain response body and returns the
// registration expiry (UTC). It is a pure function (no I/O) so it is directly
// unit-testable.
//
//   - malformed JSON                              -> ErrRDAPParse
//   - no event with eventAction == "expiration"   -> ErrRDAPNoExpiry
//   - an expiration event whose date won't parse  -> ErrRDAPParse
//
// eventDate is parsed with time.RFC3339 then time.RFC3339Nano as a fallback, and
// the result is normalized to UTC (so a non-Z offset is converted to the correct
// UTC instant).
func parseRDAPExpiry(data []byte) (time.Time, error) {
	var resp rdapDomainResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return time.Time{}, fmt.Errorf("%w: %v", ErrRDAPParse, err)
	}
	for _, ev := range resp.Events {
		if ev.EventAction != "expiration" {
			continue
		}
		t, perr := parseRDAPEventDate(ev.EventDate)
		if perr != nil {
			return time.Time{}, fmt.Errorf("%w: %v", ErrRDAPParse, perr)
		}
		return t.UTC(), nil
	}
	return time.Time{}, ErrRDAPNoExpiry
}

// parseRDAPEventDate parses an RDAP eventDate string using time.RFC3339 with a
// time.RFC3339Nano fallback. It returns an error (not wrapped) when the value is
// empty or in an unrecognized format; callers wrap it in ErrRDAPParse.
func parseRDAPEventDate(s string) (time.Time, error) {
	v := strings.TrimSpace(s)
	if v == "" {
		return time.Time{}, errors.New("empty eventDate")
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("unrecognized eventDate format: %q", v)
}
