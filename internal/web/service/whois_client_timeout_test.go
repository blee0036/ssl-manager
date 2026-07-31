package service

import (
	"testing"
	"time"

	"github.com/ssl-manager/ssl-manager/internal/config"
)

// This file holds the regression tests for the code-review finding
// "whois_timeout_seconds 只在服务启动时读取，运行时配置更新不会生效": the default
// WHOIS client must read the CURRENT runtime-config timeout on every query
// rather than freezing it at construction time.
//
// These tests are deterministic (standard testing, no gopter, no network): they
// only inspect the client's resolveTimeout(), which is exactly what query()
// feeds to SetTimeout per call.

// TestDomainExpiryService_WhoisTimeoutIsDynamic is the core regression guard for
// the finding. It proves that after NewDomainExpiryService wires the default
// WHOIS client, changing DomainExpiry.WhoisTimeoutSeconds via the runtime config
// takes effect on the NEXT query with NO service/client reconstruction, and that
// a non-positive value falls back to the WHOIS client default.
func TestDomainExpiryService_WhoisTimeoutIsDynamic(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DomainExpiry.WhoisTimeoutSeconds = 5
	rc := config.NewRuntimeConfig(cfg)

	// nil repo/scanner/alerter are fine: this test only inspects the default
	// WHOIS client the constructor builds from the runtime config.
	svc := NewDomainExpiryService(nil, nil, nil, rc)

	dc, ok := svc.whois.(*defaultWhoisClient)
	if !ok {
		t.Fatalf("expected the default WHOIS client to be *defaultWhoisClient, got %T", svc.whois)
	}

	// Initial config value is honored.
	if got := dc.resolveTimeout(); got != 5*time.Second {
		t.Fatalf("resolveTimeout() = %v, want %v (initial config)", got, 5*time.Second)
	}

	// Update the runtime config WITHOUT reconstructing the service or client.
	c := rc.Get()
	c.DomainExpiry.WhoisTimeoutSeconds = 25
	rc.Update(c)

	// The change must be reflected on the very next resolve — this is the
	// behavior the finding says was previously frozen at construction time.
	if got := dc.resolveTimeout(); got != 25*time.Second {
		t.Fatalf("after runtime config update, resolveTimeout() = %v, want %v (change must take effect with no reconstruction)", got, 25*time.Second)
	}

	// A non-positive config value falls back to the WHOIS client default.
	c = rc.Get()
	c.DomainExpiry.WhoisTimeoutSeconds = 0
	rc.Update(c)

	if got := dc.resolveTimeout(); got != defaultWhoisTimeout {
		t.Fatalf("with non-positive config, resolveTimeout() = %v, want fallback %v", got, defaultWhoisTimeout)
	}
}

// TestDefaultWhoisClient_ResolveTimeoutPrecedence pins down the precedence rules
// of resolveTimeout(): a positive timeoutFn wins; a non-positive timeoutFn falls
// back to the fixed timeout; with neither positive it falls back to
// defaultWhoisTimeout.
func TestDefaultWhoisClient_ResolveTimeoutPrecedence(t *testing.T) {
	tests := []struct {
		name   string
		client *defaultWhoisClient
		want   time.Duration
	}{
		{
			name:   "func>0 wins over fixed",
			client: &defaultWhoisClient{timeout: 7 * time.Second, timeoutFn: func() time.Duration { return 3 * time.Second }},
			want:   3 * time.Second,
		},
		{
			name:   "func==0 falls back to fixed",
			client: &defaultWhoisClient{timeout: 7 * time.Second, timeoutFn: func() time.Duration { return 0 }},
			want:   7 * time.Second,
		},
		{
			name:   "func<0 falls back to fixed",
			client: &defaultWhoisClient{timeout: 7 * time.Second, timeoutFn: func() time.Duration { return -1 }},
			want:   7 * time.Second,
		},
		{
			name:   "fixed used when no func",
			client: &defaultWhoisClient{timeout: 7 * time.Second},
			want:   7 * time.Second,
		},
		{
			name:   "both unset -> default",
			client: &defaultWhoisClient{},
			want:   defaultWhoisTimeout,
		},
		{
			name:   "func<=0 and fixed<=0 -> default",
			client: &defaultWhoisClient{timeoutFn: func() time.Duration { return 0 }},
			want:   defaultWhoisTimeout,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.client.resolveTimeout(); got != tc.want {
				t.Errorf("resolveTimeout() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestNewWhoisClient_BackwardCompatNormalization verifies the legacy fixed-timeout
// constructor still normalizes a non-positive timeout to defaultWhoisTimeout and
// preserves a positive one.
func TestNewWhoisClient_BackwardCompatNormalization(t *testing.T) {
	if dc, ok := NewWhoisClient(4 * time.Second).(*defaultWhoisClient); !ok {
		t.Fatalf("expected *defaultWhoisClient")
	} else if got := dc.resolveTimeout(); got != 4*time.Second {
		t.Errorf("NewWhoisClient(4s).resolveTimeout() = %v, want %v", got, 4*time.Second)
	}

	for _, in := range []time.Duration{0, -5 * time.Second} {
		dc, ok := NewWhoisClient(in).(*defaultWhoisClient)
		if !ok {
			t.Fatalf("expected *defaultWhoisClient for input %v", in)
		}
		if got := dc.resolveTimeout(); got != defaultWhoisTimeout {
			t.Errorf("NewWhoisClient(%v).resolveTimeout() = %v, want fallback %v", in, got, defaultWhoisTimeout)
		}
	}
}

// TestNewWhoisClientFunc_NilFuncFallsBack verifies a nil timeoutFn resolves to
// defaultWhoisTimeout (resolveTimeout guards the nil case).
func TestNewWhoisClientFunc_NilFuncFallsBack(t *testing.T) {
	dc, ok := NewWhoisClientFunc(nil).(*defaultWhoisClient)
	if !ok {
		t.Fatalf("expected *defaultWhoisClient")
	}
	if got := dc.resolveTimeout(); got != defaultWhoisTimeout {
		t.Errorf("NewWhoisClientFunc(nil).resolveTimeout() = %v, want %v", got, defaultWhoisTimeout)
	}
}
