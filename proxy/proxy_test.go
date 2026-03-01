package proxy_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"reverse-proxy/pool"
	"reverse-proxy/proxy"
)

// Fake backend helpers

func newFakeBackend(t *testing.T, body string, code int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(code)
		w.Write([]byte(body))
	}))
}

func newSlowBackend(t *testing.T, delay time.Duration) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Respect client cancellation so the test server shuts down cleanly.
		select {
		case <-time.After(delay):
			w.Write([]byte("slow"))
		case <-r.Context().Done():
		}
	}))
}

// buildPool creates a single-backend ServerPool.
// Pass rawURL="" to get an empty pool.
func buildPool(t *testing.T, rawURL string, alive bool) *pool.ServerPool {
	t.Helper()
	sp := &pool.ServerPool{Strategy: "round-robin"}
	if rawURL == "" {
		return sp
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("invalid URL %s: %v", rawURL, err)
	}
	b := &pool.Backend{URL: u}
	b.SetAlive(alive)
	sp.AddBackend(b)
	return sp
}

// Tests

// Handler forwards the request and returns the backend's response unchanged.
func TestHandler_ForwardsToHealthyBackend(t *testing.T) {
	fake := newFakeBackend(t, "hello from backend", http.StatusOK)
	defer fake.Close()

	sp := buildPool(t, fake.URL, true)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	proxy.Handler(sp, 5*time.Second)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Body.String(); got != "hello from backend" {
		t.Errorf("unexpected body: %q", got)
	}
}

// An empty pool must return 503 immediately.
func TestHandler_EmptyPool_Returns503(t *testing.T) {
	sp := buildPool(t, "", false)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	proxy.Handler(sp, 5*time.Second)(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

// A pool whose only backend is marked dead must also return 503.
func TestHandler_AllDeadBackends_Returns503(t *testing.T) {
	fake := newFakeBackend(t, "unreachable", http.StatusOK)
	defer fake.Close()

	sp := buildPool(t, fake.URL, false) // alive = false

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	proxy.Handler(sp, 5*time.Second)(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

// When the first backend is unreachable the proxy must fail over to a healthy one
// and mark the bad backend as DOWN.
func TestHandler_FailoverToSecondBackend(t *testing.T) {
	good := newFakeBackend(t, "good backend", http.StatusOK)
	defer good.Close()

	sp := &pool.ServerPool{Strategy: "round-robin"}

	// First backend: valid URL but nothing is listening there.
	deadURL, _ := url.Parse("http://127.0.0.1:19999")
	dead := &pool.Backend{URL: deadURL}
	dead.SetAlive(true)

	goodURL, _ := url.Parse(good.URL)
	goodB := &pool.Backend{URL: goodURL}
	goodB.SetAlive(true)

	sp.AddBackend(dead)
	sp.AddBackend(goodB)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	proxy.Handler(sp, 3*time.Second)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 after failover, got %d", rec.Code)
	}
	if dead.IsAlive() {
		t.Error("unreachable backend should have been marked DOWN")
	}
}

// CurrentConns must return to zero after the request completes.
func TestHandler_ConnectionCounterReturnsToZero(t *testing.T) {
	fake := newFakeBackend(t, "ok", http.StatusOK)
	defer fake.Close()

	sp := buildPool(t, fake.URL, true)
	backend := sp.GetBackends()[0]

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	proxy.Handler(sp, 5*time.Second)(rec, req)

	if conns := atomic.LoadInt64(&backend.CurrentConns); conns != 0 {
		t.Errorf("expected CurrentConns=0 after request, got %d", conns)
	}
}

// A backend that takes longer than the proxy timeout must result in a 503.
func TestHandler_BackendTimeout_Returns503(t *testing.T) {
	slow := newSlowBackend(t, 5*time.Second)
	defer slow.Close()

	sp := buildPool(t, slow.URL, true)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	proxy.Handler(sp, 200*time.Millisecond)(rec, req)             // timeout << backend delay

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 on timeout, got %d", rec.Code)
	}
}