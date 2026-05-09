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
