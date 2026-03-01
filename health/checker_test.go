package health_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"reverse-proxy/health"
	"reverse-proxy/pool"
)

// CheckBackend
// A server that replies 200 on /health must be considered alive.
func TestCheckBackend_Healthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	if !health.CheckBackend(srv.URL) {
		t.Error("expected healthy backend to return true")
	}
}

// A server that replies non-200 on /health must be considered dead.
func TestCheckBackend_UnhealthyStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	if health.CheckBackend(srv.URL) {
		t.Error("expected 500 response to return false")
	}
}

// A URL with nothing listening must return false (connection refused).
func TestCheckBackend_Unreachable(t *testing.T) {
	if health.CheckBackend("http://127.0.0.1:19998") {
		t.Error("expected unreachable server to return false")
	}
}

// ── health.Start integration
// Start should flip a backend from DOWN to UP once a healthy /health endpoint
// becomes reachable within the check interval.
func TestStart_MarksBackendAlive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	sp := &pool.ServerPool{Strategy: "round-robin"}
	u, _ := url.Parse(srv.URL)
	b := &pool.Backend{URL: u}
	b.SetAlive(false)                                       // starts dead
	sp.AddBackend(b)

	health.Start(sp, 100*time.Millisecond) 

	// Wait up to 1 second for the health checker to flip the backend UP.
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if b.IsAlive() {
			return // 
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Error("backend was not marked alive within 1 second")
}

// Start should flip a backend from UP to DOWN when the /health endpoint fails.
func TestStart_MarksBackendDead(t *testing.T) {
	// Start a server we can close on demand.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	sp := &pool.ServerPool{Strategy: "round-robin"}
	u, _ := url.Parse(srv.URL)
	b := &pool.Backend{URL: u}
	b.SetAlive(true)                                 // starts alive
	sp.AddBackend(b)

	health.Start(sp, 100*time.Millisecond)

	// Close the server — next health check should mark the backend DOWN.
	srv.Close()

	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if !b.IsAlive() {
			return // 
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Error("backend was not marked dead within 1 second after server closed")
}