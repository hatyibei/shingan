package domain

import "testing"

func intPtr(i int) *int           { return &i }
func floatPtr(f float64) *float64 { return &f }

func TestNode_GetMaxIterations(t *testing.T) {
	cases := []struct {
		name string
		node *Node
		want int
		ok   bool
	}{
		{"typed field wins", &Node{MaxIterations: intPtr(7), Config: map[string]any{"max_iterations": 99}}, 7, true},
		{"config int fallback", &Node{Config: map[string]any{"max_iterations": 5}}, 5, true},
		{"config float fallback", &Node{Config: map[string]any{"max_iterations": float64(8)}}, 8, true},
		{"config string fallback", &Node{Config: map[string]any{"max_iterations": "12"}}, 12, true},
		{"config unparseable string", &Node{Config: map[string]any{"max_iterations": "abc"}}, 0, false},
		{"missing", &Node{Config: map[string]any{}}, 0, false},
		{"nil config", &Node{}, 0, false},
		{"nil node", nil, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := tc.node.GetMaxIterations()
			if got != tc.want || ok != tc.ok {
				t.Errorf("GetMaxIterations() = (%d, %v), want (%d, %v)", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestNode_GetTemperature(t *testing.T) {
	if v, ok := (&Node{Temperature: floatPtr(0.9), Config: map[string]any{"temperature": 0.1}}).GetTemperature(); !ok || v != 0.9 {
		t.Errorf("typed temperature should win: got (%v,%v)", v, ok)
	}
	if v, ok := (&Node{Config: map[string]any{"temperature": 0.7}}).GetTemperature(); !ok || v != 0.7 {
		t.Errorf("config temperature fallback: got (%v,%v)", v, ok)
	}
	if _, ok := (&Node{Config: map[string]any{}}).GetTemperature(); ok {
		t.Error("missing temperature should be ok=false")
	}
}

func TestNode_GetModelName(t *testing.T) {
	if got := (&Node{ModelName: "gpt-4o", Config: map[string]any{"model": "old"}}).GetModelName(); got != "gpt-4o" {
		t.Errorf("typed model should win: got %q", got)
	}
	if got := (&Node{Config: map[string]any{"model": "claude-3"}}).GetModelName(); got != "claude-3" {
		t.Errorf("config model fallback: got %q", got)
	}
	if got := (&Node{}).GetModelName(); got != "" {
		t.Errorf("missing model should be empty: got %q", got)
	}
}

func TestNode_GetToolCategory(t *testing.T) {
	if got := (&Node{ToolCategory: "code_execution", Config: map[string]any{"category": "api"}}).GetToolCategory(); got != "code_execution" {
		t.Errorf("typed category should win: got %q", got)
	}
	if got := (&Node{Config: map[string]any{"category": "trigger"}}).GetToolCategory(); got != "trigger" {
		t.Errorf("config category fallback: got %q", got)
	}
	if got := (&Node{}).GetToolCategory(); got != "" {
		t.Errorf("missing category should be empty: got %q", got)
	}
}

func TestNode_GetMaxConcurrency(t *testing.T) {
	if v, ok := (&Node{MaxConcurrency: intPtr(20), Config: map[string]any{"max_concurrency": 1}}).GetMaxConcurrency(); !ok || v != 20 {
		t.Errorf("typed concurrency should win: got (%v,%v)", v, ok)
	}
	if v, ok := (&Node{Config: map[string]any{"max_concurrency": 4}}).GetMaxConcurrency(); !ok || v != 4 {
		t.Errorf("config concurrency fallback: got (%v,%v)", v, ok)
	}
	if _, ok := (&Node{}).GetMaxConcurrency(); ok {
		t.Error("missing concurrency should be ok=false")
	}
}
