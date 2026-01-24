package pool

import (
	"net/url"
	"sync"
	"sync/atomic"
	"math"
)

type Backend struct {
	URL          *url.URL `json:"url"`
	Alive        bool     `json:"alive"`
	CurrentConns int64    `json:"current_connections"`
	mux          sync.RWMutex
}

// Méthodes thread-safe
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
	Strategy string         // "round-robin" or "least-connections"
	mux      sync.RWMutex
}


func (s *ServerPool) AddBackend(b *Backend) {
	s.mux.Lock()
	defer s.mux.Unlock()
	s.Backends = append(s.Backends, b)
}

// GetNextValidPeer dynamically selects based on strategy
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

	// Increment ONCE and get the starting index
	start := atomic.AddUint64(&s.Current, 1) % uint64(length)

	// Try backends starting from 'start' position
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
			// remove element i
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
