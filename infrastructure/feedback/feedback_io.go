// Package feedback provides file I/O for domain.FeedbackRecord labels.
//
// The domain layer defines the record type and the (tp|fp) label semantics;
// this package handles JSONL serialization and disk access, keeping domain
// stdlib-pure (Onion principle, ADR-003). It mirrors infrastructure/baseline:
// same path/permission conventions, same empty-path guards and %w error
// wrapping. No confidence math lives here — records are write/read only and
// nothing reads them back into the analysis pipeline (capture-only, ADR-018).
package feedback

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hatyibei/shingan/domain"
)

// Append writes a single record to path as one compact JSON line (JSONL),
// creating the file and parent directories if they do not exist. Existing
// content is preserved — the record is appended, so labels accrue over time.
//
// The write uses os.OpenFile with O_APPEND|O_CREATE so each Append is a single
// append-mode write of one newline-terminated line; this is the atomic-ish
// pattern POSIX guarantees for small appends to a single file. File mode 0o644
// and dir mode 0o755 match infrastructure/baseline.
func Append(path string, r domain.FeedbackRecord) error {
	if path == "" {
		return fmt.Errorf("append feedback: empty path")
	}

	// Marshal compact (NOT MarshalIndent) so each record is exactly one line.
	data, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal feedback record: %w", err)
	}

	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("mkdir %q: %w", dir, err)
		}
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open feedback %q: %w", path, err)
	}
	// Append the line then close; report a close error only if the write
	// itself succeeded, so a real write failure isn't masked.
	if _, werr := f.Write(append(data, '\n')); werr != nil {
		_ = f.Close()
		return fmt.Errorf("write feedback %q: %w", path, werr)
	}
	if cerr := f.Close(); cerr != nil {
		return fmt.Errorf("close feedback %q: %w", path, cerr)
	}
	return nil
}

// AppendAll writes each record to path in order, as one JSONL line apiece.
// It is a convenience for ingesting a batch; on the first error it returns
// immediately, leaving already-written records in place (append-only, so a
// partial batch is recoverable by re-running the remainder).
func AppendAll(path string, records []domain.FeedbackRecord) error {
	for i, r := range records {
		if err := Append(path, r); err != nil {
			return fmt.Errorf("append record %d: %w", i, err)
		}
	}
	return nil
}

// Load reads and parses a JSONL feedback file from path, returning every
// record in file order. A missing file is reported as an error (mirroring
// baseline.Load); callers that treat "no file yet" as "no feedback" should
// check os.IsNotExist on the wrapped error. Blank lines are skipped so a
// trailing newline (which Append always writes) is tolerated.
func Load(path string) ([]domain.FeedbackRecord, error) {
	if path == "" {
		return nil, fmt.Errorf("load feedback: empty path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read feedback %q: %w", path, err)
	}

	var records []domain.FeedbackRecord
	scanner := bufio.NewScanner(bytes.NewReader(data))
	// Allow long lines (a record with a very long source-file path).
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		raw := bytes.TrimSpace(scanner.Bytes())
		if len(raw) == 0 {
			continue
		}
		var rec domain.FeedbackRecord
		if err := json.Unmarshal(raw, &rec); err != nil {
			return nil, fmt.Errorf("parse feedback %q line %d: %w", path, line, err)
		}
		records = append(records, rec)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan feedback %q: %w", path, err)
	}
	return records, nil
}
