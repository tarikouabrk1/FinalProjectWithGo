package proxy

import (
	"context"
	"net/http"
	"reverse-proxy/pool"
	"sync/atomic"
	"time"
)

const ProxyTimeout = 30 * time.Second

func Handler(serverPool pool.LoadBalancer) http.HandlerFunc {
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