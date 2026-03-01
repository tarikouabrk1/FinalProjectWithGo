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

// attemptBackend tries to forward the request to the given backend within the
// specified timeout. It returns the buffered response and whether the attempt
// succeeded. Using a dedicated function means defer cancel() fires at the end
// of each attempt — not at the end of the outer Handler function — which
// prevents context/timer goroutine leaks when the retry loop runs multiple times.
func attemptBackend(r *http.Request, backend *pool.Backend, proxyTimeout time.Duration) (*httptest.ResponseRecorder, bool) {
	ctx, cancel := context.WithTimeout(r.Context(), proxyTimeout)
	defer cancel() // ✅ fires when this function returns, once per attempt

	req := r.WithContext(ctx)
	recorder := httptest.NewRecorder()

	tw := &transportWrapper{transport: http.DefaultTransport}
	rp := httputil.NewSingleHostReverseProxy(backend.URL)
	rp.Transport = tw

	rp.ServeHTTP(recorder, req)
	return recorder, !tw.failed
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
			recorder, ok := attemptBackend(r, backend, proxyTimeout)
			atomic.AddInt64(&backend.CurrentConns, -1)

			if ok {
				// Only flush the buffered response to the real writer on success
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