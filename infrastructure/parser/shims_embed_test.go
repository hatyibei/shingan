package parser

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// realShim is a filename that is actually present in the embedded shimsFS, so
// extractEmbeddedShim can read its payload.
const realShim = "export_langgraph_server.py"

func shimCacheDir(t *testing.T, base string) string {
	t.Helper()
	return filepath.Join(base, "shingan-shims", shimCacheVersion())
}

// TestExtractEmbeddedShim_ConcurrentExtractions verifies that many goroutines
// extracting the same shim concurrently all succeed, agree on the path, and
// leave a correct, complete file — exercising the flock + atomic-rename path.
func TestExtractEmbeddedShim_ConcurrentExtractions(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmp)

	const n = 50
	var wg sync.WaitGroup
	paths := make([]string, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			paths[i], errs[i] = extractEmbeddedShim(realShim)
		}(i)
	}
	wg.Wait()

	for i, e := range errs {
		if e != nil {
			t.Errorf("goroutine %d: %v", i, e)
		}
	}
	for i := 1; i < n; i++ {
		if paths[i] != paths[0] {
			t.Errorf("paths differ: %q vs %q", paths[0], paths[i])
		}
	}

	want, err := shimsFS.ReadFile("shims/" + realShim)
	if err != nil {
		t.Fatalf("read embedded shim: %v", err)
	}
	got, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatalf("read extracted shim: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("extracted shim content mismatch (%d vs %d bytes)", len(got), len(want))
	}
}

// TestExtractEmbeddedShim_AtomicRename_NoPartialFile verifies that a normal
// extraction leaves no `.tmp-*` residue from the atomic write.
func TestExtractEmbeddedShim_AtomicRename_NoPartialFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmp)

	if _, err := extractEmbeddedShim(realShim); err != nil {
		t.Fatalf("extract: %v", err)
	}

	entries, err := os.ReadDir(shimCacheDir(t, tmp))
	if err != nil {
		t.Fatalf("read cache dir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("temp file leaked: %s", e.Name())
		}
	}
}

// TestExtractEmbeddedShim_FastPathIdempotent verifies that a second extraction
// of an already-current shim returns the same path without rewriting (the
// content is unchanged), and that repeated calls are stable.
func TestExtractEmbeddedShim_FastPathIdempotent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmp)

	first, err := extractEmbeddedShim(realShim)
	if err != nil {
		t.Fatalf("first extract: %v", err)
	}
	infoBefore, err := os.Stat(first)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	second, err := extractEmbeddedShim(realShim)
	if err != nil {
		t.Fatalf("second extract: %v", err)
	}
	if first != second {
		t.Errorf("path changed between calls: %q vs %q", first, second)
	}
	infoAfter, err := os.Stat(second)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	// Fast path must not rewrite the file (preserves the adjacent .pyc cache).
	if !infoBefore.ModTime().Equal(infoAfter.ModTime()) {
		t.Errorf("fast path rewrote the file: mtime %v -> %v", infoBefore.ModTime(), infoAfter.ModTime())
	}
}

// TestShimCacheVersion_DevIsPerBinary verifies the unstamped (dev) cache key
// includes the OS/arch and a binary digest so distinct binaries never collide.
func TestShimCacheVersion_DevIsPerBinary(t *testing.T) {
	if embeddedShimVersion != "dev" {
		t.Skipf("embeddedShimVersion stamped to %q; dev-key test n/a", embeddedShimVersion)
	}
	v := shimCacheVersion()
	if !strings.HasPrefix(v, "dev-") {
		t.Errorf("dev cache key %q should start with dev-", v)
	}
	if v == "dev" {
		t.Errorf("dev cache key must be more specific than bare %q", v)
	}
}
