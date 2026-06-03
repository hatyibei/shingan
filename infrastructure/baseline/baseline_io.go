// Package baseline provides file I/O for domain.Baseline snapshots.
// The domain layer defines the types and comparison semantics; this package
// handles JSON serialization and disk access, keeping domain stdlib-pure.
package baseline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hatyibei/shingan/domain"
)

// Save writes b to path as pretty-printed JSON. Parent directories are created
// if they do not exist. Existing files are overwritten in place
// (os.WriteFile truncate-then-write — not atomic; concurrent readers may see
// partial contents).
func Save(path string, b *domain.Baseline) error {
	if b == nil {
		return fmt.Errorf("save baseline: nil baseline")
	}
	if path == "" {
		return fmt.Errorf("save baseline: empty path")
	}

	// Always persist at the current schema version (ADR-016): a baseline read
	// as legacy v1 is rewritten as v2 on the next save.
	b.Version = domain.BaselineSchemaVersion

	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal baseline: %w", err)
	}

	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("mkdir %q: %w", dir, err)
		}
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write baseline %q: %w", path, err)
	}
	return nil
}

// Load reads and parses a baseline JSON file from path.
func Load(path string) (*domain.Baseline, error) {
	if path == "" {
		return nil, fmt.Errorf("load baseline: empty path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read baseline %q: %w", path, err)
	}
	var b domain.Baseline
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("parse baseline %q: %w", path, err)
	}
	// Legacy v1 files (no "version" key, or version < current) are migrated in
	// memory by FindingFingerprint.UnmarshalJSON (full message → digest). Warn
	// so the operator knows to re-save and adopt the stable v2 schema.
	if b.Version < domain.BaselineSchemaVersion {
		fmt.Fprintf(os.Stderr,
			"[shingan] baseline %q is schema v%d (legacy); migrated to v%d in memory. "+
				"Re-run with --save-baseline to persist the stable v%d format.\n",
			path, b.Version, domain.BaselineSchemaVersion, domain.BaselineSchemaVersion)
		b.Version = domain.BaselineSchemaVersion
	}
	return &b, nil
}
