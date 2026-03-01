package pool

import (
	"math"
	"net/url"
	"sync"
	"sync/atomic"
)

// Backend represents a single upstream server.
type Backend struct {
	URL          *url.URL
	alive        bool
	CurrentConns int64 // tracked atomically for least-connections balancing
	mux          sync.RWMutex
}

func (b *Backend) SetAlive(alive bool) {
	b.mux.Lock()
	defer b.mux.Unlock()
	b.alive = alive
}

func (b *Backend) IsAlive() bool {
	b.mux.RLock()
	defer b.mux.RUnlock()
	return b.alive
}

// LoadBalancer abstracts selection and management of backend servers.
type LoadBalancer interface {
	GetNextValidPeer() *Backend
	AddBackend(*Backend)
	GetBackends() []*Backend
	RemoveBackend(*url.URL) bool
	SetBackendStatus(*url.URL, bool)
}

// ServerPool holds the list of backends and the chosen load-balancing strategy.
type ServerPool struct {
	Backends []*Backend
	Current  uint64 // atomic counter for round-robin
	Strategy string // "round-robin" | "least-connections"
	mux      sync.RWMutex
}

// AddBackend registers a new backend in the pool.
func (s *ServerPool) AddBackend(b *Backend) {
	s.mux.Lock()
	defer s.mux.Unlock()
	s.Backends = append(s.Backends, b)
}

// GetNextValidPeer returns the next alive backend using the configured strategy.
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
	start := (atomic.AddUint64(&s.Current, 1) - 1) % uint64(length)
	for i := 0; i < length; i++ {
		idx := (start + uint64(i)) % uint64(length)
		if s.Backends[idx].IsAlive() {
			return s.Backends[idx]
		}
	}
	return nil
}

// SetBackendStatus updates the alive flag of the backend matching the given URL.
func (s *ServerPool) SetBackendStatus(u *url.URL, alive bool) {
	s.mux.Lock()            
	defer s.mux.Unlock()
	for _, b := range s.Backends {
		if b.URL.String() == u.String() {
			b.SetAlive(alive) // backend's own mux handles its field
			return
		}
	}
}

// RemoveBackend removes the backend with the given URL from the pool.
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

// GetBackends returns a snapshot copy of the backend slice (safe for concurrent iteration).
func (s *ServerPool) GetBackends() []*Backend {
	s.mux.RLock()
	defer s.mux.RUnlock()
	return append([]*Backend(nil), s.Backends...)
}