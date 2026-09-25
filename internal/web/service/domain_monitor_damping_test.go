package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ssl-manager/ssl-manager/internal/config"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

// These tests cover the two layers of flap damping added to domain monitoring:
//
//  1. In-round retries (domain_monitor.probe_retries) — a failure that clears on a retry
//     never becomes a failed result row at all.
//  2. Consecutive-failure gating (domain_monitor.alert_after_failures) — a failed round
//     only turns into a notification once the failure has repeated.
//
// The behaviour they pin down is the fix for a concrete production problem: a single
// transient "TLS handshake failed: context deadline exceeded" pushed a warning and then a
// [Recovered] message on the following round, so every blip cost two notifications and the
// alert was already stale when read.

// scriptedDNSResolver returns a queued outcome per call, so a test can describe a
// sequence like "fail, then succeed" across attempts or probe rounds.
type scriptedDNSResolver struct {
	results []scriptedDNSResult
	calls   int
}

type scriptedDNSResult struct {
	ips []string
	err error
}

func (r *scriptedDNSResolver) LookupHost(_ context.Context, _ string) ([]string, error) {
	i := r.calls
	r.calls++
	if i >= len(r.results) {
		// Past the end of the script, keep repeating the last outcome so a test only has
		// to describe the interesting prefix.
		i = len(r.results) - 1
	}
	return r.results[i].ips, r.results[i].err
}

// dampingTestConfig builds a runtime config with explicit damping settings and no retry
// delay, so tests exercise the thresholds without sleeping.
func dampingTestConfig(retries, alertAfterFailures int) *config.RuntimeConfig {
	cfg := config.DefaultConfig()
	cfg.DomainMonitor.TimeoutSeconds = 2
	cfg.DomainMonitor.ProbeRetries = retries
	cfg.DomainMonitor.RetryDelaySeconds = 0
	cfg.DomainMonitor.AlertAfterFailures = alertAfterFailures
	return config.NewRuntimeConfig(cfg)
}

// newDampingTestService wires a domain monitor service over a fresh DB with the given
// damping settings, and returns it together with the recording alert sender.
func newDampingTestService(t *testing.T, retries, alertAfterFailures int) (*DomainMonitorService, *mockAlertSender) {
	t.Helper()

	db := setupDomainMonitorTestDB(t)
	domainRepo := repository.NewDomainRepository(db)
	alerter := &mockAlertSender{}
	svc := NewDomainMonitorService(domainRepo, nil, alerter, dampingTestConfig(retries, alertAfterFailures))
	return svc, alerter
}

func TestProbe_TransientFailureAbsorbedByRetry(t *testing.T) {
	// One retry allowed; DNS fails on the first attempt and succeeds on the second.
	svc, alerter := newDampingTestService(t, 1, 1)
	svc.SetDNSResolver(&scriptedDNSResolver{results: []scriptedDNSResult{
		{err: fmt.Errorf("i/o timeout")},
		{ips: []string{"1.2.3.4"}},
	}})
	// The handshake still fails, but that is a different alert type; what matters here is
	// that DNS resolution reported success and produced the resolved IPs.
	svc.SetTLSDialer(&mockTLSDialer{err: fmt.Errorf("connection refused")})

	ctx := context.Background()
	domain, err := svc.Create(ctx, model.CreateDomainInput{Name: "retry.example.com", MonitorPort: 443})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	result, err := svc.Probe(ctx, domain.ID)
	if err != nil {
		t.Fatalf("Probe failed: %v", err)
	}

	if len(result.ResolvedIPs) != 1 || result.ResolvedIPs[0] != "1.2.3.4" {
		t.Errorf("expected the retry to supply resolved IPs [1.2.3.4], got %v", result.ResolvedIPs)
	}
	for _, a := range alerter.alerts {
		if a.AlertType == "dns_resolve_failed" {
			t.Error("transient DNS failure that cleared on retry must not raise dns_resolve_failed")
		}
	}
}

func TestProbe_NoRetryWhenRetriesDisabled(t *testing.T) {
	// probe_retries = 0 must be honoured literally: exactly one attempt per round.
	svc, _ := newDampingTestService(t, 0, 1)
	resolver := &scriptedDNSResolver{results: []scriptedDNSResult{
		{err: fmt.Errorf("i/o timeout")},
		{ips: []string{"1.2.3.4"}},
	}}
	svc.SetDNSResolver(resolver)

	ctx := context.Background()
	domain, err := svc.Create(ctx, model.CreateDomainInput{Name: "noretry.example.com", MonitorPort: 443})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	result, err := svc.Probe(ctx, domain.ID)
	if err != nil {
		t.Fatalf("Probe failed: %v", err)
	}

	if resolver.calls != 1 {
		t.Errorf("expected exactly 1 DNS attempt with retries disabled, got %d", resolver.calls)
	}
	if result.TLSSuccess {
		t.Error("expected the probe to fail when its single attempt fails")
	}
}

func TestProbe_AlertHeldUntilFailureThresholdReached(t *testing.T) {
	// Threshold 2: the first failed round stays silent, the second one alerts.
	svc, alerter := newDampingTestService(t, 0, 2)
	svc.SetDNSResolver(&mockDNSResolver{err: fmt.Errorf("no such host")})

	ctx := context.Background()
	domain, err := svc.Create(ctx, model.CreateDomainInput{Name: "flappy.example.com", MonitorPort: 443})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if _, err := svc.Probe(ctx, domain.ID); err != nil {
		t.Fatalf("first Probe failed: %v", err)
	}
	if len(alerter.alerts) != 0 {
		t.Fatalf("expected no alert after 1 failed round with threshold 2, got %d", len(alerter.alerts))
	}

	if _, err := svc.Probe(ctx, domain.ID); err != nil {
		t.Fatalf("second Probe failed: %v", err)
	}
	if len(alerter.alerts) != 1 {
		t.Fatalf("expected exactly 1 alert once the threshold is reached, got %d", len(alerter.alerts))
	}
	if alerter.alerts[0].AlertType != "dns_resolve_failed" {
		t.Errorf("expected alert type 'dns_resolve_failed', got %q", alerter.alerts[0].AlertType)
	}
}

func TestProbe_SuccessResetsFailureStreak(t *testing.T) {
	// A healthy round in between must break the streak, so the pattern
	// fail → success → fail never reaches a threshold of 2.
	//
	// The two earlier rounds are written directly with explicit, distinct timestamps.
	// checked_at is stored as second-precision RFC3339, so three probes driven in the same
	// wall-clock second would tie and make the ordering — and therefore the streak —
	// non-deterministic. Back-dating them keeps the live probe below unambiguously newest.
	svc, alerter := newDampingTestService(t, 0, 2)

	ctx := context.Background()
	domain, err := svc.Create(ctx, model.CreateDomainInput{Name: "reset.example.com", MonitorPort: 443})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	base := time.Now().UTC()

	// Round 1: a failure.
	if err := svc.domainRepo.SaveMonitorResult(ctx, &model.DomainMonitorResult{
		DomainID:     domain.ID,
		CheckedPort:  domain.MonitorPort,
		TLSSuccess:   false,
		ErrorMessage: "DNS resolution failed: no such host",
		CheckedAt:    base.Add(-10 * time.Second),
	}); err != nil {
		t.Fatalf("failed to save failed result: %v", err)
	}

	// Round 2: fully healthy — matches the condition that auto-resolves alerts.
	if err := svc.domainRepo.SaveMonitorResult(ctx, &model.DomainMonitorResult{
		DomainID:      domain.ID,
		CheckedPort:   domain.MonitorPort,
		TLSSuccess:    true,
		DomainMatched: true,
		ChainValid:    true,
		CheckedAt:     base.Add(-5 * time.Second),
	}); err != nil {
		t.Fatalf("failed to save healthy result: %v", err)
	}

	// Round 3: failing again. The streak is 1 because the healthy round broke it, so this
	// must stay below the threshold of 2 and raise nothing.
	svc.SetDNSResolver(&mockDNSResolver{err: fmt.Errorf("no such host")})
	if _, err := svc.Probe(ctx, domain.ID); err != nil {
		t.Fatalf("round 3 Probe failed: %v", err)
	}

	if len(alerter.alerts) != 0 {
		t.Errorf("a healthy round must reset the streak; expected no alerts, got %d", len(alerter.alerts))
	}
}

func TestProbe_AlertOnFirstFailureWhenThresholdIsOne(t *testing.T) {
	// alert_after_failures = 1 restores the original "alert immediately" behaviour for
	// operators who prefer it.
	svc, alerter := newDampingTestService(t, 0, 1)
	svc.SetDNSResolver(&mockDNSResolver{err: fmt.Errorf("no such host")})

	ctx := context.Background()
	domain, err := svc.Create(ctx, model.CreateDomainInput{Name: "eager.example.com", MonitorPort: 443})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if _, err := svc.Probe(ctx, domain.ID); err != nil {
		t.Fatalf("Probe failed: %v", err)
	}

	if len(alerter.alerts) != 1 {
		t.Fatalf("expected 1 alert on the first failure with threshold 1, got %d", len(alerter.alerts))
	}
}

func TestProbe_IgnoredDomainNeverAlerts(t *testing.T) {
	// alert_ignored must still short-circuit before the damping logic runs.
	svc, alerter := newDampingTestService(t, 0, 1)
	svc.SetDNSResolver(&mockDNSResolver{err: fmt.Errorf("no such host")})

	ctx := context.Background()
	domain, err := svc.Create(ctx, model.CreateDomainInput{Name: "ignored.example.com", MonitorPort: 443})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	ignored := true
	if _, err := svc.Update(ctx, domain.ID, model.UpdateDomainInput{AlertIgnored: &ignored}); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if _, err := svc.Probe(ctx, domain.ID); err != nil {
		t.Fatalf("Probe failed: %v", err)
	}

	if len(alerter.alerts) != 0 {
		t.Errorf("expected no alerts for an alert_ignored domain, got %d", len(alerter.alerts))
	}
}
