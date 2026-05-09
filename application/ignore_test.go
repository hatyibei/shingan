package application

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hatyibei/shingan/domain"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.py")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func TestLoadIgnoreIndex_FileLevel(t *testing.T) {
	path := writeTemp(t, `# shingan: ignore-file eval_missing
# Some other comment
node_a = "data"
`)
	idx := LoadIgnoreIndex(path)
	if !idx.Suppressed("eval_missing", 3) {
		t.Errorf("expected eval_missing suppressed at line 3 (file-level)")
	}
	if idx.Suppressed("cycle_detection", 3) {
		t.Errorf("did NOT expect cycle_detection suppressed (only eval_missing on file scope)")
	}
}

func TestLoadIgnoreIndex_FileLevel_All(t *testing.T) {
	path := writeTemp(t, `# shingan: ignore-file
node_a = "data"
`)
	idx := LoadIgnoreIndex(path)
	if !idx.Suppressed("anything", 99) {
		t.Errorf("ignore-file with no rule list should suppress all rules")
	}
}

func TestLoadIgnoreIndex_LineLevel(t *testing.T) {
	path := writeTemp(t, `node_a = "data"  # shingan: ignore eval_missing
node_b = "other"
`)
	idx := LoadIgnoreIndex(path)
	if !idx.Suppressed("eval_missing", 1) {
		t.Errorf("expected eval_missing suppressed on line 1 (where the marker lives)")
	}
	if idx.Suppressed("eval_missing", 2) {
		t.Errorf("line-2 should NOT be affected by line-1 marker")
	}
}

func TestLoadIgnoreIndex_NextLine(t *testing.T) {
	path := writeTemp(t, `# shingan: ignore-next-line eval_missing
node_a = "data"
node_b = "other"
`)
	idx := LoadIgnoreIndex(path)
	if !idx.Suppressed("eval_missing", 2) {
		t.Errorf("expected eval_missing suppressed on line 2 (next-line marker)")
	}
	if idx.Suppressed("eval_missing", 1) || idx.Suppressed("eval_missing", 3) {
		t.Errorf("only line 2 should be affected by next-line marker")
	}
}

func TestLoadIgnoreIndex_MultipleRules(t *testing.T) {
	path := writeTemp(t, `# shingan: ignore-file eval_missing, cycle_detection, prompt_injection_sink
`)
	idx := LoadIgnoreIndex(path)
	for _, r := range []string{"eval_missing", "cycle_detection", "prompt_injection_sink"} {
		if !idx.Suppressed(r, 5) {
			t.Errorf("expected %s suppressed (multi-rule file marker)", r)
		}
	}
	if idx.Suppressed("retry_storm", 5) {
		t.Errorf("retry_storm should NOT be suppressed (not in marker list)")
	}
}

func TestLoadIgnoreIndex_GoStyleComment(t *testing.T) {
	path := writeTemp(t, `// shingan: ignore-file eval_missing
node_a = "data"
`)
	idx := LoadIgnoreIndex(path)
	if !idx.Suppressed("eval_missing", 3) {
		t.Errorf("Go-style // comment should also be recognised")
	}
}

func TestFilterIgnoredFindings(t *testing.T) {
	path := writeTemp(t, `# shingan: ignore-file eval_missing
some_node = build()
`)
	findings := []domain.Finding{
		{RuleName: "eval_missing", NodeID: "n1", SourceFile: path},
		{RuleName: "cycle_detection", NodeID: "n2", SourceFile: path},
	}
	resolver := func(_ string, nodeID string) (string, int) { return path, 0 }
	out := FilterIgnoredFindings(findings, resolver)
	if len(out) != 1 || out[0].RuleName != "cycle_detection" {
		t.Errorf("expected only cycle_detection to remain after eval_missing file ignore; got %+v", out)
	}
}
