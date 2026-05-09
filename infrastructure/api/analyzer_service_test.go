package api_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/hatyibei/shingan/application"
	infraapi "github.com/hatyibei/shingan/infrastructure/api"
	"github.com/hatyibei/shingan/infrastructure/factory"

	analyzer "github.com/hatyibei/shingan/gen/analyzer"
	genserver "github.com/hatyibei/shingan/gen/http/analyzer/server"
	goahttp "goa.design/goa/v3/http"
)

// newTestServer builds a goahttp mux and wraps it in an httptest.Server.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	pf := factory.NewParserFactory()
	af := factory.NewAnalyzerFactory()
	orch := application.NewAnalysisOrchestrator()
	svc := infraapi.NewAnalyzerService(pf, af, orch)

	endpoints := analyzer.NewEndpoints(svc)
	mux := goahttp.NewMuxer()
	srv := genserver.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil)
	srv.Mount(mux)

	return httptest.NewServer(mux)
}

// loadFixture reads a file from ../../testdata/ relative to this package.
func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("../../testdata/" + name)
	if err != nil {
		t.Fatalf("loadFixture %q: %v", name, err)
	}
	return data
}

// analyzeRequestBody builds the JSON body for POST /analyze.
// goa encodes Bytes fields as base64 in JSON transport.
func analyzeRequestBody(format string, content []byte, output string) io.Reader {
	b64 := base64.StdEncoding.EncodeToString(content)
	body := map[string]any{
		"format":  format,
		"content": b64,
		"output":  output,
	}
	buf, _ := json.Marshal(body)
	return bytes.NewReader(buf)
}

// TestAnalyze_ValidJSON — Case 1: POST /analyze with valid JSON workflow → 200 + findings.
func TestAnalyze_ValidJSON(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	content := loadFixture(t, "buggy.json")
	resp, err := http.Post(
		ts.URL+"/analyze",
		"application/json",
		analyzeRequestBody("json", content, "json"),
	)
	if err != nil {
		t.Fatalf("POST /analyze: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	findings, ok := result["findings"].([]any)
	if !ok {
		t.Fatal("expected findings array in response")
	}
	if len(findings) == 0 {
		t.Error("expected at least one finding for buggy.json")
	}
	t.Logf("findings count: %d", len(findings))
}

// TestAnalyze_ValidADKGo — Case 2: POST /analyze with valid adk-go source → 200.
func TestAnalyze_ValidADKGo(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	content := loadFixture(t, "agents/unreachable.go")
	resp, err := http.Post(
		ts.URL+"/analyze",
		"application/json",
		analyzeRequestBody("adk-go", content, "json"),
	)
	if err != nil {
		t.Fatalf("POST /analyze (adk-go): %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode adk-go response: %v", err)
	}
	t.Logf("adk-go findings: %v", result["findings"])
}

// TestAnalyze_UnknownFormat — Case 3: POST /analyze with unknown format → 400.
func TestAnalyze_UnknownFormat(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	b64 := base64.StdEncoding.EncodeToString([]byte("{}"))
	body := map[string]any{
		"format":  "xml", // not in Enum, triggers DSL validation
		"content": b64,
	}
	buf, _ := json.Marshal(body)

	resp, err := http.Post(ts.URL+"/analyze", "application/json", bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("POST /analyze: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		body2, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, body2)
	}
}

// TestAnalyze_MalformedJSON — Case 4: POST /analyze with malformed JSON content → 422.
func TestAnalyze_MalformedJSON(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	content := []byte(`{ this is not valid json `)
	resp, err := http.Post(
		ts.URL+"/analyze",
		"application/json",
		analyzeRequestBody("json", content, "json"),
	)
	if err != nil {
		t.Fatalf("POST /analyze: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 422, got %d: %s", resp.StatusCode, body)
	}
}

// TestHealth — Case 5: GET /healthz → 200 "ok".
func TestHealth(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	// goa returns JSON-encoded string: "ok"
	var s string
	if err := json.Unmarshal(body, &s); err != nil {
		t.Fatalf("unmarshal health response: %v", err)
	}
	if s != "ok" {
		t.Fatalf("expected %q, got %q", "ok", s)
	}
}

// TestAnalyze_DirectService tests the service layer directly without HTTP.
func TestAnalyze_DirectService(t *testing.T) {
	pf := factory.NewParserFactory()
	af := factory.NewAnalyzerFactory()
	orch := application.NewAnalysisOrchestrator()
	svc := infraapi.NewAnalyzerService(pf, af, orch)

	content, err := os.ReadFile("../../testdata/clean.json")
	if err != nil {
		t.Fatalf("read clean.json: %v", err)
	}

	result, err := svc.Analyze(context.Background(), &analyzer.AnalyzePayload{
		Format:  "json",
		Content: content,
		Output:  "json",
	})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	t.Logf("clean.json: exit_code=%d, total=%d", result.ExitCode, result.Summary.Total)
}

// TestAnalyze_InvalidFormatDirect tests MakeInvalidFormat error directly.
func TestAnalyze_InvalidFormatDirect(t *testing.T) {
	pf := factory.NewParserFactory()
	af := factory.NewAnalyzerFactory()
	orch := application.NewAnalysisOrchestrator()
	svc := infraapi.NewAnalyzerService(pf, af, orch)

	_, err := svc.Analyze(context.Background(), &analyzer.AnalyzePayload{
		Format:  "unknown-format",
		Content: []byte("{}"),
		Output:  "json",
	})
	if err == nil {
		t.Fatal("expected error for unknown format")
	}
	t.Logf("got expected error: %v", err)
}
