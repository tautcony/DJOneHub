package snapshot

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type Name string
type InvalidationReason string
type Outcome string

const (
	Hit       Outcome = "hit"
	Miss      Outcome = "miss"
	Stale     Outcome = "stale"
	Coalesced Outcome = "coalesced"
)

type Policy struct {
	Name        Name
	TTL         time.Duration
	LoadTimeout time.Duration
}

type Scope struct {
	Generation uint64
	Epoch      uint64
}

type CloneFunc[T any] func(T) T
type ParentContext func() context.Context

type result[T any] struct {
	value T
	err   error
}

type flight[T any] struct {
	done       chan struct{}
	cacheEpoch uint64
	result[T]
}

type flightScope struct {
	scope      Scope
	cacheEpoch uint64
}

// Snapshot stores one successful immutable value for a generation and epoch.
type Snapshot[T any] struct {
	mu         sync.Mutex
	policy     Policy
	parent     ParentContext
	clone      CloneFunc[T]
	epoch      uint64
	value      T
	scope      Scope
	valueEpoch uint64
	loadedAt   time.Time
	valid      bool
	flights    map[flightScope]*flight[T]
	last       InvalidationReason
	outcomes   map[Outcome]uint64
}

func New[T any](policy Policy, parent ParentContext, clone CloneFunc[T]) *Snapshot[T] {
	if policy.Name == "" {
		panic("snapshot name is required")
	}
	if policy.TTL <= 0 {
		panic("snapshot TTL must be positive")
	}
	if policy.LoadTimeout <= 0 {
		panic("snapshot load timeout must be positive")
	}
	if parent == nil {
		panic("snapshot parent context is required")
	}
	if clone == nil {
		clone = func(value T) T { return value }
	}
	return &Snapshot[T]{policy: policy, parent: parent, clone: clone, flights: make(map[flightScope]*flight[T]), outcomes: make(map[Outcome]uint64)}
}

func (s *Snapshot[T]) Policy() Policy { return s.policy }

type Summary struct {
	Name          Name               `json:"name"`
	TTLMS         int64              `json:"ttl_ms"`
	LoadTimeoutMS int64              `json:"load_timeout_ms"`
	Outcomes      map[Outcome]uint64 `json:"outcomes"`
}

func (s *Snapshot[T]) Summary() Summary {
	s.mu.Lock()
	defer s.mu.Unlock()
	outcomes := map[Outcome]uint64{Hit: 0, Miss: 0, Stale: 0, Coalesced: 0}
	for outcome, count := range s.outcomes {
		outcomes[outcome] = count
	}
	return Summary{Name: s.policy.Name, TTLMS: s.policy.TTL.Milliseconds(), LoadTimeoutMS: s.policy.LoadTimeout.Milliseconds(), Outcomes: outcomes}
}

func (s *Snapshot[T]) Get(ctx context.Context, scope Scope, load func(context.Context) (T, error)) (T, Outcome, error) {
	now := time.Now()
	s.mu.Lock()
	key := flightScope{scope: scope, cacheEpoch: s.epoch}
	if s.valid && s.scope == scope && s.valueEpoch == s.epoch && now.Sub(s.loadedAt) < s.policy.TTL {
		s.outcomes[Hit]++
		value := s.clone(s.value)
		s.mu.Unlock()
		return value, Hit, nil
	}
	outcome := Miss
	if s.valid {
		outcome = Stale
	}
	if active := s.flights[key]; active != nil {
		s.outcomes[Coalesced]++
		s.mu.Unlock()
		return s.wait(ctx, active, Coalesced)
	}
	active := &flight[T]{done: make(chan struct{}), cacheEpoch: s.epoch}
	s.flights[key] = active
	s.outcomes[outcome]++
	s.mu.Unlock()

	go s.runLoad(scope, active, load)
	return s.wait(ctx, active, outcome)
}

func (s *Snapshot[T]) wait(ctx context.Context, active *flight[T], outcome Outcome) (T, Outcome, error) {
	select {
	case <-active.done:
		return s.clone(active.value), outcome, active.err
	case <-ctx.Done():
		var zero T
		return zero, outcome, ctx.Err()
	}
}

func (s *Snapshot[T]) runLoad(scope Scope, active *flight[T], load func(context.Context) (T, error)) {
	parent := s.parent()
	if parent == nil {
		parent = cancelledContext()
	}
	loadCtx, cancel := context.WithTimeout(parent, s.policy.LoadTimeout)
	value, err := load(loadCtx)
	cancel()

	s.mu.Lock()
	active.value, active.err = value, err
	delete(s.flights, flightScope{scope: scope, cacheEpoch: active.cacheEpoch})
	if err == nil && active.cacheEpoch == s.epoch {
		s.value = s.clone(value)
		s.scope = scope
		s.valueEpoch = active.cacheEpoch
		s.loadedAt = time.Now()
		s.valid = true
	}
	close(active.done)
	s.mu.Unlock()
}

func (s *Snapshot[T]) Peek(scope Scope) (T, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.valid || s.scope != scope || s.valueEpoch != s.epoch || time.Since(s.loadedAt) >= s.policy.TTL {
		var zero T
		return zero, false
	}
	return s.clone(s.value), true
}

// Last returns the last successful value for a scope even after its TTL has
// expired. It supports explicitly stale, no-I/O presentation paths.
func (s *Snapshot[T]) Last(scope Scope) (T, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.valid || s.scope != scope || s.valueEpoch != s.epoch {
		var zero T
		return zero, false
	}
	return s.clone(s.value), true
}

func (s *Snapshot[T]) Invalidate(reason InvalidationReason) {
	if reason == "" {
		panic("snapshot invalidation reason is required")
	}
	s.mu.Lock()
	s.epoch++
	s.valid = false
	s.last = reason
	s.mu.Unlock()
}

func (s *Snapshot[T]) String() string {
	return fmt.Sprintf("snapshot(%s)", s.policy.Name)
}

func cancelledContext() context.Context {
	ctx, cancel := context.WithCancel(context.TODO())
	cancel()
	return ctx
}
