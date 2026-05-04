package rules

import (
	"strings"
	"testing"

	"github.com/hatyibei/shingan/domain"
)

// makeTemperatureGraph constructs a one-node WorkflowGraph for the
// temperature_misuse rule's test cases. It mirrors makeDeprecatedGraph but
// stays local so the helper can evolve independently.
func makeTemperatureGraph(nodeType domain.NodeType, name string, config map[string]any) *domain.WorkflowGraph {
	if name == "" {
		name = "test_node"
	}
	nodes := map[string]*domain.Node{
		"n1": {
			ID:     "n1",
			Name:   name,
			Type:   nodeType,
			Config: config,
		},
	}
	return &domain.WorkflowGraph{
		Nodes:       nodes,
		Edges:       nil,
		EntryNodeID: "n1",
	}
}

// --- positive: structured_output flag forces the strongest signal -----------

func TestTemperatureMisuse_StructuredOutput_Warning(t *testing.T) {
	g := makeTemperatureGraph(domain.NodeTypeLLM, "json_extractor", map[string]any{
		"model":             "gpt-4o-mini",
		"temperature":       0.7,
		"structured_output": true,
	})
	findings := NewTemperatureMisuseChecker().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	f := findings[0]
	if f.Severity != domain.Warning {
		t.Errorf("expected Warning, got %s", f.Severity)
	}
	if f.Confidence != 0.9 {
		t.Errorf("expected confidence 0.9, got %f", f.Confidence)
	}
	if f.ConfidenceReason != domain.ReasonExactStaticMatch {
		t.Errorf("expected ReasonExactStaticMatch, got %q", f.ConfidenceReason)
	}
	if f.RuleName != "temperature_misuse" {
		t.Errorf("expected rule name temperature_misuse, got %s", f.RuleName)
	}
	if !strings.Contains(strings.ToLower(f.Message), "structured") {
		t.Errorf("expected 'structured' in message, got: %s", f.Message)
	}
}

func TestTemperatureMisuse_ResponseFormatJSON_Warning(t *testing.T) {
	g := makeTemperatureGraph(domain.NodeTypeLLM, "extractor", map[string]any{
		"model":           "gpt-4o-mini",
		"temperature":     0.4,
		"response_format": "json_object",
	})
	findings := NewTemperatureMisuseChecker().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Severity != domain.Warning {
		t.Errorf("expected Warning, got %s", findings[0].Severity)
	}
	if findings[0].ConfidenceReason != domain.ReasonExactStaticMatch {
		t.Errorf("expected ReasonExactStaticMatch, got %q", findings[0].ConfidenceReason)
	}
}

// --- positive: classification task ------------------------------------------

func TestTemperatureMisuse_Classification_Warning(t *testing.T) {
	g := makeTemperatureGraph(domain.NodeTypeLLM, "intent_classifier", map[string]any{
		"model":       "gpt-4o-mini",
		"temperature": 0.6,
		"task":        "classification",
	})
	findings := NewTemperatureMisuseChecker().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	f := findings[0]
	if f.Severity != domain.Warning {
		t.Errorf("expected Warning for classification with high temp, got %s", f.Severity)
	}
	if f.Confidence != 0.7 {
		t.Errorf("expected confidence 0.7, got %f", f.Confidence)
	}
	if f.ConfidenceReason != domain.ReasonHeuristicPattern {
		t.Errorf("expected ReasonHeuristicPattern, got %q", f.ConfidenceReason)
	}
	if !strings.Contains(strings.ToLower(f.Message), "classification") {
		t.Errorf("expected 'classification' in message, got: %s", f.Message)
	}
}

func TestTemperatureMisuse_Classification_LowTemp_NoFinding(t *testing.T) {
	// task=classification with temp=0.2 (below 0.3 threshold) → no finding.
	g := makeTemperatureGraph(domain.NodeTypeLLM, "intent_classifier", map[string]any{
		"model":       "gpt-4o-mini",
		"temperature": 0.2,
		"task":        "classification",
	})
	findings := NewTemperatureMisuseChecker().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for classification with low temp, got %d", len(findings))
	}
}

// --- positive: extraction task ----------------------------------------------

func TestTemperatureMisuse_Extraction_Info(t *testing.T) {
	g := makeTemperatureGraph(domain.NodeTypeLLM, "field_extractor", map[string]any{
		"model":       "gpt-4o-mini",
		"temperature": 0.5,
		"task":        "extraction",
	})
	findings := NewTemperatureMisuseChecker().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	f := findings[0]
	if f.Severity != domain.Info {
		t.Errorf("expected Info, got %s", f.Severity)
	}
	if f.Confidence != 0.5 {
		t.Errorf("expected confidence 0.5, got %f", f.Confidence)
	}
	if f.ConfidenceReason != domain.ReasonHeuristicPattern {
		t.Errorf("expected ReasonHeuristicPattern, got %q", f.ConfidenceReason)
	}
}

// --- positive: name-based heuristic fallback --------------------------------

func TestTemperatureMisuse_NameHeuristic_Extract(t *testing.T) {
	// No `task` key — Name contains "extract" → fall back to extraction signal.
	g := makeTemperatureGraph(domain.NodeTypeLLM, "extract_invoice_fields", map[string]any{
		"model":       "gpt-4o-mini",
		"temperature": 0.4,
	})
	findings := NewTemperatureMisuseChecker().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding via name heuristic, got %d", len(findings))
	}
	if findings[0].Severity != domain.Info {
		t.Errorf("expected Info via name heuristic, got %s", findings[0].Severity)
	}
}

// --- negative: temperature == 0 (correct deterministic config) -------------

func TestTemperatureMisuse_TempZero_NoFinding(t *testing.T) {
	g := makeTemperatureGraph(domain.NodeTypeLLM, "extractor", map[string]any{
		"model":             "gpt-4o-mini",
		"temperature":       0.0,
		"structured_output": true,
		"task":              "extraction",
	})
	findings := NewTemperatureMisuseChecker().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings when temperature == 0, got %d", len(findings))
	}
}

func TestTemperatureMisuse_NoTempKey_NoFinding(t *testing.T) {
	// temperature key absent → rule does not apply (provider default may be 0
	// or 1 depending on model; we only flag explicit > 0).
	g := makeTemperatureGraph(domain.NodeTypeLLM, "extractor", map[string]any{
		"model":             "gpt-4o-mini",
		"structured_output": true,
	})
	findings := NewTemperatureMisuseChecker().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings when temperature key absent, got %d", len(findings))
	}
}

// --- negative: creative task (no signal of determinism) --------------------

func TestTemperatureMisuse_CreativeTask_NoFinding(t *testing.T) {
	g := makeTemperatureGraph(domain.NodeTypeLLM, "story_writer", map[string]any{
		"model":       "gpt-4o-mini",
		"temperature": 0.9,
		"task":        "creative_writing",
	})
	findings := NewTemperatureMisuseChecker().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for creative_writing task, got %d", len(findings))
	}
}

// --- negative: non-LLM node skipped ----------------------------------------

func TestTemperatureMisuse_NonLLMNode_Skipped(t *testing.T) {
	g := makeTemperatureGraph(domain.NodeTypeTool, "tool", map[string]any{
		"temperature":       0.7,
		"structured_output": true,
	})
	findings := NewTemperatureMisuseChecker().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for non-LLM node, got %d", len(findings))
	}
}

// --- edge: temperature as int (JSON integer) -------------------------------

func TestTemperatureMisuse_TempAsInt_Detected(t *testing.T) {
	// JSON decoders may produce int / float64 depending on path; the rule
	// must accept both numeric kinds for the same logical value.
	g := makeTemperatureGraph(domain.NodeTypeLLM, "extractor", map[string]any{
		"model":             "gpt-4o-mini",
		"temperature":       1, // int, not float64
		"structured_output": true,
	})
	findings := NewTemperatureMisuseChecker().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding for temperature=1 (int), got %d", len(findings))
	}
}

// --- edge: temperature as non-numeric string ignored ----------------------

func TestTemperatureMisuse_TempNonNumeric_Skipped(t *testing.T) {
	g := makeTemperatureGraph(domain.NodeTypeLLM, "extractor", map[string]any{
		"model":             "gpt-4o-mini",
		"temperature":       "hot", // bogus
		"structured_output": true,
	})
	findings := NewTemperatureMisuseChecker().Analyze(g)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings when temperature is non-numeric string, got %d", len(findings))
	}
}

// --- meta + reason stamp validation ----------------------------------------

func TestTemperatureMisuse_Meta(t *testing.T) {
	c := NewTemperatureMisuseChecker()
	if c.Name() != "temperature_misuse" {
		t.Errorf("expected name temperature_misuse, got %s", c.Name())
	}
	m := c.Meta()
	if m.Name != "temperature_misuse" {
		t.Errorf("Meta.Name expected temperature_misuse, got %s", m.Name)
	}
	if m.Severity != domain.Warning {
		t.Errorf("Meta.Severity expected Warning (default), got %s", m.Severity)
	}
}

func TestTemperatureMisuse_AllFindingsHaveReason(t *testing.T) {
	cases := []map[string]any{
		{"model": "gpt-4o-mini", "temperature": 0.7, "structured_output": true},
		{"model": "gpt-4o-mini", "temperature": 0.7, "task": "classification"},
		{"model": "gpt-4o-mini", "temperature": 0.5, "task": "extraction"},
		{"model": "gpt-4o-mini", "temperature": 0.4, "task": "code_generation"},
	}
	for i, cfg := range cases {
		g := makeTemperatureGraph(domain.NodeTypeLLM, "n", cfg)
		findings := NewTemperatureMisuseChecker().Analyze(g)
		if len(findings) == 0 {
			t.Errorf("case %d: expected at least 1 finding, got 0", i)
			continue
		}
		for _, f := range findings {
			if f.ConfidenceReason == "" {
				t.Errorf("case %d: ConfidenceReason missing on finding %+v", i, f)
			}
		}
	}
}

// --- nil / empty graph guard ----------------------------------------------

func TestTemperatureMisuse_NilGraph_NoFinding(t *testing.T) {
	findings := NewTemperatureMisuseChecker().Analyze(nil)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for nil graph, got %d", len(findings))
	}
}
