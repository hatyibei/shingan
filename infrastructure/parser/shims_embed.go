package parser

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/gofrs/flock"
)

// shimsFS bundles the shim sources into the binary so the npm distribution
// (which ships only `shingan` + a wrapper, not the repo's `scripts/`
// directory) can still spawn the LangGraph / CrewAI (Python) and LangGraph.js
// (Node `.mjs`) workers. The LangGraph.js shim is the ~21KB source
// `export_langgraphjs_server.mjs`; it imports the TypeScript Compiler API at
// runtime, which `ensureLangGraphJSDeps` provides via `npm install` next to
// the extracted shim (the matching `package.json` is embedded alongside).
// Shipping the source instead of a ~10MB bundled compiler keeps the binary
// small — ADR-015's "runtime npm install" distribution model.
//
//go:embed shims/*.py shims/*.mjs shims/package.json
var shimsFS embed.FS

// extractEmbeddedShim writes the bundled shim of the given filename to the
// user's cache directory (under `shingan-shims/<version>/`) and returns the
// absolute path. Subsequent calls reuse the existing file when its contents
// are byte-identical — Go process restarts, the npm wrapper reusing a cache,
// and the LSP keeping a long-lived process all share the same on-disk copy.
//
// Writes are made crash-safe and concurrency-safe:
//   - A file lock (`<dst>.lock`) serialises writers across processes, so two
//     `shingan` instances extracting the same shim concurrently never race.
//   - The payload is written to a temp file in the same directory and renamed
//     into place, so readers never observe a partially written shim and a
//     crash mid-write cannot leave a truncated file at `dst`.
//
// Returns an error if the embedded filesystem doesn't contain the requested
// shim or the cache directory is unwritable.
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
	dir := filepath.Join(cacheBase, "shingan-shims", shimCacheVersion())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create shim cache %q: %w", dir, err)
	}
	dst := filepath.Join(dir, filename)

	// Fast path: the on-disk copy is already byte-identical, so we skip both
	// the lock and the rewrite. The Python interpreter caches `.pyc` next to
	// the source — overwriting even with the same bytes invalidates that
	// cache and triggers a re-compile on every parser instantiation.
	if shimUpToDate(dst, data) {
		return dst, nil
	}

	// Serialise writers across processes. A separate `.lock` sentinel is used
	// (rather than locking `dst`) so the lock lifecycle is independent of the
	// atomic rename below.
	lock := flock.New(dst + ".lock")
	if err := lock.Lock(); err != nil {
		return "", fmt.Errorf("acquire shim lock %q: %w", dst+".lock", err)
	}
	defer func() { _ = lock.Unlock() }()

	// Re-check inside the lock: another process may have written the shim
	// while we waited to acquire it.
	if shimUpToDate(dst, data) {
		return dst, nil
	}

	if err := atomicWriteFile(dir, dst, data); err != nil {
		return "", err
	}
	return dst, nil
}

// shimUpToDate reports whether dst already contains exactly data.
func shimUpToDate(dst string, data []byte) bool {
	existing, err := os.ReadFile(dst)
	return err == nil && len(existing) == len(data) && sha256.Sum256(existing) == sha256.Sum256(data)
}

// atomicWriteFile writes data to a temp file in dir and renames it onto dst.
// On any failure the temp file is removed so no `.tmp-*` residue leaks into
// the cache directory.
func atomicWriteFile(dir, dst string, data []byte) error {
	tmp, err := os.CreateTemp(dir, filepath.Base(dst)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp shim in %q: %w", dir, err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write temp shim %q: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp shim %q: %w", tmpPath, err)
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		cleanup()
		return fmt.Errorf("chmod temp shim %q: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		cleanup()
		return fmt.Errorf("rename shim into place %q: %w", dst, err)
	}
	return nil
}

// embeddedShimVersion is stamped into the cache directory path so a
// shingan upgrade gets a fresh extraction without colliding with the
// previous version's bytes (the Python interpreter would otherwise
// happily run the older shim from cache against a newer Go binary).
//
// Release builds override this via -ldflags
// (`-X ...parser.embeddedShimVersion=<ver>`). When it is left at the
// default sentinel `"dev"`, shimCacheVersion derives a per-binary key so
// that two locally-built binaries (e.g. different `go install` revisions)
// do NOT share a `vdev/` directory and clobber each other's shims.
var embeddedShimVersion = "dev"

// shimCacheVersion returns the directory segment under `shingan-shims/`.
//
// For release builds (embeddedShimVersion overridden) it is `v<version>`,
// preserving the historical layout. For unstamped dev builds it is a
// per-binary key `dev-<goos>-<goarch>-<binary-sha256[:8]>`, so each distinct
// binary gets its own cache and `-ldflags -X ...` being forgotten no longer
// causes cross-binary shim collisions.
func shimCacheVersion() string {
	if embeddedShimVersion != "dev" {
		return "v" + embeddedShimVersion
	}
	return devShimVersion()
}

var (
	devShimVersionOnce sync.Once
	devShimVersionVal  string
)

func devShimVersion() string {
	devShimVersionOnce.Do(func() {
		base := fmt.Sprintf("dev-%s-%s", runtime.GOOS, runtime.GOARCH)
		exe, err := os.Executable()
		if err != nil {
			devShimVersionVal = base
			return
		}
		f, err := os.Open(exe)
		if err != nil {
			devShimVersionVal = base
			return
		}
		defer f.Close()
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			devShimVersionVal = base
			return
		}
		devShimVersionVal = base + "-" + hex.EncodeToString(h.Sum(nil))[:8]
	})
	return devShimVersionVal
}
