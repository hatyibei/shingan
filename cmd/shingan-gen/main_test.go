package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/hatyibei/shingan/domain"
)

// ---- helpers ----

// roundTrip generates a graph for the given pattern, marshals it to JSON via
// toOutputGraph, and unmarshals it back into a domain.WorkflowGraph.
// This validates that the JSON format is round-trip compatible with shingan's
// JSON parser (which expects nodes as an array).
func roundTrip(t *testing.T, pattern string, size int, seed int64) *domain.WorkflowGraph {
	t.Helper()

	g, err := generate(pattern, size, seed)
	if err != nil {
		t.Fatalf("generate(%q): %v", pattern, err)
	}
	if g == nil {
		t.Fatalf("generate(%q) returned nil", pattern)
	}

	out := toOutputGraph(g)
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed domain.WorkflowGraph
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v\nJSON was:\n%s", err, data)
	}

	return &parsed
}

// ---- pattern round-trip tests ----

func TestGenerate_Random_ValidJSON(t *testing.T) {
	g := roundTrip(t, "random", 20, 42)
	if len(g.Nodes) == 0 {
		t.Error("expected at least one node after round-trip")
	}
}

func TestGenerate_Clean_ValidJSON(t *testing.T) {
	g := roundTrip(t, "clean", 5, 42)
	if len(g.Nodes) == 0 {
		t.Error("expected at least one node after round-trip")
	}
	if g.EntryNodeID == "" {
		t.Error("expected EntryNodeID to be set after round-trip")
	}
}

func TestGenerate_Buggy_ValidJSON(t *testing.T) {
	g := roundTrip(t, "buggy", 0, 42)
	if len(g.Nodes) == 0 {
		t.Error("expected at least one node after round-trip")
	}
}

func TestGenerate_InfiniteLoop_ValidJSON(t *testing.T) {
	g := roundTrip(t, "infinite-loop", 0, 42)
	if len(g.Nodes) == 0 {
		t.Error("expected at least one node after round-trip")
	}
}

func TestGenerate_Unreachable_ValidJSON(t *testing.T) {
	g := roundTrip(t, "unreachable", 10, 42)
	if len(g.Nodes) == 0 {
		t.Error("expected at least one node after round-trip")
	}
}

func TestGenerate_PIILeak_ValidJSON(t *testing.T) {
	g := roundTrip(t, "pii-leak", 0, 42)
	if len(g.Nodes) == 0 {
		t.Error("expected at least one node after round-trip")
	}
}

func TestGenerate_Cycle_ValidJSON(t *testing.T) {
	g := roundTrip(t, "cycle", 4, 42)
	if len(g.Nodes) == 0 {
		t.Error("expected at least one node after round-trip")
	}
}

// ---- determinism test ----

func TestGenerate_SeedReproducibility(t *testing.T) {
	patterns := []string{"random", "clean", "unreachable", "cycle"}
	for _, p := range patterns {
		t.Run(p, func(t *testing.T) {
			g1, err1 := generate(p, 10, 99)
			g2, err2 := generate(p, 10, 99)
			if err1 != nil || err2 != nil {
				t.Fatalf("generate error: %v / %v", err1, err2)
			}
			if len(g1.Nodes) != len(g2.Nodes) {
				t.Errorf("same seed should produce same node count: %d vs %d", len(g1.Nodes), len(g2.Nodes))
			}
			if len(g1.Edges) != len(g2.Edges) {
				t.Errorf("same seed should produce same edge count: %d vs %d", len(g1.Edges), len(g2.Edges))
			}
		})
	}
}

// Deterministic patterns (ignore seed)
func TestGenerate_DeterministicPatterns_SameOutput(t *testing.T) {
	deterministicPatterns := []string{"buggy", "infinite-loop", "pii-leak"}
	for _, p := range deterministicPatterns {
		t.Run(p, func(t *testing.T) {
			g1, err1 := generate(p, 0, 1)
			g2, err2 := generate(p, 0, 2) // different seed, should produce same graph
			if err1 != nil || err2 != nil {
				t.Fatalf("generate error: %v / %v", err1, err2)
			}
			if len(g1.Nodes) != len(g2.Nodes) {
				t.Errorf("deterministic pattern %q should ignore seed: %d vs %d nodes", p, len(g1.Nodes), len(g2.Nodes))
			}
		})
	}
}

// ---- unknown pattern ----

func TestGenerate_UnknownPattern_ReturnsError(t *testing.T) {
	_, err := generate("invalid-pattern", 10, 42)
	if err == nil {
		t.Error("expected error for unknown pattern")
	}
}

// ---- output file test ----

func TestGenerate_OutputFile_WritesSuccessfully(t *testing.T) {
	tmp := t.TempDir()
	outPath := filepath.Join(tmp, "test-output.json")

	g, err := generate("clean", 5, 42)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	out := toOutputGraph(g)
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Read back and verify
	readBack, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	var parsed domain.WorkflowGraph
	if err := json.Unmarshal(readBack, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(parsed.Nodes) == 0 {
		t.Error("expected at least one node in written file")
	}
}

// ---- toOutputGraph sorts nodes deterministically ----

func TestToOutputGraph_SortedNodes(t *testing.T) {
	g, err := generate("buggy", 0, 42)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	out := toOutputGraph(g)
	for i := 1; i < len(out.Nodes); i++ {
		if out.Nodes[i-1].ID > out.Nodes[i].ID {
			t.Errorf("nodes not sorted: %q > %q at index %d", out.Nodes[i-1].ID, out.Nodes[i].ID, i)
		}
	}
}

// ---- all patterns produce non-empty graphs ----

func TestGenerate_AllPatterns_NonEmpty(t *testing.T) {
	patterns := []struct {
		name string
		size int
		seed int64
	}{
		{"random", 10, 42},
		{"clean", 5, 42},
		{"buggy", 0, 42},
		{"infinite-loop", 0, 42},
		{"unreachable", 10, 42},
		{"pii-leak", 0, 42},
		{"cycle", 4, 42},
	}

	for _, tc := range patterns {
		t.Run(tc.name, func(t *testing.T) {
			g, err := generate(tc.name, tc.size, tc.seed)
			if err != nil {
				t.Fatalf("generate: %v", err)
			}
			if g == nil {
				t.Fatal("expected non-nil graph")
			}
			if len(g.Nodes) == 0 {
				t.Error("expected at least one node")
			}
			if g.EntryNodeID == "" {
				t.Error("expected EntryNodeID to be set")
			}
			if _, ok := g.Nodes[g.EntryNodeID]; !ok {
				t.Errorf("entry node %q not found in Nodes", g.EntryNodeID)
			}
		})
	}
}
