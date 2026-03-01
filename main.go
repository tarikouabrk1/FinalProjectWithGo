package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"reverse-proxy/admin"
	"reverse-proxy/health"
	"reverse-proxy/pool"
	"reverse-proxy/proxy"
	"time"
)

type Config struct {
	Port                 int      `json:"port"`
	AdminPort            int      `json:"admin_port"`
	Strategy             string   `json:"strategy"`
	HealthCheckFrequency int      `json:"health_check_frequency"`
	Backends             []string `json:"backends"`
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	err = json.Unmarshal(data, &cfg)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

func main() {
	cfg, err := loadConfig("config/config.json")
	if err != nil {
		log.Fatal("Failed to load config:", err)
	}

	// Validate the strategy
	if cfg.Strategy != "round-robin" && cfg.Strategy != "least-connections" {
		log.Fatalf("Invalid strategy: %s (must be 'round-robin' or 'least-connections')", cfg.Strategy)
	}

	serverPool := &pool.ServerPool{Strategy: cfg.Strategy}

	log.Println("Validating backends...")
	validBackendCount := 0

	for _, b := range cfg.Backends {
		u, err := url.Parse(b)
		if err != nil || u.Host == "" {
			log.Printf("Invalid backend URL: %s, skipping", b)
			continue
		}

		isAlive := health.CheckBackend(u.String())

		backend := &pool.Backend{
			URL: u,
		}
		backend.SetAlive(isAlive)

		serverPool.AddBackend(backend)

		if isAlive {
			validBackendCount++
		}
	}

	// Warn if no backend is available
	if validBackendCount == 0 {
		log.Println("WARNING: No healthy backends found! Proxy will return 503 until backends become available.")
	} else {
		log.Printf("%d/%d backends are healthy\n", validBackendCount, len(cfg.Backends))
	}

	// start health checks
	health.Start(serverPool, time.Duration(cfg.HealthCheckFrequency)*time.Second)

	// start admin API
	admin.Start(serverPool, cfg.AdminPort)

	// start the reverse proxy
	http.HandleFunc("/", proxy.Handler(serverPool))

	log.Printf("Reverse Proxy running on :%d (strategy: %s)\n", cfg.Port, cfg.Strategy)
	log.Fatal(http.ListenAndServe(
		fmt.Sprintf(":%d", cfg.Port),
		nil,
	))
}