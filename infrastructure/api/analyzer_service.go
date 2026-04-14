// Package api implements the goa analyzer.Service interface.
// This package sits in the infrastructure layer (Onion Architecture) and
// wires together the application-layer Orchestrator with the factories.
package api

import (
	"context"
	"fmt"

	analyzer "github.com/hatyibei/shingan/gen/analyzer"
	"github.com/hatyibei/shingan/application"
	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/infrastructure/factory"
)

// AnalyzerService implements analyzer.Service (goa generated interface).
type AnalyzerService struct {
	parserFactory   *factory.ParserFactory
	analyzerFactory *factory.AnalyzerFactory
	orchestrator    *application.AnalysisOrchestrator
}

// NewAnalyzerService returns an AnalyzerService with all dependencies injected.
func NewAnalyzerService(
	pf *factory.ParserFactory,
	af *factory.AnalyzerFactory,
	orch *application.AnalysisOrchestrator,
) *AnalyzerService {
	return &AnalyzerService{
		parserFactory:   pf,
		analyzerFactory: af,
		orchestrator:    orch,
	}
}

// Analyze implements the "analyze" method of analyzer.Service.
func (s *AnalyzerService) Analyze(ctx context.Context, p *analyzer.AnalyzePayload) (*analyzer.AnalysisResult, error) {
	// 1. Resolve parser from format string.
	parser, err := s.parserFactory.Create(p.Format)
	if err != nil {
		return nil, analyzer.MakeInvalidFormat(fmt.Errorf("unsupported format %q: %w", p.Format, err))
	}

	// 2. Parse the raw workflow definition.
	graph, err := parser.Parse(p.Content)
	if err != nil {
		return nil, analyzer.MakeParseError(fmt.Errorf("parse %q: %w", p.Format, err))
	}

	// 3. Run all analysis rules via the orchestrator.
	rules := s.analyzerFactory.CreateAll()
	findings := s.orchestrator.Analyze(graph, rules)

	// 4. Map domain findings → goa generated types.
	outFindings := make([]*analyzer.Finding, 0, len(findings))
	counts := map[domain.Severity]int{
		domain.Critical: 0,
		domain.Warning:  0,
		domain.Info:     0,
	}

	for _, f := range findings {
		out := &analyzer.Finding{
			Rule:     f.RuleName,
			Severity: f.Severity.String(),
			NodeID:   f.NodeID,
			Message:  f.Message,
		}
		if f.Suggestion != "" {
			s := f.Suggestion
			out.Suggestion = &s
		}
		outFindings = append(outFindings, out)
		counts[f.Severity]++
	}

	// 5. Compute exit_code: 2=critical, 1=warning, 0=clean
	exitCode := 0
	if counts[domain.Critical] > 0 {
		exitCode = 2
	} else if counts[domain.Warning] > 0 {
		exitCode = 1
	}

	return &analyzer.AnalysisResult{
		Findings: outFindings,
		Summary: &analyzer.Summary{
			Total:    len(findings),
			Critical: counts[domain.Critical],
			Warning:  counts[domain.Warning],
			Info:     counts[domain.Info],
		},
		ExitCode: exitCode,
	}, nil
}

// Health implements the "health" method of analyzer.Service.
func (s *AnalyzerService) Health(ctx context.Context) (string, error) {
	return "ok", nil
}
