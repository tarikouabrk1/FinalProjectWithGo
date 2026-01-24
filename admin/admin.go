package admin

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"reverse-proxy/pool"
	"sync/atomic"
)

type BackendStatus struct {
	URL          string `json:"url"`
	Alive        bool   `json:"alive"`
	CurrentConns int64  `json:"current_connections"`
}

type StatusResponse struct {
	TotalBackends  int             `json:"total_backends"`
	ActiveBackends int             `json:"active_backends"`
	Backends       []BackendStatus `json:"backends"`
}

func Start(serverPool *pool.ServerPool, port int) {
	//Créer un ServeMux dédié pour isoler l'admin
	adminMux := http.NewServeMux()

	// ---------- STATUS ----------
	adminMux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		backends := serverPool.GetBackends()

		resp := StatusResponse{
			TotalBackends: len(backends),
		}

		for _, b := range backends {
			if b.IsAlive() {
				resp.ActiveBackends++
			}

			resp.Backends = append(resp.Backends, BackendStatus{
				URL:          b.URL.String(),
				Alive:        b.IsAlive(),
				CurrentConns: atomic.LoadInt64(&b.CurrentConns), 

			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// ---------- BACKENDS MANAGEMENT ----------
	adminMux.HandleFunc("/backends", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			URL string `json:"url"`
		}

		switch r.Method {

		case http.MethodPost:
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "Invalid JSON", http.StatusBadRequest)
				return
			}

			parsedURL, err := url.Parse(body.URL)
			if err != nil || parsedURL.Host == "" {
				http.Error(w, "Invalid URL", http.StatusBadRequest)
				return
			}

			//check if the backend already exists
			for _, b := range serverPool.GetBackends() {
				if b.URL.String() == parsedURL.String() {
					http.Error(w, "Backend already exists", http.StatusConflict)
					return
				}
			}

			serverPool.AddBackend(&pool.Backend{
				URL:   parsedURL,
				Alive: true,
			})

			log.Printf("Backend added: %s", parsedURL.String())
			w.WriteHeader(http.StatusCreated)

		case http.MethodDelete:
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "Invalid JSON", http.StatusBadRequest)
				return
			}

			parsedURL, err := url.Parse(body.URL)
			if err != nil || parsedURL.Host == "" {
				http.Error(w, "Invalid URL", http.StatusBadRequest)
				return
			}

			if !serverPool.RemoveBackend(parsedURL) {
				http.Error(w, "Backend not found", http.StatusNotFound)
				return
			}

			log.Printf("Backend removed: %s", parsedURL.String())
			w.WriteHeader(http.StatusNoContent)

		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// ---------- START ADMIN SERVER ----------
	log.Printf("Admin API running on :%d\n", port)
	go func() {
		if err := http.ListenAndServe(fmt.Sprintf(":%d", port), adminMux); err != nil {
			log.Printf("Admin server error: %v", err)
		}
	}()
}