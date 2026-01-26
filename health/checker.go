package health

import (
	"context"
	"log"
	"net/http"
	"reverse-proxy/pool"
	"time"
	"strings"
)

func Start(serverPool *pool.ServerPool, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			backends := serverPool.GetBackends()
			
			for _, backend := range backends {
				status := checkBackend(backend.URL.String())
				previousStatus := backend.IsAlive()
				backend.SetAlive(status)
				
				// Log les changements d'état
				if previousStatus != status {
					if status {
						log.Printf("Backend %s is now UP", backend.URL.String())
					} else {
						log.Printf("Backend %s is now DOWN", backend.URL.String())
					}
				}
			}
		}
	}()
	log.Printf("Health checker started (interval: %v)", interval)
}

func checkBackend(rawURL string) bool {
	// Ensure we ping /health endpoint
	healthURL := strings.TrimSuffix(rawURL, "/") + "/health"

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", healthURL, nil)
	if err != nil {
		return false
	}

	// we can also use this resp, err := http.DefaultClient.Do(req) instead of the two following lines
	client := &http.Client{Timeout: 2 * time.Second,}
    resp, err := client.Do(req)

	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}