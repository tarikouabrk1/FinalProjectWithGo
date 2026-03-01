package health

import (
	"context"
	"log"
	"net/http"
	"reverse-proxy/pool"
	"strings"
	"time"
)

func Start(serverPool pool.LoadBalancer, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			backends := serverPool.GetBackends()
			for _, backend := range backends {
				status := CheckBackend(backend.URL.String())
				previousStatus := backend.IsAlive()
				backend.SetAlive(status)

				// Only log on state change to avoid noise
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

func CheckBackend(rawURL string) bool {
	healthURL := strings.TrimSuffix(rawURL, "/") + "/health"

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", healthURL, nil)
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