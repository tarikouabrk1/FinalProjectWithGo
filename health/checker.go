package health

import (
	"context"
	"log"
	"net/http"
	"reverse-proxy/pool"
	"strings"
	"time"
)

// Start launches a background goroutine that pings every backend at the given interval.
// State transitions (UP→DOWN, DOWN→UP) are logged and applied via the LoadBalancer interface.
func Start(serverPool pool.LoadBalancer, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			backends := serverPool.GetBackends()
			for _, backend := range backends {
				newStatus := CheckBackend(backend.URL.String())
				previousStatus := backend.IsAlive()

				if previousStatus != newStatus {
					// Route state mutation through the interface (consistent & testable)
					serverPool.SetBackendStatus(backend.URL, newStatus)

					if newStatus {
						log.Printf("✓ Backend %s is now UP", backend.URL.String())
					} else {
						log.Printf("✗ Backend %s is now DOWN", backend.URL.String())
					}
				}
			}
		}
	}()
	log.Printf("Health checker started (interval: %v)", interval)
}

// CheckBackend performs a GET request to <url>/health and returns true if the
// response status is 200 OK within a 2-second timeout.
func CheckBackend(rawURL string) bool {
	healthURL := strings.TrimSuffix(rawURL, "/") + "/health"

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return false
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}