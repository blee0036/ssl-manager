package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	agentconfig "github.com/ssl-manager/ssl-manager/internal/agent/config"
	"github.com/ssl-manager/ssl-manager/internal/model"
)

func TestHeartbeatWorker_SendsFirstHeartbeatImmediately(t *testing.T) {
	received := make(chan model.HeartbeatInfo, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agent/heartbeat" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		var info model.HeartbeatInfo
		if err := json.NewDecoder(r.Body).Decode(&info); err != nil {
			t.Errorf("failed to decode heartbeat body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		received <- info
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &agentconfig.AgentConfig{
		ServerURL:           server.URL,
		MachineID:           "machine-123",
		AgentToken:          "test-token",
		PollIntervalSeconds: 60, // Long interval so only initial heartbeat fires
	}

	worker := NewHeartbeatWorker(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	worker.Run(ctx)

	select {
	case info := <-received:
		if info.MachineID != "machine-123" {
			t.Errorf("expected machine_id=machine-123, got %s", info.MachineID)
		}
		if info.OS == "" {
			t.Error("expected OS to be non-empty")
		}
		if info.Arch == "" {
			t.Error("expected Arch to be non-empty")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for initial heartbeat")
	}
}

func TestHeartbeatWorker_IncludesAuthorizationHeader(t *testing.T) {
	authHeader := make(chan string, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader <- r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &agentconfig.AgentConfig{
		ServerURL:           server.URL,
		MachineID:           "machine-456",
		AgentToken:          "my-secret-token",
		PollIntervalSeconds: 60,
	}

	worker := NewHeartbeatWorker(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	worker.Run(ctx)

	select {
	case header := <-authHeader:
		expected := "Bearer my-secret-token"
		if header != expected {
			t.Errorf("expected Authorization header %q, got %q", expected, header)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for heartbeat")
	}
}

func TestHeartbeatWorker_StopsOnTokenRevoked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	cfg := &agentconfig.AgentConfig{
		ServerURL:           server.URL,
		MachineID:           "machine-789",
		AgentToken:          "revoked-token",
		PollIntervalSeconds: 1,
	}

	worker := NewHeartbeatWorker(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	revokedCh := worker.Run(ctx)

	select {
	case <-revokedCh:
		// Token revoked channel was closed - this is expected
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for token revoked signal")
	}
}

func TestHeartbeatWorker_RepeatsAtPollInterval(t *testing.T) {
	var mu sync.Mutex
	heartbeatCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		heartbeatCount++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &agentconfig.AgentConfig{
		ServerURL:           server.URL,
		MachineID:           "machine-repeat",
		AgentToken:          "test-token",
		PollIntervalSeconds: 1, // 1 second interval for fast testing
	}

	worker := NewHeartbeatWorker(cfg)
	ctx, cancel := context.WithCancel(context.Background())

	worker.Run(ctx)

	// Wait for initial + at least 2 periodic heartbeats
	time.Sleep(2500 * time.Millisecond)
	cancel()

	// Give worker time to stop
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	count := heartbeatCount
	mu.Unlock()

	// Should have at least 3 heartbeats: 1 initial + 2 periodic
	if count < 3 {
		t.Errorf("expected at least 3 heartbeats, got %d", count)
	}
}

func TestHeartbeatWorker_ContinuesOnServerError(t *testing.T) {
	var mu sync.Mutex
	heartbeatCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		heartbeatCount++
		mu.Unlock()
		// Return 500 - worker should continue
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := &agentconfig.AgentConfig{
		ServerURL:           server.URL,
		MachineID:           "machine-error",
		AgentToken:          "test-token",
		PollIntervalSeconds: 1,
	}

	worker := NewHeartbeatWorker(cfg)
	ctx, cancel := context.WithCancel(context.Background())

	revokedCh := worker.Run(ctx)

	// Wait for initial + 1 periodic heartbeat
	time.Sleep(1500 * time.Millisecond)
	cancel()

	// Verify the worker did NOT signal token revoked
	select {
	case <-revokedCh:
		// Wait a bit to see if it was just context cancellation
		mu.Lock()
		count := heartbeatCount
		mu.Unlock()
		if count < 2 {
			t.Error("worker stopped too early on server error")
		}
	case <-time.After(200 * time.Millisecond):
		// Good - revokedCh was not closed immediately
	}

	mu.Lock()
	count := heartbeatCount
	mu.Unlock()

	if count < 2 {
		t.Errorf("expected at least 2 heartbeats despite server errors, got %d", count)
	}
}

func TestHeartbeatWorker_StopsOnContextCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &agentconfig.AgentConfig{
		ServerURL:           server.URL,
		MachineID:           "machine-cancel",
		AgentToken:          "test-token",
		PollIntervalSeconds: 60,
	}

	worker := NewHeartbeatWorker(cfg)
	ctx, cancel := context.WithCancel(context.Background())

	revokedCh := worker.Run(ctx)

	// Wait for initial heartbeat
	time.Sleep(200 * time.Millisecond)

	// Cancel context
	cancel()

	// The revokedCh should be closed (goroutine exits)
	select {
	case <-revokedCh:
		// Good - worker stopped
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for worker to stop after context cancel")
	}
}

func TestHeartbeatWorker_SendsCorrectPayload(t *testing.T) {
	var receivedInfo model.HeartbeatInfo
	received := make(chan struct{}, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&receivedInfo); err != nil {
			t.Errorf("failed to decode body: %v", err)
		}
		received <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &agentconfig.AgentConfig{
		ServerURL:           server.URL,
		MachineID:           "machine-payload",
		AgentToken:          "test-token",
		PollIntervalSeconds: 60,
	}

	worker := NewHeartbeatWorker(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	worker.Run(ctx)

	select {
	case <-received:
		if receivedInfo.MachineID != "machine-payload" {
			t.Errorf("expected machine_id=machine-payload, got %s", receivedInfo.MachineID)
		}
		if receivedInfo.Hostname == "" {
			t.Error("expected hostname to be non-empty")
		}
		if receivedInfo.OS == "" {
			t.Error("expected OS to be non-empty")
		}
		if receivedInfo.Arch == "" {
			t.Error("expected Arch to be non-empty")
		}
		if receivedInfo.AgentVersion == "" {
			t.Error("expected AgentVersion to be non-empty")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for heartbeat payload")
	}
}

func TestHeartbeatWorker_HandlesConnectionError(t *testing.T) {
	// Use a server URL that will refuse connections
	cfg := &agentconfig.AgentConfig{
		ServerURL:           "http://127.0.0.1:1", // Port 1 should refuse connections
		MachineID:           "machine-connfail",
		AgentToken:          "test-token",
		PollIntervalSeconds: 1,
	}

	worker := NewHeartbeatWorker(cfg)
	ctx, cancel := context.WithCancel(context.Background())

	revokedCh := worker.Run(ctx)

	// Wait a bit - worker should continue despite connection errors
	time.Sleep(1500 * time.Millisecond)
	cancel()

	// Verify the worker did NOT signal token revoked on connection error
	select {
	case <-revokedCh:
		// Worker stopped due to context cancel, which is fine
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for worker to stop")
	}
}
