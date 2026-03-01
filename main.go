package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"reverse-proxy/admin"
	"reverse-proxy/health"
	"reverse-proxy/pool"
	"reverse-proxy/proxy"
	"syscall"
	"time"
)

type Config struct {
	Port                 int      `json:"port"`
	AdminPort            int      `json:"admin_port"`
	Strategy             string   `json:"strategy"`
	HealthCheckFrequency int      `json:"health_check_frequency"`
	ProxyTimeout         int      `json:"proxy_timeout"` // seconds; defaults to 30 if omitted
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

	// Apply sensible defaults
	if cfg.ProxyTimeout <= 0 {
		cfg.ProxyTimeout = 30
	}
	if cfg.HealthCheckFrequency <= 0 {
		cfg.HealthCheckFrequency = 10
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
			log.Printf("✓ Backend %s is healthy", u.String())
		} else {
			log.Printf("✗ Backend %s is unreachable", u.String())
		}
	}

	if validBackendCount == 0 {
		log.Println("WARNING: No healthy backends found! Proxy will return 503 until backends become available.")
	} else {
		log.Printf("%d/%d backends are healthy\n", validBackendCount, len(cfg.Backends))
	}

	// Start background health checker
	health.Start(serverPool, time.Duration(cfg.HealthCheckFrequency)*time.Second)

	// Start admin API (runs in its own goroutine internally)
	admin.Start(serverPool, cfg.AdminPort)

	// Build the main proxy server
	proxyTimeout := time.Duration(cfg.ProxyTimeout) * time.Second
	mux := http.NewServeMux()
	mux.HandleFunc("/", proxy.Handler(serverPool, proxyTimeout))

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: mux,
	}

	// Start proxy in background goroutine so we can listen for shutdown signals
	go func() {
		log.Printf("Reverse Proxy running on :%d (strategy: %s, proxy timeout: %ds)\n",
			cfg.Port, cfg.Strategy, cfg.ProxyTimeout)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Proxy server error: %v", err)
		}
	}()

	// Graceful shutdown 
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutdown signal received — draining in-flight requests (up to 10s)...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Forced shutdown due to timeout: %v", err)
	}

	log.Println("Server stopped cleanly.")
}