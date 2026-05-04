// Package cache provides in-memory caching primitives used by the LSP server
// (cmd/shingan-lsp). Keeping the cache in infrastructure (rather than
// application) is intentional: callers depend on a domain-typed return value
// while the storage backend (simplelru) is purely an implementation detail.
//
// The cache is keyed by the SHA-256 of the analyzed file contents plus the
// parser format. This guarantees:
//
//  1. Pure idempotence: identical bytes + parser ⇒ identical findings.
//  2. Format isolation: a file viewed as "json" and the same bytes viewed as
//     "adk-go" never collide. Callers must therefore always include the parser
//     format when constructing a key.
//
// A 1-hour TTL is layered on top of simplelru so that, in the rare case where
// a parser process crashes mid-session and produces partial findings before we
// notice, those partial entries cannot persist forever. simplelru itself does
// not support TTL; we attach a timestamp to each entry and validate on Get.
package cache

import (
	"crypto/sha256"
	"sync"
	"time"

	"github.com/hashicorp/golang-lru/v2/simplelru"

	"github.com/hatyibei/shingan/domain"
)

// DefaultSize is the LRU capacity used when callers pass size <= 0.
//
// 512 entries chosen as a compromise between memory footprint (~few MB worst
// case) and hit rate during typical IDE editing sessions where a developer
// flips between a handful of related files.
const DefaultSize = 512

// DefaultTTL is the maximum age of a cached findings entry. Beyond this, Get
// reports a miss and the entry is evicted on next access.
//
// 1 hour matches ADR-009 ("parser crash 後の cache 信頼性") — long enough to
// preserve interactive performance across a typical workday, short enough that
// stale results from a transient parser bug do not survive a coffee break.
const DefaultTTL = time.Hour

// Key uniquely identifies a cache entry. The format is deliberately included
// alongside the SHA-256 hash so identical bytes interpreted by different
// parsers (json vs adk-go) cannot alias each other. The Path component is
// only meaningful for parsers whose semantics depend on the file location
// (langgraph: sys.path resolution); other formats leave Path empty and the
// key collapses to (format, hash).
type Key struct {
	Format string
	Path   string
	Hash   [sha256.Size]byte
}

// MakeKey returns a Key for the given parser format and raw input bytes.
// SHA-256 was chosen for its strong collision resistance (cache mis-association
// would silently surface wrong diagnostics, an outright user-visible bug)
// rather than raw speed. xxhash would be ~5x faster but its 64-bit output is
// not safe enough for a content-addressable diagnostics cache.
//
// Path is empty for content-addressable parsers (json, adk-go in-memory).
// For path-sensitive parsers (langgraph), callers use MakeKeyWithPath so
// two identical files in different folders — which can resolve different
// sibling imports — get distinct cache entries (Codex iter4 P2).
func MakeKey(format string, content []byte) Key {
	return MakeKeyWithPath(format, "", content)
}

// MakeKeyWithPath is MakeKey with an explicit path component. Use this for
// parsers whose output depends on the file's on-disk location (langgraph
// sys.path resolution). Pass path="" to collapse to MakeKey behaviour.
func MakeKeyWithPath(format, path string, content []byte) Key {
	return Key{
		Format: format,
		Path:   path,
		Hash:   sha256.Sum256(content),
	}
}

// entry pairs the cached findings with the wall-clock time at which they were
// stored. Storing the wall clock (rather than a monotonic deadline) is
// acceptable because the comparison happens against time.Now() in the same
// process; clock jumps would only cause a one-shot miss/hit anomaly.
type entry struct {
	findings []domain.Finding
	storedAt time.Time
}

// AnalysisCache is a goroutine-safe SHA-256-keyed LRU of analysis findings
// with an attached TTL. It is intentionally narrow: only Get / Add / Len are
// exposed because the LSP server has no need for richer eviction semantics.
type AnalysisCache struct {
	mu  sync.Mutex
	lru *simplelru.LRU[Key, entry]
	ttl time.Duration

	// now is injected for tests. Production callers always use time.Now.
	now func() time.Time
}

// NewAnalysisCache constructs a fresh cache. size <= 0 selects DefaultSize.
//
// The constructor returns a value rather than an error: simplelru.NewLRU only
// fails on size <= 0, which we now guard against ourselves, so the failure
// mode is structurally unreachable. We swallow the error to keep call sites
// at the LSP entry point straightforward.
func NewAnalysisCache(size int) *AnalysisCache {
	if size <= 0 {
		size = DefaultSize
	}
	lru, _ := simplelru.NewLRU[Key, entry](size, nil)
	return &AnalysisCache{
		lru: lru,
		ttl: DefaultTTL,
		now: time.Now,
	}
}

// SetTTL overrides the entry expiration window. Intended for tests; production
// callers should rely on DefaultTTL.
func (c *AnalysisCache) SetTTL(ttl time.Duration) {
	c.mu.Lock()
	c.ttl = ttl
	c.mu.Unlock()
}

// Get returns the cached findings for key when present and unexpired. A miss
// returns (nil, false) — the boolean is canonical; callers must not infer
// presence from a non-nil slice (a successful cache of zero findings is also
// (nil, true) — but we always store a defensive empty slice to keep that
// shape stable, see Add).
//
// On TTL expiration the stale entry is evicted as a side effect, keeping the
// cache from accumulating zombie data. This means a hit immediately followed
// by a Get returning false implies the entry's TTL just elapsed, which is
// fine; the caller will re-analyze and Add a fresh entry.
func (c *AnalysisCache) Get(key Key) ([]domain.Finding, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.lru.Get(key)
	if !ok {
		return nil, false
	}
	if c.now().Sub(e.storedAt) > c.ttl {
		// Expire on access. Removing here keeps the cache compact without
		// requiring a background reaper goroutine — the LSP server is
		// long-running but TTL-touched-on-every-Get is sufficient because
		// every diagnostics cycle goes through Get.
		c.lru.Remove(key)
		return nil, false
	}
	// Defensive copy is intentionally avoided — the returned slice is
	// read-only by the caller (the LSP server only reads findings to convert
	// them to Diagnostics). If a future caller mutates this, we revisit.
	return e.findings, true
}

// Add stores findings for key, replacing any existing entry. A nil findings
// slice is normalized to an empty (non-nil) slice so subsequent Get calls
// always return a slice that is safe to range over without a nil check.
func (c *AnalysisCache) Add(key Key, findings []domain.Finding) {
	if findings == nil {
		findings = []domain.Finding{}
	}
	c.mu.Lock()
	c.lru.Add(key, entry{
		findings: findings,
		storedAt: c.now(),
	})
	c.mu.Unlock()
}

// Len returns the number of entries currently held. Useful for tests and
// metrics; not consumed by the LSP server hot path.
func (c *AnalysisCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lru.Len()
}
