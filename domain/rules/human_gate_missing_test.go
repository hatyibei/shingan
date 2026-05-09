package rules

import (
	"strings"
	"testing"

	"github.com/hatyibei/shingan/domain"
)

func graphWith(nodes map[string]*domain.Node) *domain.WorkflowGraph {
	return &domain.WorkflowGraph{Nodes: nodes}
}

func TestHumanGateMissing_FiresOnProdWithoutHuman(t *testing.T) {
	g := graphWith(map[string]*domain.Node{
		"agent": {
			ID: "agent", Name: "agent", Type: domain.NodeTypeLLM,
			Config: map[string]any{"env": "prod"},
		},
		"send_email": {
			ID: "send_email", Name: "send_email", Type: domain.NodeTypeTool,
			Config: map[string]any{"category": "api"},
		},
	})
	findings := NewHumanGateMissingChecker().Analyze(g)
	if len(findings) != 1 || findings[0].RuleName != "human_gate_missing" {
		t.Fatalf("expected 1 human_gate_missing Warning, got %+v", findings)
	}
	if findings[0].Severity != domain.Warning {
		t.Errorf("severity = %v, want Warning", findings[0].Severity)
	}
}

func TestHumanGateMissing_SilentInDev(t *testing.T) {
	g := graphWith(map[string]*domain.Node{
		"agent": {
			ID: "agent", Type: domain.NodeTypeLLM,
			Config: map[string]any{"env": "dev"},
		},
		"send_email": {
			ID: "send_email", Type: domain.NodeTypeTool,
			Config: map[string]any{"category": "api"},
		},
	})
	if findings := NewHumanGateMissingChecker().Analyze(g); len(findings) != 0 {
		t.Errorf("expected no findings in dev env, got %+v", findings)
	}
}

func TestHumanGateMissing_SilentWithHumanNode(t *testing.T) {
	g := graphWith(map[string]*domain.Node{
		"agent": {
			ID: "agent", Type: domain.NodeTypeLLM,
			Config: map[string]any{"env": "prod"},
		},
		"approval": {ID: "approval", Type: domain.NodeTypeHuman},
		"send_email": {
			ID: "send_email", Type: domain.NodeTypeTool,
			Config: map[string]any{"category": "api"},
		},
	})
	if findings := NewHumanGateMissingChecker().Analyze(g); len(findings) != 0 {
		t.Errorf("expected no findings when a Human node exists, got %+v", findings)
	}
}

func TestHumanGateMissing_SilentOnPureCompute(t *testing.T) {
	g := graphWith(map[string]*domain.Node{
		"prep": {
			ID: "prep", Type: domain.NodeTypeTool,
			Config: map[string]any{"deployment": true, "category": "compute"},
		},
		"transform": {
			ID: "transform", Type: domain.NodeTypeTool,
			Config: map[string]any{"category": "compute"},
		},
	})
	if findings := NewHumanGateMissingChecker().Analyze(g); len(findings) != 0 {
		t.Errorf("pure-compute graph should NOT trigger; got %+v", findings)
	}
}

func TestHumanGateMissing_SensitiveByName(t *testing.T) {
	g := graphWith(map[string]*domain.Node{
		"agent": {
			ID: "agent", Type: domain.NodeTypeLLM,
			Config: map[string]any{"env": "production"},
		},
		"transfer_funds": {
			ID: "transfer_funds", Name: "Transfer Funds",
			Type: domain.NodeTypeTool,
			// No `category` — must be detected via the name keyword.
			Config: map[string]any{},
		},
	})
	findings := NewHumanGateMissingChecker().Analyze(g)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding via name keyword (transfer); got %+v", findings)
	}
	if !strings.Contains(findings[0].Message, "transfer_funds") {
		t.Errorf("finding should reference the sensitive node id; got %q", findings[0].Message)
	}
}

func TestHumanGateMissing_NilGraph(t *testing.T) {
	if f := NewHumanGateMissingChecker().Analyze(nil); f != nil {
		t.Errorf("nil graph should yield nil findings; got %+v", f)
	}
}
