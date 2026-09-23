package webui

import (
	"sync"
	"time"

	"github.com/yarma/tsession/internal/sessions"
)

// sessionCache is a stale-while-revalidate cache in front of a
// SessionsProvider. Remote sessions require a live SSH round trip
// (internal/remote.FetchAll), which can take ~2s — long enough that every
// caller sharing one cache (the /api/sessions handler, the SSE poller, the
// terminal WebSocket's session lookup, and rename) noticeably speeds up by
// never blocking on it once a snapshot exists.
//
// Semantics:
//   - The first call (no snapshot yet) loads synchronously; there is
//     nothing useful to show before that anyway.
//   - A call within ttl of the last successful load returns the cached
//     snapshot immediately.
//   - A call past ttl returns the (stale) cached snapshot immediately and
//     kicks off a single background refresh; concurrent stale callers
//     coalesce into that one refresh rather than each starting their own.
//   - ttl == 0 disables caching entirely (every call loads synchronously),
//     which tests use to keep observing fast, deterministic transitions.
type sessionCache struct {
	fn  SessionsProvider
	now func() time.Time

	mu         sync.Mutex
	ttl        time.Duration
	have       bool
	snapshot   []sessions.Session
	err        error
	fetchedAt  time.Time
	refreshing bool
}

// newSessionCache returns a sessionCache that loads from fn, using now to
// timestamp loads (tests can inject a fake clock).
func newSessionCache(fn SessionsProvider, now func() time.Time) *sessionCache {
	return &sessionCache{fn: fn, now: now, ttl: 3 * time.Second}
}

// SetTTL overrides the freshness window. 0 disables caching.
func (c *sessionCache) SetTTL(d time.Duration) {
	c.mu.Lock()
	c.ttl = d
	c.mu.Unlock()
}

// Get returns the current session list, per the staleness rules above.
func (c *sessionCache) Get() ([]sessions.Session, error) {
	c.mu.Lock()
	ttl := c.ttl
	have := c.have
	c.mu.Unlock()

	if ttl <= 0 || !have {
		return c.load()
	}

	c.mu.Lock()
	age := c.now().Sub(c.fetchedAt)
	if age < ttl {
		snap, err := c.snapshot, c.err
		c.mu.Unlock()
		return snap, err
	}
	if !c.refreshing {
		c.refreshing = true
		go c.backgroundRefresh()
	}
	snap, err := c.snapshot, c.err
	c.mu.Unlock()
	return snap, err
}

func (c *sessionCache) load() ([]sessions.Session, error) {
	all, err := c.fn()
	c.mu.Lock()
	c.snapshot = all
	c.err = err
	c.have = true
	c.fetchedAt = c.now()
	c.mu.Unlock()
	return all, err
}

func (c *sessionCache) backgroundRefresh() {
	all, err := c.fn()
	c.mu.Lock()
	c.snapshot = all
	c.err = err
	c.fetchedAt = c.now()
	c.refreshing = false
	c.mu.Unlock()
}

// PatchName updates the Name field of the cached session with the given id
// in place, so a rename is reflected instantly without waiting out the TTL
// or forcing a reload (which would re-run the expensive SessionsProvider).
// It is a no-op if id isn't present in the current snapshot (e.g. cache is
// still cold).
func (c *sessionCache) PatchName(id, name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.snapshot {
		if c.snapshot[i].ID == id {
			c.snapshot[i].Name = name
		}
	}
}
