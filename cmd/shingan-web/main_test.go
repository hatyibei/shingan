package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hatyibei/shingan/domain"
)

// --- Middleware unit tests ---

// dummyHandler is an http.Handler that records whether it was called.
type dummyHandler struct {
	called bool
}

func (d *dummyHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	d.called = true
	w.WriteHeader(http.StatusOK)
}

// testSourceMap returns a minimal source map pointing at the real example files.
// Uses absolute paths so tests work regardless of working directory.
func testSourceMap() map[string]string {
	root := findProjectRoot()
	return map[string]string{
		"infinite_loop_unbounded": root + "/examples/runtime/infinite_loop_unbounded.go",
		"infinite_loop_bounded":   root + "/examples/runtime/infinite_loop_bounded.go",
		"simple_hello":            root + "/examples/runtime/simple_agent.go",
	}
}

// buildRunRequest returns a POST request to /api/run with the given appName.
func buildRunRequest(t *testing.T, appName string) *http.Request {
	t.Helper()
	body := map[string]interface{}{
		"appName":   appName,
		"userId":    "test-user",
		"sessionId": "test-session",
		"newMessage": map[string]interface{}{
			"role":  "user",
			"parts": []map[string]string{{"text": "hello"}},
		},
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal run request: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/run", bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	return r
}

// TestMiddleware_BlocksCriticalAgent verifies that a request for an agent with
// Critical findings receives HTTP 403 and the downstream handler is NOT called.
func TestMiddleware_BlocksCriticalAgent(t *testing.T) {
	sourceMap := testSourceMap()
	downstream := &dummyHandler{}
	mw := shinganGuardMiddleware(sourceMap)(downstream)

	req := buildRunRequest(t, "infinite_loop_unbounded")
	rec := httptest.NewRecorder()

	mw.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}

	if downstream.called {
		t.Error("downstream handler should NOT be called for blocked agent")
	}

	// Verify response body contains expected fields.
	var resp guardResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode guard response: %v", err)
	}
	if resp.Error != "shingan_guard" {
		t.Errorf("error field = %q, want %q", resp.Error, "shingan_guard")
	}
	if resp.Agent != "infinite_loop_unbounded" {
		t.Errorf("agent field = %q, want %q", resp.Agent, "infinite_loop_unbounded")
	}
	if len(resp.Findings) == 0 {
		t.Error("expected at least one finding in response")
	}
	for _, f := range resp.Findings {
		if f.Severity != "critical" {
			t.Errorf("finding severity = %q, want %q", f.Severity, "critical")
		}
	}
}

// TestMiddleware_PassesSafeAgent verifies that a request for an agent without
// Critical findings is passed through to the downstream handler (HTTP 200).
func TestMiddleware_PassesSafeAgent(t *testing.T) {
	sourceMap := testSourceMap()
	downstream := &dummyHandler{}
	mw := shinganGuardMiddleware(sourceMap)(downstream)

	req := buildRunRequest(t, "infinite_loop_bounded")
	rec := httptest.NewRecorder()

	mw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 (pass-through), got %d", rec.Code)
	}
	if !downstream.called {
		t.Error("downstream handler SHOULD be called for safe agent")
	}
}

// TestMiddleware_PassesSimpleHello verifies simple_hello passes the guard.
func TestMiddleware_PassesSimpleHello(t *testing.T) {
	sourceMap := testSourceMap()
	downstream := &dummyHandler{}
	mw := shinganGuardMiddleware(sourceMap)(downstream)

	req := buildRunRequest(t, "simple_hello")
	rec := httptest.NewRecorder()

	mw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 (pass-through), got %d", rec.Code)
	}
	if !downstream.called {
		t.Error("downstream handler SHOULD be called for simple_hello")
	}
}

// TestMiddleware_PassesNonRunPath verifies that non-run paths bypass the guard.
func TestMiddleware_PassesNonRunPath(t *testing.T) {
	sourceMap := testSourceMap()
	downstream := &dummyHandler{}
	mw := shinganGuardMiddleware(sourceMap)(downstream)

	req := httptest.NewRequest(http.MethodGet, "/api/list-apps", nil)
	rec := httptest.NewRecorder()

	mw.ServeHTTP(rec, req)

	if !downstream.called {
		t.Error("downstream handler SHOULD be called for non-run path")
	}
}

// TestMiddleware_BodyRestoredAfterRead verifies that the request body is
// readable by the downstream handler even after the middleware inspects it.
func TestMiddleware_BodyRestoredAfterRead(t *testing.T) {
	sourceMap := testSourceMap()

	var capturedBody []byte
	capturingHandler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		data := make([]byte, 4096)
		n, _ := r.Body.Read(data)
		capturedBody = data[:n]
	})

	mw := shinganGuardMiddleware(sourceMap)(capturingHandler)

	req := buildRunRequest(t, "infinite_loop_bounded")
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if len(capturedBody) == 0 {
		t.Error("downstream handler received empty body; body was not restored after middleware read")
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(capturedBody, &decoded); err != nil {
		t.Errorf("downstream body not valid JSON: %v", err)
	}
}

// TestIsRunPath tests the isRunPath helper.
func TestIsRunPath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/api/run", true},
		{"/api/run_sse", true},
		{"/api/list-apps", false},
		{"/ui/", false},
		{"/run", true}, // direct without prefix
	}
	for _, tc := range cases {
		got := isRunPath(tc.path)
		if got != tc.want {
			t.Errorf("isRunPath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// TestFilterCritical tests the filterCritical helper.
func TestFilterCritical(t *testing.T) {
	findings := []domain.Finding{
		{RuleName: "loop_guard", Severity: domain.Critical, Message: "critical"},
		{RuleName: "cost_estimation", Severity: domain.Warning, Message: "warning"},
		{RuleName: "cost_estimation", Severity: domain.Info, Message: "info"},
	}

	got := filterCritical(findings)
	if len(got) != 1 {
		t.Errorf("filterCritical: got %d findings, want 1", len(got))
	}
	if got[0].Severity != domain.Critical {
		t.Errorf("expected Critical severity, got %v", got[0].Severity)
	}
}
