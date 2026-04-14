package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/hatyibei/shingan/domain"
)

// runRequestBody is a minimal struct for extracting the app/agent name from
// ADK REST API run requests.  Only AppName is decoded; remaining fields are ignored.
type runRequestBody struct {
	AppName string `json:"appName"`
}

// guardResponse is the JSON payload returned on 403.
type guardResponse struct {
	Error    string           `json:"error"`
	Agent    string           `json:"agent"`
	Findings []findingSummary `json:"findings"`
}

// findingSummary is a serializable summary of a domain.Finding.
type findingSummary struct {
	Rule       string `json:"rule"`
	Severity   string `json:"severity"`
	NodeID     string `json:"nodeId"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion"`
}

// shinganGuardMiddleware returns a gorilla/mux-compatible middleware that
// intercepts ADK run requests and runs Shingan static analysis before allowing
// execution.
//
// Interception paths (POST): /api/run, /api/run_sse
//   - If the agent has Critical findings → 403 + JSON error body.
//   - Otherwise (or for non-run paths) → pass through to next handler.
//
// The request body is fully read, inspected, then restored so downstream
// handlers can decode it normally.
func shinganGuardMiddleware(sourceMap map[string]string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only intercept POST requests to /api/run and /api/run_sse.
			if r.Method != http.MethodPost || !isRunPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			// Read the full body.
			bodyBytes, err := io.ReadAll(r.Body)
			if err != nil {
				log.Printf("[shingan] failed to read request body: %v", err)
				next.ServeHTTP(w, r)
				return
			}
			// Restore the body for downstream handlers.
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

			// Decode only the appName field.
			var req runRequestBody
			if err := json.Unmarshal(bodyBytes, &req); err != nil || req.AppName == "" {
				// Body unreadable or missing appName — pass through, let ADK return 400.
				next.ServeHTTP(w, r)
				return
			}

			agentName := req.AppName
			log.Printf("[shingan] guard check: agent=%q path=%s", agentName, r.URL.Path)

			// Run Shingan static analysis on the agent's source file.
			findings, err := analyzeAgentSource(agentName, sourceMap)
			if err != nil {
				// Unknown agent or analysis error — log and pass through.
				log.Printf("[shingan] analysis skipped: %v", err)
				next.ServeHTTP(w, r)
				return
			}

			criticalFindings := filterCritical(findings)
			if len(criticalFindings) == 0 {
				log.Printf("[shingan] PASSED: agent=%q (no critical findings)", agentName)
				next.ServeHTTP(w, r)
				return
			}

			// Critical findings — block execution with 403.
			log.Printf("[shingan] BLOCKED: agent=%q critical_count=%d", agentName, len(criticalFindings))
			writeGuardError(w, agentName, criticalFindings)
		})
	}
}

// isRunPath returns true for the ADK run/run_sse API paths.
func isRunPath(path string) bool {
	return strings.HasSuffix(path, "/run") || strings.HasSuffix(path, "/run_sse")
}

// filterCritical returns only Critical-severity findings.
func filterCritical(findings []domain.Finding) []domain.Finding {
	var out []domain.Finding
	for _, f := range findings {
		if f.Severity == domain.Critical {
			out = append(out, f)
		}
	}
	return out
}

// writeGuardError writes a 403 JSON response with Shingan finding details.
func writeGuardError(w http.ResponseWriter, agentName string, criticalFindings []domain.Finding) {
	summaries := make([]findingSummary, len(criticalFindings))
	for i, f := range criticalFindings {
		summaries[i] = findingSummary{
			Rule:       f.RuleName,
			Severity:   f.Severity.String(),
			NodeID:     f.NodeID,
			Message:    f.Message,
			Suggestion: f.Suggestion,
		}
	}

	resp := guardResponse{
		Error:    "shingan_guard",
		Agent:    agentName,
		Findings: summaries,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("[shingan] failed to encode guard response: %v", err)
	}
}
