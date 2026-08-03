package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// This file holds the RDAP client tests. It exercises the two pure, testable
// parse functions (parseRDAPExpiry / parseRDAPBootstrap) with unit cases, drives
// the full defaultRDAPClient.LookupExpiry path against a LOCAL httptest server
// (no real network), and adds a gopter property over parseRDAPExpiry. No test in
// this file makes a real network request.

// ---------------------------------------------------------------------------
// parseRDAPExpiry unit tests
// ---------------------------------------------------------------------------

// TestParseRDAPExpiry_ReturnsExpirationInUTC verifies that a realistic RDAP
// domain response carrying registration + expiration + last-changed events yields
// the expiration instant, normalized to UTC.
func TestParseRDAPExpiry_ReturnsExpirationInUTC(t *testing.T) {
	body := []byte(`{
		"objectClassName": "domain",
		"ldhName": "iterflor.app",
		"events": [
			{"eventAction": "registration",  "eventDate": "2020-08-01T04:00:00Z"},
			{"eventAction": "expiration",    "eventDate": "2025-08-01T04:00:00Z"},
			{"eventAction": "last changed",  "eventDate": "2024-01-02T09:10:11Z"}
		]
	}`)

	got, err := parseRDAPExpiry(body)
	if err != nil {
		t.Fatalf("parseRDAPExpiry returned error: %v", err)
	}
	want := time.Date(2025, 8, 1, 4, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("expiry = %s; want instant-equal to %s", got, want)
	}
	if got.Location().String() != "UTC" {
		t.Errorf("expiry location = %s; want UTC", got.Location())
	}
}

// TestParseRDAPExpiry_NonZOffsetNormalizesToUTC verifies that an eventDate with a
// non-Z timezone offset is converted to the correct UTC instant.
func TestParseRDAPExpiry_NonZOffsetNormalizesToUTC(t *testing.T) {
	// 2025-01-15T00:00:00+08:00 == 2025-01-14T16:00:00Z.
	body := []byte(`{"events":[{"eventAction":"expiration","eventDate":"2025-01-15T00:00:00+08:00"}]}`)

	got, err := parseRDAPExpiry(body)
	if err != nil {
		t.Fatalf("parseRDAPExpiry returned error: %v", err)
	}
	want := time.Date(2025, 1, 14, 16, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("expiry = %s; want instant-equal to %s (UTC)", got, want)
	}
	if got.Location().String() != "UTC" {
		t.Errorf("expiry location = %s; want UTC", got.Location())
	}
}

// TestParseRDAPExpiry_MissingExpirationEvent verifies that a response with events
// but no "expiration" event yields ErrRDAPNoExpiry.
func TestParseRDAPExpiry_MissingExpirationEvent(t *testing.T) {
	body := []byte(`{"events":[{"eventAction":"registration","eventDate":"2020-08-01T04:00:00Z"}]}`)

	_, err := parseRDAPExpiry(body)
	if !errors.Is(err, ErrRDAPNoExpiry) {
		t.Errorf("err = %v; want ErrRDAPNoExpiry", err)
	}
}

// TestParseRDAPExpiry_NoEventsAtAll verifies an empty/absent events array yields
// ErrRDAPNoExpiry (parsed OK, but nothing to read).
func TestParseRDAPExpiry_NoEventsAtAll(t *testing.T) {
	for _, body := range [][]byte{
		[]byte(`{"objectClassName":"domain","ldhName":"x.app"}`),
		[]byte(`{"events":[]}`),
	} {
		if _, err := parseRDAPExpiry(body); !errors.Is(err, ErrRDAPNoExpiry) {
			t.Errorf("parseRDAPExpiry(%s) err = %v; want ErrRDAPNoExpiry", body, err)
		}
	}
}

// TestParseRDAPExpiry_UnparseableExpirationDate verifies that an expiration event
// whose eventDate is not a recognized format yields ErrRDAPParse.
func TestParseRDAPExpiry_UnparseableExpirationDate(t *testing.T) {
	for _, body := range [][]byte{
		[]byte(`{"events":[{"eventAction":"expiration","eventDate":"not-a-date"}]}`),
		[]byte(`{"events":[{"eventAction":"expiration","eventDate":""}]}`),
	} {
		if _, err := parseRDAPExpiry(body); !errors.Is(err, ErrRDAPParse) {
			t.Errorf("parseRDAPExpiry(%s) err = %v; want ErrRDAPParse", body, err)
		}
	}
}

// TestParseRDAPExpiry_MalformedJSON verifies that a body that is not valid JSON
// yields ErrRDAPParse.
func TestParseRDAPExpiry_MalformedJSON(t *testing.T) {
	if _, err := parseRDAPExpiry([]byte(`{not json`)); !errors.Is(err, ErrRDAPParse) {
		t.Errorf("err = %v; want ErrRDAPParse", err)
	}
}

// ---------------------------------------------------------------------------
// parseRDAPBootstrap unit tests
// ---------------------------------------------------------------------------

// TestParseRDAPBootstrap_BuildsLowercasedTLDMap verifies that a sample IANA
// dns.json builds the correct tld -> baseURLs map: keys are lowercased and a
// single service entry listing multiple TLDs maps each of them to the same base
// URL list.
func TestParseRDAPBootstrap_BuildsLowercasedTLDMap(t *testing.T) {
	data := []byte(`{
		"description": "RDAP bootstrap file for DNS",
		"services": [
			[ ["app", "DEV"], ["https://rdap.nic.google/"] ],
			[ ["com"],        ["https://rdap.verisign.com/com/v1/"] ]
		]
	}`)

	m, err := parseRDAPBootstrap(data)
	if err != nil {
		t.Fatalf("parseRDAPBootstrap returned error: %v", err)
	}

	cases := map[string][]string{
		"app": {"https://rdap.nic.google/"},
		"dev": {"https://rdap.nic.google/"}, // lowercased from "DEV"
		"com": {"https://rdap.verisign.com/com/v1/"},
	}
	if len(m) != len(cases) {
		t.Fatalf("map has %d keys (%v); want %d", len(m), keysOfStrSlice(m), len(cases))
	}
	for tld, wantURLs := range cases {
		gotURLs, ok := m[tld]
		if !ok {
			t.Errorf("missing tld %q in map %v", tld, keysOfStrSlice(m))
			continue
		}
		if len(gotURLs) != len(wantURLs) || gotURLs[0] != wantURLs[0] {
			t.Errorf("tld %q -> %v; want %v", tld, gotURLs, wantURLs)
		}
	}
	// Uppercase keys must NOT be present (only lowercased ones).
	if _, ok := m["DEV"]; ok {
		t.Errorf("map unexpectedly contains uppercase key \"DEV\"")
	}
}

// TestParseRDAPBootstrap_SkipsEmptyAndOddEntries verifies that well-formed JSON
// with entries lacking URLs (or lacking the URL slice) are skipped rather than
// causing an error.
func TestParseRDAPBootstrap_SkipsEmptyAndOddEntries(t *testing.T) {
	data := []byte(`{"services":[
		[ ["com"], [] ],
		[ ["net"] ],
		[ ["org"], ["https://rdap.example/org/"] ]
	]}`)

	m, err := parseRDAPBootstrap(data)
	if err != nil {
		t.Fatalf("parseRDAPBootstrap returned error: %v", err)
	}
	if _, ok := m["com"]; ok {
		t.Errorf("com should be skipped (empty URL list)")
	}
	if _, ok := m["net"]; ok {
		t.Errorf("net should be skipped (no URL slice)")
	}
	if got := m["org"]; len(got) != 1 || got[0] != "https://rdap.example/org/" {
		t.Errorf("org -> %v; want [https://rdap.example/org/]", got)
	}
}

// TestParseRDAPBootstrap_MalformedJSON verifies a non-JSON body yields
// ErrRDAPBootstrap.
func TestParseRDAPBootstrap_MalformedJSON(t *testing.T) {
	if _, err := parseRDAPBootstrap([]byte(`nope`)); !errors.Is(err, ErrRDAPBootstrap) {
		t.Errorf("err = %v; want ErrRDAPBootstrap", err)
	}
}

// keysOfStrSlice returns the keys of a map[string][]string for readable logs.
func keysOfStrSlice(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// ---------------------------------------------------------------------------
// End-to-end LookupExpiry against a LOCAL httptest server (no real network)
// ---------------------------------------------------------------------------

// newTestRDAPServer stands up a local httptest server that plays BOTH the IANA
// bootstrap registry and an RDAP registry:
//
//   - GET /rdap/dns.json      -> a bootstrap doc mapping "app" (and "dev") at THIS
//     same server (base = http://<host>/, with a trailing slash to exercise the
//     TrimSuffix in LookupExpiry).
//   - GET /domain/{name}      -> an RDAP domain object with an expiration event,
//     EXCEPT "notfound.app" which returns 404. The handler also asserts the
//     "Accept: application/rdap+json" request header is sent.
//
// The returned expiry is the instant every served domain (other than notfound)
// reports as its expiration.
func newTestRDAPServer(t *testing.T) (*httptest.Server, time.Time) {
	t.Helper()

	expiry := time.Date(2025, 8, 1, 4, 0, 0, 0, time.UTC)

	mux := http.NewServeMux()
	mux.HandleFunc("/rdap/dns.json", func(w http.ResponseWriter, r *http.Request) {
		// Point the "app"/"dev" TLDs at this same server. Trailing slash on the
		// base URL exercises LookupExpiry's strings.TrimSuffix(base, "/").
		base := "http://" + r.Host + "/"
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"services":[[["app","dev"],[%q]]]}`, base)
	})
	mux.HandleFunc("/domain/", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "application/rdap+json" {
			t.Errorf("domain request Accept header = %q; want application/rdap+json", got)
		}
		name := strings.TrimPrefix(r.URL.Path, "/domain/")
		if name == "notfound.app" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/rdap+json")
		fmt.Fprintf(w, `{
			"objectClassName": "domain",
			"ldhName": %q,
			"events": [
				{"eventAction": "registration", "eventDate": "2020-08-01T04:00:00Z"},
				{"eventAction": "expiration",   "eventDate": "2025-08-01T04:00:00Z"}
			]
		}`, name)
	})

	srv := httptest.NewServer(mux)
	return srv, expiry
}

// newTestRDAPClient builds a defaultRDAPClient wired to the given test server's
// client and bootstrap endpoint. Because the test lives in package service it can
// set the unexported fields directly, keeping the production constructor free of
// test seams.
func newTestRDAPClient(srv *httptest.Server) *defaultRDAPClient {
	return &defaultRDAPClient{
		httpClient:   srv.Client(),
		bootstrapURL: srv.URL + "/rdap/dns.json",
	}
}

// TestDefaultRDAPClient_LookupExpiry_EndToEnd drives the full path (bootstrap
// fetch -> TLD resolution -> domain query -> expiration parse) against the local
// test server and asserts the expected UTC expiry.
func TestDefaultRDAPClient_LookupExpiry_EndToEnd(t *testing.T) {
	srv, wantExpiry := newTestRDAPServer(t)
	defer srv.Close()

	c := newTestRDAPClient(srv)

	got, err := c.LookupExpiry(context.Background(), "iterflor.app")
	if err != nil {
		t.Fatalf("LookupExpiry returned error: %v", err)
	}
	if !got.Equal(wantExpiry) {
		t.Errorf("expiry = %s; want instant-equal to %s", got, wantExpiry)
	}
	if got.Location().String() != "UTC" {
		t.Errorf("expiry location = %s; want UTC", got.Location())
	}
}

// TestDefaultRDAPClient_LookupExpiry_CachesBootstrap verifies the bootstrap map is
// fetched once and reused across lookups (a second lookup issues no second
// bootstrap fetch).
func TestDefaultRDAPClient_LookupExpiry_CachesBootstrap(t *testing.T) {
	var bootstrapHits int32
	expiry := time.Date(2030, 3, 4, 5, 6, 7, 0, time.UTC)

	mux := http.NewServeMux()
	mux.HandleFunc("/rdap/dns.json", func(w http.ResponseWriter, r *http.Request) {
		bootstrapHits++
		base := "http://" + r.Host + "/"
		fmt.Fprintf(w, `{"services":[[["app"],[%q]]]}`, base)
	})
	mux.HandleFunc("/domain/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"events":[{"eventAction":"expiration","eventDate":%q}]}`, expiry.Format(time.RFC3339))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestRDAPClient(srv)
	for i := 0; i < 3; i++ {
		if _, err := c.LookupExpiry(context.Background(), "iterflor.app"); err != nil {
			t.Fatalf("LookupExpiry #%d error: %v", i, err)
		}
	}
	if bootstrapHits != 1 {
		t.Errorf("bootstrap fetched %d times; want exactly 1 (cached)", bootstrapHits)
	}
}

// TestDefaultRDAPClient_LookupExpiry_NotFound verifies a 404 from the registry
// maps to ErrRDAPNotFound.
func TestDefaultRDAPClient_LookupExpiry_NotFound(t *testing.T) {
	srv, _ := newTestRDAPServer(t)
	defer srv.Close()

	c := newTestRDAPClient(srv)
	if _, err := c.LookupExpiry(context.Background(), "notfound.app"); !errors.Is(err, ErrRDAPNotFound) {
		t.Errorf("err = %v; want ErrRDAPNotFound", err)
	}
}

// TestDefaultRDAPClient_LookupExpiry_NoServerForTLD verifies that a TLD absent
// from the bootstrap map yields ErrRDAPNoServer (bootstrap succeeds; the TLD just
// isn't served).
func TestDefaultRDAPClient_LookupExpiry_NoServerForTLD(t *testing.T) {
	srv, _ := newTestRDAPServer(t)
	defer srv.Close()

	c := newTestRDAPClient(srv)
	if _, err := c.LookupExpiry(context.Background(), "example.zzz"); !errors.Is(err, ErrRDAPNoServer) {
		t.Errorf("err = %v; want ErrRDAPNoServer", err)
	}
}

// TestDefaultRDAPClient_LookupExpiry_NoTLD verifies that a domain with no dot (no
// TLD to look up) yields ErrRDAPNoServer without any network call.
func TestDefaultRDAPClient_LookupExpiry_NoTLD(t *testing.T) {
	// bootstrapURL is deliberately unreachable; the no-TLD guard must short-circuit
	// before any bootstrap fetch.
	c := &defaultRDAPClient{httpClient: &http.Client{}, bootstrapURL: "http://127.0.0.1:0/never"}
	if _, err := c.LookupExpiry(context.Background(), "localhost"); !errors.Is(err, ErrRDAPNoServer) {
		t.Errorf("err = %v; want ErrRDAPNoServer", err)
	}
}

// TestDefaultRDAPClient_BootstrapFailureNoPriorCache verifies that when the very
// first bootstrap fetch fails (and there is no prior cache), LookupExpiry surfaces
// ErrRDAPBootstrap.
func TestDefaultRDAPClient_BootstrapFailureNoPriorCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newTestRDAPClient(srv)
	if _, err := c.LookupExpiry(context.Background(), "iterflor.app"); !errors.Is(err, ErrRDAPBootstrap) {
		t.Errorf("err = %v; want ErrRDAPBootstrap", err)
	}
}

// TestDefaultRDAPClient_PrefersHTTPSBase verifies preferHTTPSBase selects the
// first https base URL, falling back to the first URL when none is https.
func TestDefaultRDAPClient_PrefersHTTPSBase(t *testing.T) {
	if got := preferHTTPSBase([]string{"http://a/", "https://b/", "https://c/"}); got != "https://b/" {
		t.Errorf("preferHTTPSBase = %q; want https://b/", got)
	}
	if got := preferHTTPSBase([]string{"http://only/"}); got != "http://only/" {
		t.Errorf("preferHTTPSBase = %q; want http://only/ (fallback to first)", got)
	}
}

// ---------------------------------------------------------------------------
// Property: parseRDAPExpiry round-trips any RFC3339 instant (pure-function PBT)
// ---------------------------------------------------------------------------

// TestPropertyRDAPParseExpiryRoundTrip is a local property test (NOT one of the
// design's numbered DomainExpiryService properties): for ANY whole-second instant
// displayed in an arbitrary zone and written into an "expiration" event as an
// RFC3339 string, parseRDAPExpiry returns that same instant (UTC-equal via
// time.Time.Equal) in the UTC location. This pins the parser's instant-fidelity
// and zone normalization across the full input space. No network. gopter >=100
// iterations.
func TestPropertyRDAPParseExpiryRoundTrip(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42) // deterministic

	properties := gopter.NewProperties(parameters)

	properties.Property("parseRDAPExpiry returns the RFC3339 expiration instant, normalized to UTC", prop.ForAll(
		func(sec int64, offsetHours int) bool {
			loc := time.FixedZone("rdapzone", offsetHours*3600)
			want := time.Unix(sec, 0).In(loc)
			dateStr := want.Format(time.RFC3339)

			body := []byte(fmt.Sprintf(
				`{"events":[{"eventAction":"registration","eventDate":"2000-01-01T00:00:00Z"},{"eventAction":"expiration","eventDate":%q}]}`,
				dateStr,
			))

			got, err := parseRDAPExpiry(body)
			if err != nil {
				t.Logf("parseRDAPExpiry(%s) error: %v", dateStr, err)
				return false
			}
			if !got.Equal(want) {
				t.Logf("parseRDAPExpiry(%s) = %s; want instant-equal to %s", dateStr, got, want)
				return false
			}
			if got.Location().String() != "UTC" {
				t.Logf("parseRDAPExpiry(%s) location = %s; want UTC", dateStr, got.Location())
				return false
			}
			return true
		},
		// Whole-second instants in [1970, 2100) (reusing prop6MaxExpirySeconds from
		// the service property tests) so RFC3339 round-trips exactly.
		gen.Int64Range(0, prop6MaxExpirySeconds),
		gen.IntRange(-12, 14), // arbitrary display-zone offset (hours)
	))

	properties.TestingRun(t)
}
