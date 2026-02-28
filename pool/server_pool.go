package pool

import (
	"math"
	"net/http/httputil"
	"net/url"
	"sync"
	"sync/atomic"
	"net/http"
)

type Backend struct {
	URL          *url.URL
	Alive        bool
	CurrentConns int64
	Proxy        *httputil.ReverseProxy // created once, reused for every request
	mux          sync.RWMutex
}

// Thread-safe alive setter
func (b *Backend) SetAlive(alive bool) {
	b.mux.Lock()
	defer b.mux.Unlock()
	b.Alive = alive
}

func (b *Backend) IsAlive() bool {
	b.mux.RLock()
	defer b.mux.RUnlock()
	return b.Alive
}

type LoadBalancer interface {
	GetNextValidPeer() *Backend
	AddBackend(*Backend)
	SetBackendStatus(*url.URL, bool)
}

type ServerPool struct {
	Backends []*Backend
	Current  uint64
	Strategy string // "round-robin" or "least-connections"
	mux      sync.RWMutex
}

func (s *ServerPool) AddBackend(b *Backend) {
    if b.Proxy == nil {
        p := httputil.NewSingleHostReverseProxy(b.URL)
        // Set ErrorHandler once here — never touch it per-request
        p.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
            b.SetAlive(false)
            http.Error(w, "Backend down", http.StatusBadGateway)
        }
        b.Proxy = p
    }
    s.mux.Lock()
    defer s.mux.Unlock()
    s.Backends = append(s.Backends, b)
}

// GetNextValidPeer dynamically selects a backend based on the configured strategy
func (s *ServerPool) GetNextValidPeer() *Backend {
	s.mux.RLock()
	defer s.mux.RUnlock()

	if s.Strategy == "least-connections" {
		var best *Backend
		minConns := int64(math.MaxInt64)
		for _, b := range s.Backends {
			conns := atomic.LoadInt64(&b.CurrentConns)
			if b.IsAlive() && conns < minConns {
				best = b
				minConns = conns
			}
		}
		return best
	}

	// Default: Round-Robin
	length := len(s.Backends)
	if length == 0 {
		return nil
	}

	start := atomic.AddUint64(&s.Current, 1) % uint64(length)
	for i := 0; i < length; i++ {
		idx := (start + uint64(i)) % uint64(length)
		if s.Backends[idx].IsAlive() {
			return s.Backends[idx]
		}
	}
	return nil
}

func (s *ServerPool) SetBackendStatus(u *url.URL, alive bool) {
	s.mux.Lock()
	defer s.mux.Unlock()

	for _, b := range s.Backends {
		if b.URL.String() == u.String() {
			b.SetAlive(alive)
			return
		}
	}
}

func (s *ServerPool) RemoveBackend(u *url.URL) bool {
	s.mux.Lock()
	defer s.mux.Unlock()

	for i, b := range s.Backends {
		if b.URL.String() == u.String() {
			s.Backends = append(s.Backends[:i], s.Backends[i+1:]...)
			return true
		}
	}
	return false
}

func (s *ServerPool) GetBackends() []*Backend {
	s.mux.RLock()
	defer s.mux.RUnlock()
	return append([]*Backend(nil), s.Backends...)
}