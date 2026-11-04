package rules

import (
	"testing"

	"github.com/hatyibei/shingan/domain"
)

func TestToolDescriptionMissing_FiresOnEmpty(t *testing.T) {
	g := graphWith(map[string]*domain.Node{
		"tool": {
			ID: "tool", Name: "search", Type: domain.NodeTypeTool,
			Config: map[string]any{},
		},
	})
	findings := NewToolDescriptionMissingChecker().Analyze(g)
	if len(findings) != 1 || findings[0].Severity != domain.Info {
		t.Fatalf("expected 1 Info finding; got %+v", findings)
	}
}

func TestToolDescriptionMissing_SilentOnGoodDescription(t *testing.T) {
	g := graphWith(map[string]*domain.Node{
		"tool": {
			ID: "tool", Name: "search", Type: domain.NodeTypeTool,
			Config: map[string]any{
				"description": "Search the public web index and return top-5 results as JSON.",
			},
		},
	})
	if findings := NewToolDescriptionMissingChecker().Analyze(g); len(findings) != 0 {
		t.Errorf("good description should not trigger; got %+v", findings)
	}
}

func TestToolDescriptionMissing_AcceptsAlternateKey(t *testing.T) {
	g := graphWith(map[string]*domain.Node{
		"tool": {
			ID: "tool", Name: "search", Type: domain.NodeTypeTool,
			Config: map[string]any{"doc": "Looks something up online and returns it"},
		},
	})
	if findings := NewToolDescriptionMissingChecker().Analyze(g); len(findings) != 0 {
		t.Errorf("alternate doc key should be accepted; got %+v", findings)
	}
}

func TestToolDescriptionMissing_NaturalLanguageNameIsOK(t *testing.T) {
	g := graphWith(map[string]*domain.Node{
		"tool": {
			ID: "tool", Name: "Send email to recipient",
			Type: domain.NodeTypeTool, Config: map[string]any{},
		},
	})
	if findings := NewToolDescriptionMissingChecker().Analyze(g); len(findings) != 0 {
		t.Errorf("name as natural-language description should pass; got %+v", findings)
	}
}

func TestToolDescriptionMissing_TooShort(t *testing.T) {
	g := graphWith(map[string]*domain.Node{
		"tool": {
			ID: "tool", Name: "search", Type: domain.NodeTypeTool,
			Config: map[string]any{"description": "fast"}, // < 10 chars
		},
	})
	findings := NewToolDescriptionMissingChecker().Analyze(g)
	if len(findings) != 1 {
		t.Errorf("description shorter than 10 chars should still trigger; got %+v", findings)
	}
}

func TestToolDescriptionMissing_TriggerExempt(t *testing.T) {
	g := graphWith(map[string]*domain.Node{
		"webhook": {
			ID: "webhook", Name: "webhook", Type: domain.NodeTypeTool,
			Config: map[string]any{"category": "trigger"},
		},
	})
	if findings := NewToolDescriptionMissingChecker().Analyze(g); len(findings) != 0 {
		t.Errorf("trigger nodes should be exempt; got %+v", findings)
	}
}

func TestToolDescriptionMissing_SilentOnNonTool(t *testing.T) {
	g := graphWith(map[string]*domain.Node{
		"agent": {ID: "agent", Type: domain.NodeTypeLLM, Config: map[string]any{}},
	})
	if findings := NewToolDescriptionMissingChecker().Analyze(g); len(findings) != 0 {
		t.Errorf("non-Tool nodes should not be considered; got %+v", findings)
	}
}

func TestToolDescriptionMissing_RespectsShinganIgnore(t *testing.T) {
	g := graphWith(map[string]*domain.Node{
		"tool": {
			ID: "tool", Name: "search", Type: domain.NodeTypeTool,
			Config: map[string]any{
				"_shingan_ignore": []any{"tool_description_missing"},
			},
		},
	})
	if findings := NewToolDescriptionMissingChecker().Analyze(g); len(findings) != 0 {
		t.Errorf("_shingan_ignore should suppress; got %+v", findings)
	}
}

// Issue #39: a LangGraph prebuilt ToolNode is an aggregate dispatcher; the
// descriptions the LLM reads live on the wrapped tools, so the dispatcher node
// itself must not be flagged.
func TestToolDescriptionMissing_SilentOnToolDispatcher(t *testing.T) {
	g := graphWith(map[string]*domain.Node{
		"tools": {
			ID: "tools", Name: "tools", Type: domain.NodeTypeTool,
			Config: map[string]any{"tool_dispatcher": true},
		},
	})
	if findings := NewToolDescriptionMissingChecker().Analyze(g); len(findings) != 0 {
		t.Errorf("tool_dispatcher node should not trigger; got %+v", findings)
	}
}

// tool_dispatcher=false (or a non-bool value) must not suppress the rule.
func TestToolDescriptionMissing_DispatcherFalseStillFires(t *testing.T) {
	g := graphWith(map[string]*domain.Node{
		"tools": {
			ID: "tools", Name: "tools", Type: domain.NodeTypeTool,
			Config: map[string]any{"tool_dispatcher": false},
		},
	})
	if findings := NewToolDescriptionMissingChecker().Analyze(g); len(findings) != 1 {
		t.Errorf("tool_dispatcher=false should still fire; got %+v", findings)
	}
}
