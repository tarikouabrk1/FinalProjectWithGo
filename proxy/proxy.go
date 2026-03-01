package proxy

import (
	"context"
	"log"
	"net/http"
	"net/http/httputil"
	"reverse-proxy/pool"
	"sync/atomic"
	"time"
)

// transportWrapper wraps http.DefaultTransport and records whether the
// RoundTrip call failed with a connection-level error (refused, reset, EOF).
// A new instance is created per request attempt — zero shared state between
// concurrent goroutines.
type transportWrapper struct {
	transport http.RoundTripper
	failed    bool
}

func (t *transportWrapper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.transport.RoundTrip(req)
	if err != nil {
		t.failed = true
	}
	return resp, err
}

// Handler returns an http.HandlerFunc that forwards requests to a healthy backend.
//
// Retry logic: if the selected backend fails with a connection-level error
// (refused, reset, EOF, timeout), it is immediately marked as DOWN and the
// next available peer is tried up to len(backends) times total.
// A 503 is returned only when all attempts are exhausted.
//
// Thread safety: a fresh httputil.ReverseProxy and transportWrapper are built
// per attempt so concurrent requests never share mutable per-request state.
func Handler(serverPool pool.LoadBalancer, proxyTimeout time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		maxAttempts := len(serverPool.GetBackends())
		if maxAttempts == 0 {
			http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
			return
		}

		for attempt := 0; attempt < maxAttempts; attempt++ {
			backend := serverPool.GetNextValidPeer()
			if backend == nil {
				break // no alive peers left
			}

			// Increment connection counter for least-connections balancing.
			atomic.AddInt64(&backend.CurrentConns, 1)

			// Honour the client context AND enforce a per-request timeout.
			// If the client disconnects, r.Context() cancels immediately.
			ctx, cancel := context.WithTimeout(r.Context(), proxyTimeout)
			req := r.WithContext(ctx)

			// Fresh transport per attempt — avoids any shared mutable state
			// between concurrent goroutines hitting the same backend.
			tw := &transportWrapper{transport: http.DefaultTransport}
			rp := httputil.NewSingleHostReverseProxy(backend.URL)
			rp.Transport = tw

			rp.ServeHTTP(w, req)
			atomic.AddInt64(&backend.CurrentConns, -1)
			cancel()

			if !tw.failed {
				return // success
			}

			// Connection-level failure: mark backend dead and try the next peer.
			log.Printf("Backend %s error — marking DOWN, retrying (attempt %d/%d)",
				backend.URL, attempt+1, maxAttempts)
			backend.SetAlive(false)
		}

		// Every peer was tried and failed (or no peers available).
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
	}
}