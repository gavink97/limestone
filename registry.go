package limestone

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type member struct {
	ID         string
	Dial       string
	Healthy    bool
	Draining   bool
	LastChange time.Time
}

type MemberInfo struct {
	ID         string
	Dial       string
	Healthy    bool
	Draining   bool
	LastChange time.Time
}

type Registry struct {
	mu     sync.Mutex
	cond   *sync.Cond
	pools  map[string]map[string]*member
	infl   map[string]*atomic.Int64
	demand chan string
}

func NewRegistry(demand chan string) *Registry {
	r := &Registry{
		pools:  make(map[string]map[string]*member),
		infl:   make(map[string]*atomic.Int64),
		demand: demand,
	}
	r.cond = sync.NewCond(&r.mu)
	return r
}

func (r *Registry) AddMember(pool, id, dial string, healthy bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	p := r.pools[pool]
	if p == nil {
		p = make(map[string]*member)
		r.pools[pool] = p
	}

	p[id] = &member{ID: id, Dial: dial, Healthy: healthy, LastChange: time.Now()}
	r.cond.Broadcast()
}

func (r *Registry) SetHealth(pool, id string, healthy bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	m, ok := r.pools[pool][id]
	if !ok || m.Healthy == healthy {
		return
	}

	m.Healthy = healthy
	m.LastChange = time.Now()
	r.cond.Broadcast()
}

func (r *Registry) MarkDraining(pool, id string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if m, ok := r.pools[pool][id]; ok {
		m.Draining = true
		m.LastChange = time.Now()
		r.cond.Broadcast()
	}
}

func (r *Registry) RemoveMember(pool, id string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if p := r.pools[pool]; p != nil {
		delete(p, id)
	}

	r.cond.Broadcast()
}

func (r *Registry) Members(pool string) []MemberInfo {
	r.mu.Lock()
	defer r.mu.Unlock()

	var out []MemberInfo
	for _, m := range r.pools[pool] {
		out = append(out, MemberInfo{m.ID, m.Dial, m.Healthy, m.Draining, m.LastChange})
	}

	return out
}

func (r *Registry) healthyLocked(pool string) []string {
	var dials []string

	for _, m := range r.pools[pool] {
		if m.Healthy && !m.Draining {
			dials = append(dials, m.Dial)
		}
	}

	return dials
}

func (r *Registry) HealthyDials(pool string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.healthyLocked(pool)
}

func (r *Registry) HealthyCount(pool string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.healthyLocked(pool))
}

// Inflight returns the per-pool request counter. Waiting-for-capacity
func (r *Registry) Inflight(pool string) *atomic.Int64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	g := r.infl[pool]
	if g == nil {
		g = &atomic.Int64{}
		r.infl[pool] = g
	}

	return g
}

func (r *Registry) Nudge(pool string) {
	select {
	case r.demand <- pool:
	default:
	}
}

func (r *Registry) WaitHealthy(ctx context.Context, pool string, max time.Duration) error {
	deadline := time.Now().Add(max)
	r.mu.Lock()
	defer r.mu.Unlock()

	for len(r.healthyLocked(pool)) == 0 {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for capacity in pool %q", pool)
		}

		r.cond.Wait()
	}
	return nil
}

func (r *Registry) Broadcast() {
	r.mu.Lock()
	r.cond.Broadcast()
	r.mu.Unlock()
}
