package proxy

import (
	"context"
	"log"
	"net/http"
	"reverse-proxy/pool"
	"sync/atomic"
	"time"
)

// Handler returns an HTTP handler that forwards requests to a healthy backend.
// It retries with the next available peer if the chosen backend fails mid-request,
// marking the failed backend as dead immediately so the health checker picks it up.
func Handler(serverPool pool.LoadBalancer, proxyTimeout time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		backends := serverPool.GetBackends()
		maxAttempts := len(backends)
		if maxAttempts == 0 {
			http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
			return
		}

		for attempt := 0; attempt < maxAttempts; attempt++ {
			backend := serverPool.GetNextValidPeer()
			if backend == nil {
				// No alive backend left
				break
			}

			// Track active connections for least-connections balancing
			atomic.AddInt64(&backend.CurrentConns, 1)

			// Wrap request context with a timeout so slow backends don't block forever.
			// If the client disconnects first, r.Context() is already canceled and this
			// context will inherit that cancellation immediately.
			ctx, cancel := context.WithTimeout(r.Context(), proxyTimeout)
			req := r.WithContext(ctx)

			// failed is set to true inside the ErrorHandler closure when the backend
			// returns a connection-level error (refused, reset, timeout, etc.)
			failed := false

			backend.SetErrorHandler(func(w http.ResponseWriter, r *http.Request, err error) {
				log.Printf("Backend %s error: %v — marking as DOWN and retrying", backend.URL, err)
				backend.SetAlive(false)
				failed = true
				// Do NOT write to w here; we'll either retry or send 503 below.
			})

			backend.Proxy.ServeHTTP(w, req)

			atomic.AddInt64(&backend.CurrentConns, -1)
			cancel()

			if !failed {
				// Request completed successfully
				return
			}

			// Backend failed: loop and try the next peer
		}

		// All attempts exhausted
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
	}
}