package cache

import (
	"testing"
	"time"

	"github.com/hatyibei/shingan/domain"
)

// findingFixture returns a small slice with deterministic content so tests
// can assert equality without depending on rule ordering side effects.
func findingFixture(rule string) []domain.Finding {
	return []domain.Finding{
		{
			RuleName: rule,
			Severity: domain.Warning,
			NodeID:   "n1",
			Message:  "fixture",
		},
	}
}

func TestMakeKey_FormatIsolation(t *testing.T) {
	t.Parallel()

	body := []byte(`{"nodes":[]}`)
	jsonKey := MakeKey("json", body)
	adkKey := MakeKey("adk-go", body)

	if jsonKey == adkKey {
		t.Fatalf("expected different keys for different formats; got identical %v", jsonKey)
	}
	if jsonKey.Hash != adkKey.Hash {
		t.Fatalf("expected identical hash payload; json=%x adk=%x", jsonKey.Hash, adkKey.Hash)
	}
}

func TestAnalysisCache_HitAndMiss(t *testing.T) {
	t.Parallel()

	c := NewAnalysisCache(8)
	key := MakeKey("json", []byte("hello"))

	if _, ok := c.Get(key); ok {
		t.Fatalf("expected miss on empty cache")
	}

	want := findingFixture("rule_a")
	c.Add(key, want)

	got, ok := c.Get(key)
	if !ok {
		t.Fatalf("expected hit after Add")
	}
	if len(got) != 1 || got[0].RuleName != "rule_a" {
		t.Fatalf("unexpected findings returned: %+v", got)
	}
}

func TestAnalysisCache_AddNilNormalizesEmpty(t *testing.T) {
	t.Parallel()

	c := NewAnalysisCache(4)
	key := MakeKey("json", []byte("nil"))
	c.Add(key, nil)

	got, ok := c.Get(key)
	if !ok {
		t.Fatalf("expected hit even when stored nil")
	}
	if got == nil {
		t.Fatalf("Add(nil) must store a non-nil slice")
	}
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %d", len(got))
	}
}

func TestAnalysisCache_LRUEviction(t *testing.T) {
	t.Parallel()

	c := NewAnalysisCache(2)
	keyA := MakeKey("json", []byte("A"))
	keyB := MakeKey("json", []byte("B"))
	keyC := MakeKey("json", []byte("C"))

	c.Add(keyA, findingFixture("a"))
	c.Add(keyB, findingFixture("b"))

	// Touch A so B becomes the LRU victim.
	if _, ok := c.Get(keyA); !ok {
		t.Fatalf("expected A to still be present")
	}

	c.Add(keyC, findingFixture("c"))

	if _, ok := c.Get(keyB); ok {
		t.Fatalf("expected B to be evicted as LRU after touching A then adding C")
	}
	if _, ok := c.Get(keyA); !ok {
		t.Fatalf("expected A to survive eviction")
	}
	if _, ok := c.Get(keyC); !ok {
		t.Fatalf("expected C to be present")
	}
	if got := c.Len(); got != 2 {
		t.Fatalf("expected Len=2, got %d", got)
	}
}

func TestAnalysisCache_TTLExpiration(t *testing.T) {
	t.Parallel()

	c := NewAnalysisCache(4)
	c.SetTTL(50 * time.Millisecond)

	// Inject a controllable clock so we don't rely on real wall-clock sleep
	// in tests (flake-prone on busy CI).
	now := time.Unix(1_700_000_000, 0)
	c.now = func() time.Time { return now }

	key := MakeKey("json", []byte("ttl"))
	c.Add(key, findingFixture("rule_ttl"))

	// First Get within TTL: hit.
	if _, ok := c.Get(key); !ok {
		t.Fatalf("expected hit within TTL window")
	}

	// Advance virtual clock past the TTL: subsequent Get must miss and
	// remove the stale entry.
	now = now.Add(time.Second)
	if _, ok := c.Get(key); ok {
		t.Fatalf("expected miss after TTL expiration")
	}
	if got := c.Len(); got != 0 {
		t.Fatalf("expected expired entry to be evicted on Get; Len=%d", got)
	}
}

func TestAnalysisCache_DefaultSize(t *testing.T) {
	t.Parallel()

	c := NewAnalysisCache(0) // selects DefaultSize internally
	if c.lru == nil {
		t.Fatalf("expected a non-nil simplelru even with size=0")
	}
	// Insert one to confirm the cache is functional, not just non-nil.
	key := MakeKey("json", []byte("default"))
	c.Add(key, findingFixture("default"))
	if got, ok := c.Get(key); !ok || len(got) == 0 {
		t.Fatalf("default-sized cache failed basic round-trip")
	}
}
