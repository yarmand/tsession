package webui

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/yarma/tsession/internal/sessions"
)

// fakeClock lets tests control sessionCache's notion of "now" without
// sleeping for the TTL.
type fakeClock struct {
	t time.Time
}

func (c *fakeClock) now() time.Time { return c.t }

func TestSessionCache_ColdReadLoadsSynchronously(t *testing.T) {
	var calls int32
	clock := &fakeClock{t: time.Unix(0, 0)}
	c := newSessionCache(func() ([]sessions.Session, error) {
		atomic.AddInt32(&calls, 1)
		return []sessions.Session{{ID: "s1"}}, nil
	}, clock.now)

	all, err := c.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(all) != 1 || all[0].ID != "s1" {
		t.Fatalf("Get() = %+v, want one session s1", all)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("provider calls = %d, want 1", got)
	}
}

func TestSessionCache_FreshReadDoesNotReload(t *testing.T) {
	var calls int32
	clock := &fakeClock{t: time.Unix(0, 0)}
	c := newSessionCache(func() ([]sessions.Session, error) {
		atomic.AddInt32(&calls, 1)
		return []sessions.Session{{ID: "s1"}}, nil
	}, clock.now)
	c.SetTTL(3 * time.Second)

	if _, err := c.Get(); err != nil {
		t.Fatalf("first Get: %v", err)
	}

	clock.t = clock.t.Add(1 * time.Second) // still within the 3s TTL
	if _, err := c.Get(); err != nil {
		t.Fatalf("second Get: %v", err)
	}

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("provider calls = %d, want 1 (fresh read should not reload)", got)
	}
}

func TestSessionCache_StaleReadReturnsImmediatelyAndRefreshesInBackground(t *testing.T) {
	var calls int32
	clock := &fakeClock{t: time.Unix(0, 0)}
	refreshStarted := make(chan struct{})
	releaseRefresh := make(chan struct{})
	c := newSessionCache(func() ([]sessions.Session, error) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			return []sessions.Session{{ID: "s1"}}, nil
		}
		// Second call is the background refresh: block until the test
		// releases it, so we can assert the stale read didn't wait on it.
		close(refreshStarted)
		<-releaseRefresh
		return []sessions.Session{{ID: "s2"}}, nil
	}, clock.now)
	c.SetTTL(3 * time.Second)

	if _, err := c.Get(); err != nil {
		t.Fatalf("first Get: %v", err)
	}

	clock.t = clock.t.Add(10 * time.Second) // now stale

	start := time.Now()
	all, err := c.Get()
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("stale Get: %v", err)
	}
	if elapsed > 200*time.Millisecond {
		t.Fatalf("stale Get took %v, want it to return immediately without waiting on refresh", elapsed)
	}
	if len(all) != 1 || all[0].ID != "s1" {
		t.Fatalf("stale Get() = %+v, want the old snapshot (s1)", all)
	}

	select {
	case <-refreshStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("expected a background refresh to have started")
	}
	close(releaseRefresh)

	// Wait for the background refresh to land; keep polling (without
	// forcing repeated extra refreshes) since the exact refresh completion
	// time is nondeterministic.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		all, _ := c.Get()
		if len(all) == 1 && all[0].ID == "s2" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	all, _ = c.Get()
	if len(all) != 1 || all[0].ID != "s2" {
		t.Fatalf("after background refresh, Get() = %+v, want s2", all)
	}
}

func TestSessionCache_ConcurrentStaleReadsCoalesceIntoOneRefresh(t *testing.T) {
	var calls int32
	clock := &fakeClock{t: time.Unix(0, 0)}
	c := newSessionCache(func() ([]sessions.Session, error) {
		atomic.AddInt32(&calls, 1)
		return []sessions.Session{{ID: "s1"}}, nil
	}, clock.now)
	c.SetTTL(3 * time.Second)

	if _, err := c.Get(); err != nil {
		t.Fatalf("first Get: %v", err)
	}
	clock.t = clock.t.Add(10 * time.Second)

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			_, _ = c.Get()
			done <- struct{}{}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}

	// Give the (at most one) background refresh time to complete.
	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("provider calls = %d, want 2 (one initial load + one coalesced refresh)", got)
	}
}

func TestSessionCache_ZeroTTLAlwaysLoadsLive(t *testing.T) {
	var calls int32
	clock := &fakeClock{t: time.Unix(0, 0)}
	c := newSessionCache(func() ([]sessions.Session, error) {
		atomic.AddInt32(&calls, 1)
		return []sessions.Session{{ID: "s1"}}, nil
	}, clock.now)
	c.SetTTL(0)

	for i := 0; i < 3; i++ {
		if _, err := c.Get(); err != nil {
			t.Fatalf("Get: %v", err)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("provider calls = %d, want 3 (TTL 0 disables caching)", got)
	}
}

func TestSessionCache_PatchNamePatchesInPlace(t *testing.T) {
	clock := &fakeClock{t: time.Unix(0, 0)}
	c := newSessionCache(func() ([]sessions.Session, error) {
		return []sessions.Session{{ID: "s1", Name: "old"}, {ID: "s2", Name: "other"}}, nil
	}, clock.now)
	c.SetTTL(3 * time.Second)

	if _, err := c.Get(); err != nil {
		t.Fatalf("Get: %v", err)
	}

	c.PatchName("s1", "new")

	all, err := c.Get()
	if err != nil {
		t.Fatalf("Get after patch: %v", err)
	}
	var got string
	for _, s := range all {
		if s.ID == "s1" {
			got = s.Name
		}
	}
	if got != "new" {
		t.Fatalf("Name after PatchName = %q, want %q", got, "new")
	}
	for _, s := range all {
		if s.ID == "s2" && s.Name != "other" {
			t.Fatalf("PatchName should not affect other sessions, got %q", s.Name)
		}
	}
}

func TestSessionCache_PatchNameNoOpWhenColdOrMissing(t *testing.T) {
	clock := &fakeClock{t: time.Unix(0, 0)}
	c := newSessionCache(func() ([]sessions.Session, error) {
		return []sessions.Session{{ID: "s1", Name: "old"}}, nil
	}, clock.now)

	// Cache is cold: PatchName before any Get must not panic and must be a
	// no-op.
	c.PatchName("s1", "new")

	all, err := c.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(all) != 1 || all[0].Name != "old" {
		t.Fatalf("Get() = %+v, want unpatched snapshot (cache was cold)", all)
	}

	// Unknown id: no-op, no panic.
	c.PatchName("missing", "whatever")
}
