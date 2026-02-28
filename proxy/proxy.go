package proxy

import (
	"context"
	"net/http"
	"reverse-proxy/pool"
	"sync/atomic"
	"time"
)

// ProxyTimeout is the maximum time the proxy will wait for a backend response.
// Set high enough to accommodate slow backends 
const ProxyTimeout = 30 * time.Second

func Handler(serverPool *pool.ServerPool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		backend := serverPool.GetNextValidPeer()
		if backend == nil {
			http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
			return
		}

		atomic.AddInt64(&backend.CurrentConns, 1)
		defer atomic.AddInt64(&backend.CurrentConns, -1)

		ctx, cancel := context.WithTimeout(r.Context(), ProxyTimeout)
		defer cancel()
		r = r.WithContext(ctx)

		backend.Proxy.ServeHTTP(w, r)
	}
}