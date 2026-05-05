package rules

import (
	"strings"
	"testing"

	"github.com/hatyibei/shingan/domain"
)

// makeToolGraph constructs a one-node WorkflowGraph for the
// unbounded_tool_arg rule's test cases.
func makeToolGraph(config map[string]any) *domain.WorkflowGraph {
	nodes := map[string]*domain.Node{
		"t1": {
			ID:     "t1",
			Name:   "tool_under_test",
			Type:   domain.NodeTypeTool,
			Config: config,
		},
	}
	return &domain.WorkflowGraph{
		Nodes:       nodes,
		Edges:       nil,
		EntryNodeID: "t1",
	}
}

// --- Name / Meta -----------------------------------------------------------

func TestUnboundedToolArg_Name(t *testing.T) {
	if got := NewUnboundedToolArgChecker().Name(); got != "unbounded_tool_arg" {
		t.Errorf("Name() = %q, want unbounded_tool_arg", got)
	}
}

func TestUnboundedToolArg_Meta(t *testing.T) {
	m := NewUnboundedToolArgChecker().Meta()
	if m.Name != "unbounded_tool_arg" {
		t.Errorf("Meta.Name = %q, want unbounded_tool_arg", m.Name)
	}
	if m.Severity != domain.Warning {
		t.Errorf("Meta.Severity = %s, want Warning", m.Severity)
	}
}

// --- positive: string field without maxLength ----------------------------

func TestUnboundedToolArg_StringNoMaxLength_Warning(t *testing.T) {
	g := makeToolGraph(map[string]any{
		"args_schema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type": "string",
				},
			},
		},
	})
	findings := NewUnboundedToolArgChecker().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.Severity != domain.Warning {
		t.Errorf("Severity = %s, want Warning", f.Severity)
	}
	if f.Confidence != 0.7 {
		t.Errorf("Confidence = %f, want 0.7", f.Confidence)
	}
	if f.ConfidenceReason != domain.ReasonHeuristicPattern {
		t.Errorf("Reason = %q, want heuristic_pattern", f.ConfidenceReason)
	}
	if !strings.Contains(f.Message, "maxLength") {
		t.Errorf("expected 'maxLength' in message, got: %s", f.Message)
	}
	if !strings.Contains(f.Message, "args_schema.query") {
		t.Errorf("expected field path in message, got: %s", f.Message)
	}
}

// --- positive: oversize maxLength is Info ------------------------------------

func TestUnboundedToolArg_StringHugeMaxLength_Info(t *testing.T) {
	g := makeToolGraph(map[string]any{
		"args_schema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":      "string",
					"maxLength": 1_000_000.0, // way above 100K threshold
				},
			},
		},
	})
	findings := NewUnboundedToolArgChecker().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if findings[0].Severity != domain.Info {
		t.Errorf("Severity = %s, want Info", findings[0].Severity)
	}
	if findings[0].Confidence != 0.5 {
		t.Errorf("Confidence = %f, want 0.5", findings[0].Confidence)
	}
}

// --- positive: array field without maxItems ----------------------------------

func TestUnboundedToolArg_ArrayNoMaxItems_Warning(t *testing.T) {
	g := makeToolGraph(map[string]any{
		"parameters": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"items": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type":      "string",
						"maxLength": 100.0,
					},
				},
			},
		},
	})
	findings := NewUnboundedToolArgChecker().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if findings[0].Severity != domain.Warning {
		t.Errorf("Severity = %s, want Warning", findings[0].Severity)
	}
	if !strings.Contains(findings[0].Message, "maxItems") {
		t.Errorf("expected 'maxItems' in message, got: %s", findings[0].Message)
	}
}

// --- positive: number field without maximum ----------------------------------

func TestUnboundedToolArg_NumberNoMaximum_Info(t *testing.T) {
	g := makeToolGraph(map[string]any{
		"input_schema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"limit": map[string]any{
					"type": "integer",
				},
			},
		},
	})
	findings := NewUnboundedToolArgChecker().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if findings[0].Severity != domain.Info {
		t.Errorf("Severity = %s, want Info", findings[0].Severity)
	}
	if findings[0].Confidence != 0.4 {
		t.Errorf("Confidence = %f, want 0.4", findings[0].Confidence)
	}
}

// --- negative: bounded fields produce no findings ---------------------------

func TestUnboundedToolArg_BoundedSchema_NoFinding(t *testing.T) {
	g := makeToolGraph(map[string]any{
		"args_schema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":      "string",
					"maxLength": 4000.0,
				},
				"items": map[string]any{
					"type":     "array",
					"maxItems": 100.0,
					"items": map[string]any{
						"type":      "string",
						"maxLength": 200.0,
					},
				},
				"limit": map[string]any{
					"type":    "integer",
					"maximum": 1000.0,
				},
			},
		},
	})
	findings := NewUnboundedToolArgChecker().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings on bounded schema, got %d: %+v", len(findings), findings)
	}
}

// --- negative: schemaless tool (no args_schema / parameters / input_schema) -

func TestUnboundedToolArg_NoSchemaConfig_NoFinding(t *testing.T) {
	g := makeToolGraph(map[string]any{
		"category":    "api",
		"description": "external HTTP",
	})
	findings := NewUnboundedToolArgChecker().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings when no schema config present, got %d: %+v", len(findings), findings)
	}
}

// --- negative: non-Tool node ignored ----------------------------------------

func TestUnboundedToolArg_NonToolNode_Skipped(t *testing.T) {
	nodes := map[string]*domain.Node{
		"l1": {
			ID:   "l1",
			Type: domain.NodeTypeLLM,
			Config: map[string]any{
				"args_schema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query": map[string]any{"type": "string"},
					},
				},
			},
		},
	}
	g := &domain.WorkflowGraph{Nodes: nodes, EntryNodeID: "l1"}
	findings := NewUnboundedToolArgChecker().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings on non-Tool node, got %d", len(findings))
	}
}

// --- edge: nested object schema is recursed into ----------------------------

func TestUnboundedToolArg_NestedObjectField_Detected(t *testing.T) {
	g := makeToolGraph(map[string]any{
		"args_schema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"outer": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"inner_query": map[string]any{
							"type": "string", // no maxLength
						},
					},
				},
			},
		},
	})
	findings := NewUnboundedToolArgChecker().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 nested finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, "outer.inner_query") {
		t.Errorf("expected nested path in message, got: %s", findings[0].Message)
	}
}

// --- edge: cap at maxFindingsPerNode ----------------------------------------

func TestUnboundedToolArg_FindingsCap(t *testing.T) {
	props := make(map[string]any, 20)
	for i := 0; i < 20; i++ {
		key := "f" + string(rune('a'+i))
		props[key] = map[string]any{"type": "string"} // each unbounded
	}
	g := makeToolGraph(map[string]any{
		"args_schema": map[string]any{
			"type":       "object",
			"properties": props,
		},
	})
	findings := NewUnboundedToolArgChecker().Analyze(g)
	if len(findings) > maxFindingsPerNode {
		t.Errorf("expected at most %d findings (cap), got %d", maxFindingsPerNode, len(findings))
	}
	if len(findings) == 0 {
		t.Errorf("expected some findings hitting the cap, got 0")
	}
}

// --- edge: parameters key is also accepted ---------------------------------

func TestUnboundedToolArg_ParametersKey_Detected(t *testing.T) {
	g := makeToolGraph(map[string]any{
		"parameters": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"q": map[string]any{"type": "string"},
			},
		},
	})
	findings := NewUnboundedToolArgChecker().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding via parameters key, got %d", len(findings))
	}
}

// --- edge: maxLength as int (JSON integer) is accepted ---------------------

func TestUnboundedToolArg_MaxLengthAsInt_Bounded(t *testing.T) {
	g := makeToolGraph(map[string]any{
		"args_schema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"q": map[string]any{
					"type":      "string",
					"maxLength": 1000, // int, not float64
				},
			},
		},
	})
	findings := NewUnboundedToolArgChecker().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings when maxLength is int 1000, got %d", len(findings))
	}
}

// --- edge: maxLength as non-numeric string is treated as missing -----------

func TestUnboundedToolArg_MaxLengthAsString_StillUnbounded(t *testing.T) {
	g := makeToolGraph(map[string]any{
		"args_schema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"q": map[string]any{
					"type":      "string",
					"maxLength": "unlimited", // bogus
				},
			},
		},
	})
	findings := NewUnboundedToolArgChecker().Analyze(g)
	if len(findings) != 1 {
		t.Errorf("expected 1 finding (maxLength typo treated as missing), got %d", len(findings))
	}
}

// --- nil / empty graph guard ------------------------------------------------

func TestUnboundedToolArg_NilGraph_NoFinding(t *testing.T) {
	findings := NewUnboundedToolArgChecker().Analyze(nil)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for nil graph, got %d", len(findings))
	}
}

func TestUnboundedToolArg_EmptyGraph_NoFinding(t *testing.T) {
	g := &domain.WorkflowGraph{Nodes: map[string]*domain.Node{}}
	findings := NewUnboundedToolArgChecker().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for empty graph, got %d", len(findings))
	}
}

// --- ConfidenceReason stamp on every Finding -------------------------------

func TestUnboundedToolArg_AllFindingsHaveReason(t *testing.T) {
	g := makeToolGraph(map[string]any{
		"args_schema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"q":     map[string]any{"type": "string"},
				"items": map[string]any{"type": "array", "items": map[string]any{"type": "string", "maxLength": 100.0}},
				"limit": map[string]any{"type": "integer"},
			},
		},
	})
	findings := NewUnboundedToolArgChecker().Analyze(g)
	if len(findings) == 0 {
		t.Fatalf("expected findings to assert Reason on, got 0")
	}
	for i, f := range findings {
		if f.ConfidenceReason == "" {
			t.Errorf("finding %d missing ConfidenceReason: %+v", i, f)
		}
	}
}
