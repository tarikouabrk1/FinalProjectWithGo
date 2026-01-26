package proxy

import (
	"context"
	"net/http"
	"net/http/httputil"
	"reverse-proxy/pool"
	"sync/atomic"
	"time"
)

func Handler(serverPool *pool.ServerPool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		backend := serverPool.GetNextValidPeer()
		if backend == nil {
			http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
			return
		}

		// Increment connection safely
		atomic.AddInt64(&backend.CurrentConns, 1)
		defer atomic.AddInt64(&backend.CurrentConns, -1)
		
		// Set a timeout context (5s) and propagate client cancellation
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		r = r.WithContext(ctx)

		proxy := httputil.NewSingleHostReverseProxy(backend.URL)

		proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			backend.SetAlive(false)
			http.Error(w, "Backend down", http.StatusBadGateway)
		}

		proxy.ServeHTTP(w, r)
	}
}
