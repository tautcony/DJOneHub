package snapshot

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func testSnapshot[T any](t *testing.T, ttl, timeout time.Duration, clone CloneFunc[T]) *Snapshot[T] {
	t.Helper()
	return New(Policy{Name: "test", TTL: ttl, LoadTimeout: timeout}, context.Background, clone)
}

func TestSnapshotHitStaleErrorAndClone(t *testing.T) {
	s := testSnapshot(t, 10*time.Millisecond, time.Second, func(value []string) []string {
		return append([]string(nil), value...)
	})
	calls := 0
	load := func(context.Context) ([]string, error) {
		calls++
		return []string{"original"}, nil
	}

	first, outcome, err := s.Get(context.Background(), Scope{Generation: 1}, load)
	if err != nil || outcome != Miss || calls != 1 {
		t.Fatalf("first = %#v %s %v calls=%d", first, outcome, err, calls)
	}
	first[0] = "changed"
	second, outcome, err := s.Get(context.Background(), Scope{Generation: 1}, load)
	if err != nil || outcome != Hit || second[0] != "original" || calls != 1 {
		t.Fatalf("hit = %#v %s %v calls=%d", second, outcome, err, calls)
	}
	time.Sleep(15 * time.Millisecond)
	_, outcome, err = s.Get(context.Background(), Scope{Generation: 1}, load)
	if err != nil || outcome != Stale || calls != 2 {
		t.Fatalf("stale outcome=%s err=%v calls=%d", outcome, err, calls)
	}

	s.Invalidate("test_mutation")
	failing := func(context.Context) ([]string, error) { calls++; return nil, errors.New("failed") }
	if _, _, err = s.Get(context.Background(), Scope{Generation: 1}, failing); err == nil {
		t.Fatal("failed load succeeded")
	}
	if _, outcome, err = s.Get(context.Background(), Scope{Generation: 1}, load); err != nil || outcome != Miss || calls != 4 {
		t.Fatalf("retry outcome=%s err=%v calls=%d", outcome, err, calls)
	}
}

func TestSnapshotCoalescesAndCallerCancellationDoesNotCancelLoad(t *testing.T) {
	s := testSnapshot[int](t, time.Second, time.Second, nil)
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	load := func(ctx context.Context) (int, error) {
		calls.Add(1)
		close(started)
		select {
		case <-release:
			return 42, nil
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}

	firstCtx, cancel := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() { _, _, err := s.Get(firstCtx, Scope{Generation: 1}, load); firstDone <- err }()
	<-started
	secondDone := make(chan struct {
		value   int
		outcome Outcome
		err     error
	}, 1)
	go func() {
		value, outcome, err := s.Get(context.Background(), Scope{Generation: 1}, load)
		secondDone <- struct {
			value   int
			outcome Outcome
			err     error
		}{value, outcome, err}
	}()
	time.Sleep(10 * time.Millisecond)
	cancel()
	if err := <-firstDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("first error = %v", err)
	}
	close(release)
	second := <-secondDone
	if second.err != nil || second.value != 42 || second.outcome != Coalesced || calls.Load() != 1 {
		t.Fatalf("second = %#v calls=%d", second, calls.Load())
	}
}

func TestSnapshotLoadTimeout(t *testing.T) {
	s := testSnapshot[int](t, time.Second, 10*time.Millisecond, nil)
	_, _, err := s.Get(context.Background(), Scope{}, func(ctx context.Context) (int, error) {
		<-ctx.Done()
		return 0, ctx.Err()
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v", err)
	}
}

func TestSnapshotRejectsLateWritesAcrossInvalidationAndGeneration(t *testing.T) {
	s := testSnapshot[int](t, time.Second, time.Second, nil)
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _ = s.Get(context.Background(), Scope{Generation: 1}, func(context.Context) (int, error) {
			close(started)
			<-release
			return 1, nil
		})
	}()
	<-started
	s.Invalidate("generation_changed")
	close(release)
	<-done
	if _, ok := s.Peek(Scope{Generation: 1}); ok {
		t.Fatal("late invalidated value was stored")
	}
	value, outcome, err := s.Get(context.Background(), Scope{Generation: 2}, func(context.Context) (int, error) { return 2, nil })
	if err != nil || value != 2 || outcome != Miss {
		t.Fatalf("new generation = %d %s %v", value, outcome, err)
	}
	if _, ok := s.Peek(Scope{Generation: 1}); ok {
		t.Fatal("new generation value appeared in old scope")
	}
}

func TestSnapshotSummaryUsesFixedOutcomes(t *testing.T) {
	s := testSnapshot[int](t, time.Second, time.Second, nil)
	if _, _, err := s.Get(context.Background(), Scope{}, func(context.Context) (int, error) { return 1, nil }); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Get(context.Background(), Scope{}, func(context.Context) (int, error) { return 2, nil }); err != nil {
		t.Fatal(err)
	}
	summary := s.Summary()
	if summary.Name != "test" || summary.TTLMS != 1000 || summary.LoadTimeoutMS != 1000 {
		t.Fatalf("summary = %#v", summary)
	}
	if len(summary.Outcomes) != 4 || summary.Outcomes[Miss] != 1 || summary.Outcomes[Hit] != 1 || summary.Outcomes[Stale] != 0 || summary.Outcomes[Coalesced] != 0 {
		t.Fatalf("outcomes = %#v", summary.Outcomes)
	}
}
