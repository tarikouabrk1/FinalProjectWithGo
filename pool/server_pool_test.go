package pool

import (
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
)

// helper: build a Backend with a given URL and alive status
func newBackend(rawURL string, alive bool) *Backend {
	u, _ := url.Parse(rawURL)
	b := &Backend{URL: u}
	b.SetAlive(alive)
	return b
}

// ── Round-Robin ──────────────────────────────────────────────────────────────

func TestRoundRobin_CyclesAcrossAliveBackends(t *testing.T) {
	pool := &ServerPool{Strategy: "round-robin"}
	pool.AddBackend(newBackend("http://a:8080", true))
	pool.AddBackend(newBackend("http://b:8080", true))
	pool.AddBackend(newBackend("http://c:8080", true))

	seen := map[string]int{}
	for i := 0; i < 9; i++ {
		b := pool.GetNextValidPeer()
		if b == nil {
			t.Fatal("expected a backend, got nil")
		}
		seen[b.URL.Host]++
	}

	// Each backend should be chosen exactly 3 times in 9 calls
	for host, count := range seen {
		if count != 3 {
			t.Errorf("backend %s selected %d times, expected 3", host, count)
		}
	}
}

func TestRoundRobin_SkipsDeadBackends(t *testing.T) {
	pool := &ServerPool{Strategy: "round-robin"}
	pool.AddBackend(newBackend("http://alive:8080", true))
	pool.AddBackend(newBackend("http://dead:8080", false))

	for i := 0; i < 6; i++ {
		b := pool.GetNextValidPeer()
		if b == nil {
			t.Fatal("expected a backend, got nil")
		}
		if b.URL.Host == "dead:8080" {
			t.Errorf("round-robin returned a dead backend on call %d", i)
		}
	}
}

func TestRoundRobin_AllDead_ReturnsNil(t *testing.T) {
	pool := &ServerPool{Strategy: "round-robin"}
	pool.AddBackend(newBackend("http://a:8080", false))
	pool.AddBackend(newBackend("http://b:8080", false))

	if b := pool.GetNextValidPeer(); b != nil {
		t.Errorf("expected nil, got %s", b.URL)
	}
}

func TestRoundRobin_EmptyPool_ReturnsNil(t *testing.T) {
	pool := &ServerPool{Strategy: "round-robin"}
	if b := pool.GetNextValidPeer(); b != nil {
		t.Errorf("expected nil for empty pool, got %s", b.URL)
	}
}

// ── Least-Connections ────────────────────────────────────────────────────────

func TestLeastConn_PrefersLowestConnections(t *testing.T) {
	pool := &ServerPool{Strategy: "least-connections"}
	low := newBackend("http://low:8080", true)
	high := newBackend("http://high:8080", true)

	atomic.StoreInt64(&low.CurrentConns, 1)
	atomic.StoreInt64(&high.CurrentConns, 10)

	pool.AddBackend(low)
	pool.AddBackend(high)

	b := pool.GetNextValidPeer()
	if b == nil || b.URL.Host != "low:8080" {
		t.Errorf("expected low-conn backend, got %v", b)
	}
}

func TestLeastConn_SkipsDeadBackends(t *testing.T) {
	pool := &ServerPool{Strategy: "least-connections"}
	dead := newBackend("http://dead:8080", false)
	alive := newBackend("http://alive:8080", true)

	atomic.StoreInt64(&dead.CurrentConns, 0) // even at 0 conns, dead must be skipped
	atomic.StoreInt64(&alive.CurrentConns, 5)

	pool.AddBackend(dead)
	pool.AddBackend(alive)

	b := pool.GetNextValidPeer()
	if b == nil || b.URL.Host != "alive:8080" {
		t.Errorf("expected alive backend, got %v", b)
	}
}

// ── SetBackendStatus & RemoveBackend ─────────────────────────────────────────

func TestSetBackendStatus_UpdatesAliveFlag(t *testing.T) {
	p := &ServerPool{Strategy: "round-robin"}
	u, _ := url.Parse("http://target:8080")
	b := newBackend("http://target:8080", true)
	p.AddBackend(b)

	p.SetBackendStatus(u, false)

	if b.IsAlive() {
		t.Error("expected backend to be marked dead")
	}

	p.SetBackendStatus(u, true)
	if !b.IsAlive() {
		t.Error("expected backend to be revived")
	}
}

func TestRemoveBackend_RemovesCorrectEntry(t *testing.T) {
	p := &ServerPool{Strategy: "round-robin"}
	p.AddBackend(newBackend("http://keep:8080", true))
	p.AddBackend(newBackend("http://remove:8080", true))

	u, _ := url.Parse("http://remove:8080")
	removed := p.RemoveBackend(u)
	if !removed {
		t.Fatal("expected RemoveBackend to return true")
	}
	if len(p.Backends) != 1 {
		t.Errorf("expected 1 backend after removal, got %d", len(p.Backends))
	}
	if p.Backends[0].URL.Host != "keep:8080" {
		t.Errorf("wrong backend kept: %s", p.Backends[0].URL.Host)
	}
}

func TestRemoveBackend_NonExistent_ReturnsFalse(t *testing.T) {
	p := &ServerPool{Strategy: "round-robin"}
	u, _ := url.Parse("http://ghost:8080")
	if p.RemoveBackend(u) {
		t.Error("expected false for non-existent backend")
	}
}

// ── Concurrent access (race detector) ────────────────────────────────────────

func TestConcurrentAccess_NoRace(t *testing.T) {
	p := &ServerPool{Strategy: "round-robin"}
	p.AddBackend(newBackend("http://a:8080", true))
	p.AddBackend(newBackend("http://b:8080", true))

	var wg sync.WaitGroup
	// 50 goroutines reading, 10 writing — run with: go test -race ./pool/...
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.GetNextValidPeer()
		}()
	}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			u, _ := url.Parse("http://a:8080")
			p.SetBackendStatus(u, i%2 == 0)
		}(i)
	}
	wg.Wait()
}