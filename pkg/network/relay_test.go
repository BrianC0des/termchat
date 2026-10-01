package network

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPreWarmRelay(t *testing.T) {
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/health") {
			atomic.AddInt32(&hits, 1)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
		}
	}))
	defer server.Close()

	// 1. Standard HTTP URL
	PreWarmRelay(server.URL)

	// 2. WebSocket URL prefix
	wsURL := "ws://" + strings.TrimPrefix(server.URL, "http://") + "/ws"
	PreWarmRelay(wsURL)

	// Wait briefly for the goroutines to fire
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&hits) >= 2 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if got := atomic.LoadInt32(&hits); got < 2 {
		t.Fatalf("expected at least 2 hits on /health, got %d", got)
	}
}

func TestConnectRelay_GenerationAndStatus(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var statuses []string
	var mu sync.Mutex

	events := NetworkEvents{
		OnRelayStatus: func(status string, connected bool) {
			mu.Lock()
			statuses = append(statuses, status)
			mu.Unlock()
		},
	}

	mgr := &Manager{
		LocalID:   "test-local-id",
		LocalName: "Tester",
		ctx:       ctx,
		cancel:    cancel,
		events:    events,
	}

	// Dial an invalid local port so it triggers backoff and status reports
	mgr.ConnectRelay("ws://127.0.0.1:54321/ws", "alpha-room")

	// Verify room and initial generation
	mgr.relayGenMu.Lock()
	gen1 := mgr.relayGen
	mgr.relayGenMu.Unlock()
	if gen1 == 0 {
		t.Fatalf("expected relayGen > 0, got %d", gen1)
	}
	if mgr.RoomName != "alpha-room" {
		t.Fatalf("expected room name alpha-room, got %s", mgr.RoomName)
	}

	// Wait briefly for the initial status report
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		count := len(statuses)
		mu.Unlock()
		if count > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	mu.Lock()
	if len(statuses) == 0 {
		mu.Unlock()
		t.Fatalf("expected at least 1 status notification, got none")
	}
	firstStatus := statuses[0]
	mu.Unlock()

	if !strings.Contains(firstStatus, "Connecting to cloud room #alpha-room") && !strings.Contains(firstStatus, "Waking up relay") {
		t.Errorf("unexpected status string: %s", firstStatus)
	}

	// Now switch room to beta-room: generation must increment and invalidate old dial loop
	mgr.ConnectRelay("ws://127.0.0.1:54321/ws", "beta-room")
	mgr.relayGenMu.Lock()
	gen2 := mgr.relayGen
	mgr.relayGenMu.Unlock()

	if gen2 <= gen1 {
		t.Fatalf("expected gen2 (%d) > gen1 (%d)", gen2, gen1)
	}
	if mgr.RoomName != "beta-room" {
		t.Fatalf("expected room name beta-room, got %s", mgr.RoomName)
	}

	// Now leave room: generation increments again, RoomName cleared
	mgr.LeaveRoom()
	mgr.relayGenMu.Lock()
	gen3 := mgr.relayGen
	mgr.relayGenMu.Unlock()

	if gen3 <= gen2 {
		t.Fatalf("expected gen3 (%d) > gen2 (%d)", gen3, gen2)
	}
	if mgr.RoomName != "" {
		t.Fatalf("expected empty RoomName after LeaveRoom, got %s", mgr.RoomName)
	}
}
