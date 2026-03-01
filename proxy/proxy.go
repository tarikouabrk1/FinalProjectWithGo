package proxy

import (
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"reverse-proxy/pool"
	"sync/atomic"
	"time"
)

// transportWrapper wraps http.DefaultTransport and records whether the
// RoundTrip call failed with a connection-level error.
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
				break
			}

			atomic.AddInt64(&backend.CurrentConns, 1)

			ctx, cancel := context.WithTimeout(r.Context(), proxyTimeout)
			req := r.WithContext(ctx)

			// Buffer into a recorder — never touch the real writer until success
			recorder := httptest.NewRecorder()

			tw := &transportWrapper{transport: http.DefaultTransport}
			rp := httputil.NewSingleHostReverseProxy(backend.URL)
			rp.Transport = tw

			rp.ServeHTTP(recorder, req)
			atomic.AddInt64(&backend.CurrentConns, -1)
			cancel()

			if !tw.failed {
				// Only now flush the buffered response to the real writer
				for key, vals := range recorder.Header() {
					for _, val := range vals {
						w.Header().Add(key, val)
					}
				}
				w.WriteHeader(recorder.Code)
				recorder.Body.WriteTo(w)
				return
			}

			log.Printf("Backend %s error — marking DOWN, retrying (attempt %d/%d)",
				backend.URL, attempt+1, maxAttempts)
			backend.SetAlive(false)
		}

		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
	}
}