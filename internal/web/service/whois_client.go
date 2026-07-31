package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/likexian/whois"
	whoisparser "github.com/likexian/whois-parser"
)

// WhoisClient abstracts a WHOIS query plus registration-expiry parsing so the
// dependency can be mocked in tests (no real network requests in unit tests).
type WhoisClient interface {
	// LookupExpiry performs a WHOIS query against registrableDomain (an eTLD+1)
	// and returns the parsed registration expiry date normalized to UTC.
	// On failure it returns one of the ErrWhois* sentinels (wrapped) below.
	LookupExpiry(ctx context.Context, registrableDomain string) (time.Time, error)
}

// WHOIS failure classification sentinels. These let the service record a
// descriptive last_error and inform future backoff strategies.
//
// NOTE: ErrInvalidDomain (public-suffix / syntax errors) is defined alongside
// RegistrableDomain in registrable_domain.go; it is intentionally NOT redefined
// here — this file reuses the same-package ErrInvalidDomain.
var (
	// ErrWhoisQuery indicates the WHOIS query itself failed (network error or no
	// available WHOIS server). See requirement 4.5.
	ErrWhoisQuery = errors.New("whois query failed")
	// ErrWhoisRateLimit indicates the WHOIS server rate-limited the query. See
	// requirement 4.5.
	ErrWhoisRateLimit = errors.New("whois rate limited")
	// ErrWhoisParse indicates the WHOIS response could not be parsed into an
	// expiry date. See requirement 4.6.
	ErrWhoisParse = errors.New("whois parse failed")
	// ErrWhoisNoExpiry indicates the WHOIS response parsed successfully but has no
	// expiration date field. See requirement 4.6.
	ErrWhoisNoExpiry = errors.New("whois has no expiry")
)

// defaultWhoisTimeout is used when a non-positive timeout is supplied.
const defaultWhoisTimeout = 10 * time.Second

// expiryDateLayouts holds common WHOIS expiration-date string formats, tried in
// order only as a fallback when whois-parser did not populate
// ExpirationDateInTime. whois-parser normally pre-parses the time, so this is a
// best-effort last resort.
var expiryDateLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05Z",
	"2006-01-02 15:04:05",
	"2006-01-02",
	"02-Jan-2006",
	"2006.01.02",
	"2006/01/02",
}

// defaultWhoisClient is the default WhoisClient implementation wrapping
// github.com/likexian/whois and github.com/likexian/whois-parser.
type defaultWhoisClient struct {
	// timeout is a fixed per-query timeout, used when timeoutFn is nil or returns
	// a non-positive value. Set by NewWhoisClient.
	timeout time.Duration
	// timeoutFn, when set, supplies the per-query timeout dynamically: it is read
	// on EVERY query (see resolveTimeout), so a change to the underlying source
	// (e.g. runtime config's whois_timeout_seconds) takes effect without
	// reconstructing the client. Set by NewWhoisClientFunc.
	timeoutFn func() time.Duration
}

// compile-time assertion that defaultWhoisClient satisfies WhoisClient.
var _ WhoisClient = (*defaultWhoisClient)(nil)

// NewWhoisClient creates the default WhoisClient with the given fixed per-query
// timeout. A non-positive timeout falls back to defaultWhoisTimeout. Kept for
// backward compatibility; prefer NewWhoisClientFunc when the timeout must track
// a dynamic source such as the runtime config.
func NewWhoisClient(timeout time.Duration) WhoisClient {
	if timeout <= 0 {
		timeout = defaultWhoisTimeout
	}
	return &defaultWhoisClient{timeout: timeout}
}

// NewWhoisClientFunc creates the default WhoisClient whose per-query timeout is
// resolved dynamically by timeoutFn on EVERY query. This lets a runtime config
// change (e.g. whois_timeout_seconds updated via /api/system/config) take effect
// on the next query with NO client reconstruction. When timeoutFn is nil or
// returns a non-positive duration, resolveTimeout falls back to
// defaultWhoisTimeout.
func NewWhoisClientFunc(timeoutFn func() time.Duration) WhoisClient {
	return &defaultWhoisClient{timeoutFn: timeoutFn}
}

// resolveTimeout returns the per-query WHOIS timeout, resolved fresh on every
// call so a dynamic timeoutFn reflects the current source value. Precedence:
//  1. timeoutFn's value, when the func is set AND returns a positive duration;
//  2. otherwise the fixed timeout, when positive;
//  3. otherwise defaultWhoisTimeout.
func (c *defaultWhoisClient) resolveTimeout() time.Duration {
	if c.timeoutFn != nil {
		if d := c.timeoutFn(); d > 0 {
			return d
		}
	}
	if c.timeout > 0 {
		return c.timeout
	}
	return defaultWhoisTimeout
}

// whoisResult bundles the raw WHOIS text with any query error produced by the
// blocking lookup goroutine.
type whoisResult struct {
	raw string
	err error
}

// query runs the blocking whois.Whois call in a goroutine and honors ctx
// cancellation/deadline via select. The underlying TCP timeout is set with
// SetTimeout so the goroutine cannot block forever (avoiding a leak: the result
// channel is buffered so the goroutine can always send and exit even after the
// caller has returned on ctx.Done()).
func (c *defaultWhoisClient) query(ctx context.Context, domain string) (string, error) {
	// Resolve the timeout per query so a dynamic timeoutFn (e.g. reading the
	// current runtime config) is honored on every call, not frozen at
	// construction time.
	client := whois.NewClient().SetTimeout(c.resolveTimeout())
	ch := make(chan whoisResult, 1)
	go func() {
		raw, err := client.Whois(domain)
		ch <- whoisResult{raw: raw, err: err}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case res := <-ch:
		return res.raw, res.err
	}
}

// LookupExpiry implements WhoisClient.
func (c *defaultWhoisClient) LookupExpiry(ctx context.Context, registrableDomain string) (time.Time, error) {
	raw, err := c.query(ctx, registrableDomain)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %v", ErrWhoisQuery, err)
	}

	parsed, err := whoisparser.Parse(raw)
	if err != nil {
		// A rate-limit response is a distinct, recoverable failure class.
		if errors.Is(err, whoisparser.ErrDomainLimitExceed) {
			return time.Time{}, fmt.Errorf("%w: %v", ErrWhoisRateLimit, err)
		}
		return time.Time{}, fmt.Errorf("%w: %v", ErrWhoisParse, err)
	}

	// No parsable domain block, or neither expiry representation present.
	if parsed.Domain == nil ||
		(parsed.Domain.ExpirationDateInTime == nil && strings.TrimSpace(parsed.Domain.ExpirationDate) == "") {
		return time.Time{}, ErrWhoisNoExpiry
	}

	// Prefer the pre-parsed *time.Time; fall back to parsing the raw string.
	if parsed.Domain.ExpirationDateInTime != nil {
		return parsed.Domain.ExpirationDateInTime.UTC(), nil
	}

	expiry, perr := parseExpiryString(parsed.Domain.ExpirationDate)
	if perr != nil {
		return time.Time{}, fmt.Errorf("%w: %v", ErrWhoisParse, perr)
	}
	return expiry.UTC(), nil
}

// parseExpiryString attempts to parse a WHOIS expiration-date string using a set
// of common layouts. It is only used as a fallback when whois-parser did not
// populate ExpirationDateInTime.
func parseExpiryString(s string) (time.Time, error) {
	v := strings.TrimSpace(s)
	if v == "" {
		return time.Time{}, errors.New("empty expiration date")
	}
	for _, layout := range expiryDateLayouts {
		if t, err := time.Parse(layout, v); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized expiration date format: %q", v)
}
