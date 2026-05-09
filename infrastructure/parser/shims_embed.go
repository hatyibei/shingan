package parser

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// shimsFS bundles the Python shim sources into the binary so the npm
// distribution (which ships only `shingan` + a wrapper, not the repo's
// `scripts/` directory) can still spawn the LangGraph and CrewAI workers.
//
//go:embed shims/*.py
var shimsFS embed.FS

// extractEmbeddedShim writes the bundled shim of the given filename to
// the user's cache directory (under `shingan-shims/v<version>/`) and
// returns the absolute path. Subsequent calls reuse the existing file
// when its size + mtime match — Go process restarts, the npm wrapper
// reusing a cache, and the LSP keeping a long-lived process all share
// the same on-disk copy.
//
// Returns an error if the embedded filesystem doesn't contain the
// requested shim or the cache directory is unwritable.
func extractEmbeddedShim(filename string) (string, error) {
	rel := "shims/" + filename
	data, err := fs.ReadFile(shimsFS, rel)
	if err != nil {
		return "", fmt.Errorf("embedded shim %q not bundled: %w", filename, err)
	}

	cacheBase, err := os.UserCacheDir()
	if err != nil {
		// Fall back to OS temp dir when the user has no $XDG_CACHE_HOME
		// and HOME isn't writable (CI sandboxes, scratch containers).
		cacheBase = os.TempDir()
	}
	dir := filepath.Join(cacheBase, "shingan-shims", "v"+embeddedShimVersion)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create shim cache %q: %w", dir, err)
	}
	dst := filepath.Join(dir, filename)

	// Skip rewrite when the on-disk copy is already byte-identical. The
	// Python interpreter caches `.pyc` next to the source — overwriting
	// even with the same bytes invalidates that cache and triggers a
	// re-compile on every parser instantiation.
	if existing, err := os.ReadFile(dst); err == nil && len(existing) == len(data) && string(existing) == string(data) {
		return dst, nil
	}

	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return "", fmt.Errorf("write shim %q: %w", dst, err)
	}
	return dst, nil
}

// embeddedShimVersion is stamped into the cache directory path so a
// shingan upgrade gets a fresh extraction without colliding with the
// previous version's bytes (the Python interpreter would otherwise
// happily run the older shim from cache against a newer Go binary).
//
// Tests / vendor builds may override this via -ldflags so air-gapped
// installs share a stable path.
var embeddedShimVersion = "dev"
