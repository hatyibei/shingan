package rules

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/hatyibei/shingan/domain"
)

// PromptInjectionSink flags workflow paths in which untrusted user-input nodes
// can reach an LLM prompt-template field, creating a prompt-injection attack
// surface.
//
// Tier: Path (ADR-007). Sources are nodes that look like user-input
// receptacles (Config["source"]=="user_input" or name patterns such as
// user_*, *_input, query, request); sinks are LLM nodes whose Config carries
// a prompt-template-like field (system_prompt / prompt_template /
// user_message_template / instruction / system).
//
// ConfidenceReason: ReasonHeuristicPattern. Both source and sink classification
// rely on naming + config-key heuristics rather than full taint analysis; the
// path traversal guarantees only structural reachability, not semantic flow.
//
// Severity rules (decided at sink classification time):
//   - sink has system-level field with {{var}} / ${var} / {var} substitution
//     → Critical (Confidence 0.9)
//   - sink has system-level field with no substitution
//     → Warning  (Confidence 0.7)
//   - sink has non-system template field with substitution
//     → Info     (Confidence 0.5)
type PromptInjectionSink struct{}

// NewPromptInjectionSink returns a ready-to-use PromptInjectionSink.
func NewPromptInjectionSink() *PromptInjectionSink {
	return &PromptInjectionSink{}
}

// Name returns the unique rule identifier.
func (p *PromptInjectionSink) Name() string {
	return "prompt_injection_sink"
}

// Meta returns the rule metadata used by the tier-aware orchestrator.
func (p *PromptInjectionSink) Meta() domain.RuleMeta {
	return domain.RuleMeta{
		Name:     p.Name(),
		Severity: domain.Critical,
		Fixable:  false,
	}
}

// userInputNamePattern matches node names that look like user-input
// receptacles (case-insensitive). Compiled once at package init.
var userInputNamePattern = regexp.MustCompile(`(?i)^(user[_\-].*|.*[_\-]input|query|request|user_query|user_request)$`)

// systemPromptKeys is the set of Config keys that indicate a system-level
// prompt template. These earn Critical / Warning severity depending on
// whether they contain substitution markers.
var systemPromptKeys = []string{"system_prompt", "system", "instruction", "instructions"}

// userTemplateKeys is the set of Config keys that indicate a non-system
// prompt template (typically rendered into a user-role message). These earn
// the lower-severity Info classification when paired with substitution.
var userTemplateKeys = []string{"prompt_template", "user_message_template", "user_template", "prompt"}

// substitutionPattern matches the three template-substitution syntaxes we
// recognise: {{var}}, ${var}, {var}. The pattern is intentionally permissive —
// we just need to know if any substitution slot exists.
var substitutionPattern = regexp.MustCompile(`(\{\{[^}]+\}\}|\$\{[^}]+\}|\{[A-Za-z_][A-Za-z0-9_\.]*\})`)

// isUserInputSource reports whether n looks like a user-input source: either
// Config["source"] is the literal string "user_input", or the node Name/ID
// matches the userInputNamePattern.
func isUserInputSource(n *domain.Node) bool {
	if n == nil {
		return false
	}
	if stringConfig(n, "source") == "user_input" {
		return true
	}
	if userInputNamePattern.MatchString(n.Name) {
		return true
	}
	if userInputNamePattern.MatchString(n.ID) {
		return true
	}
	return false
}

// classifySink inspects an LLM node's Config and returns the severity,
// confidence, and the offending Config key. ok is false when the node is not
// classified as a sink at all.
func classifySink(n *domain.Node) (severity domain.Severity, confidence float64, key string, hasSubstitution bool, ok bool) {
	if n == nil || n.Type != domain.NodeTypeLLM || n.Config == nil {
		return 0, 0, "", false, false
	}

	// System-level templates earn the highest severity.
	for _, k := range systemPromptKeys {
		v := stringConfig(n, k)
		if v == "" {
			continue
		}
		if substitutionPattern.MatchString(v) {
			return domain.Critical, 0.9, k, true, true
		}
		return domain.Warning, 0.7, k, false, true
	}

	// Non-system templates with substitution earn Info (still a sink,
	// since user content reaches a template — but the role separation in a
	// well-structured `messages` array makes it lower-risk).
	for _, k := range userTemplateKeys {
		v := stringConfig(n, k)
		if v == "" {
			continue
		}
		if substitutionPattern.MatchString(v) {
			return domain.Info, 0.5, k, true, true
		}
	}

	return 0, 0, "", false, false
}

// Sources implements domain.PathRule. It returns every node classified as a
// user-input source.
func (p *PromptInjectionSink) Sources(g *domain.WorkflowGraph) []*domain.Node {
	if g == nil {
		return nil
	}
	var out []*domain.Node
	for _, n := range g.Nodes {
		if isUserInputSource(n) {
			out = append(out, n)
		}
	}
	return out
}

// Sinks implements domain.PathRule. It returns every LLM node that classifies
// as a prompt-template sink (system or user-message).
func (p *PromptInjectionSink) Sinks(g *domain.WorkflowGraph) []*domain.Node {
	if g == nil {
		return nil
	}
	var out []*domain.Node
	for _, n := range g.Nodes {
		if _, _, _, _, ok := classifySink(n); ok {
			out = append(out, n)
		}
	}
	return out
}

// Propagate implements domain.PathRule. It runs reverse-BFS from each sink in
// ctx.Sinks and reports a Finding for every Source it discovers along the way.
//
// Unlike pii_leak_scanner, this rule has no Human-gate boundary: any
// reachable user-input source on the way back from an LLM template node is
// considered a finding. Sanitisation in this codebase is the user's
// responsibility outside the static graph.
func (p *PromptInjectionSink) Propagate(ctx *domain.PathContext) []domain.Finding {
	if ctx == nil || ctx.Graph == nil || len(ctx.Sources) == 0 || len(ctx.Sinks) == 0 {
		return nil
	}
	return runPromptInjectionReverseBFS(ctx.Graph, ctx.Reverse, ctx.Sources, ctx.Sinks)
}

// Analyze keeps the legacy AnalysisRule contract alive for callers that have
// not yet migrated to the tier-aware orchestrator.
func (p *PromptInjectionSink) Analyze(graph *domain.WorkflowGraph) []domain.Finding {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil
	}
	reverse := make(map[string][]domain.Edge, len(graph.Nodes))
	for _, e := range graph.Edges {
		reverse[e.To] = append(reverse[e.To], e)
	}
	sources := p.Sources(graph)
	sinks := p.Sinks(graph)
	if len(sources) == 0 || len(sinks) == 0 {
		return nil
	}
	return runPromptInjectionReverseBFS(graph, reverse, sources, sinks)
}

// runPromptInjectionReverseBFS performs the path traversal shared by
// Propagate and the legacy Analyze fallback. It walks backwards from each
// sink and emits one Finding per (sink, source) pair reachable in reverse.
func runPromptInjectionReverseBFS(graph *domain.WorkflowGraph, reverse map[string][]domain.Edge, sources []*domain.Node, sinks []*domain.Node) []domain.Finding {
	sourceIDs := make(map[string]*domain.Node, len(sources))
	for _, n := range sources {
		sourceIDs[n.ID] = n
	}

	var findings []domain.Finding

	for _, sinkNode := range sinks {
		severity, confidence, key, hasSub, ok := classifySink(sinkNode)
		if !ok {
			// Defensive: Sinks() returned this node, classifySink should
			// agree. If it doesn't, skip rather than panic.
			continue
		}

		visited := make(map[string]bool, len(graph.Nodes))
		visited[sinkNode.ID] = true
		queue := []string{sinkNode.ID}

		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]

			for _, edge := range reverse[current] {
				pred := edge.From
				if visited[pred] {
					continue
				}
				visited[pred] = true

				if src, isSrc := sourceIDs[pred]; isSrc {
					findings = append(findings, buildPromptInjectionFinding(src, sinkNode, severity, confidence, key, hasSub))
				}

				queue = append(queue, pred)
			}
		}
	}

	return findings
}

// buildPromptInjectionFinding constructs the Finding for a (source, sink)
// pair. It centralises message / suggestion templating so the BFS loop stays
// compact.
func buildPromptInjectionFinding(source *domain.Node, sink *domain.Node, severity domain.Severity, confidence float64, key string, hasSubstitution bool) domain.Finding {
	var (
		msg        string
		suggestion string
	)

	switch severity {
	case domain.Critical:
		msg = fmt.Sprintf(
			"prompt-injection sink: user-input node %q reaches LLM %q via Config[%q] which contains template substitution (%s)",
			source.ID, sink.ID, key, summariseSubstitution(stringConfig(sink, key)),
		)
		suggestion = "user input is reaching a system prompt template — sanitize / validate / use parameterized message structure (separate `messages` array with `role: user`)"
	case domain.Warning:
		msg = fmt.Sprintf(
			"prompt-injection sink: user-input node %q reaches LLM %q which sets Config[%q] (no template substitution detected; review for direct concatenation upstream)",
			source.ID, sink.ID, key,
		)
		suggestion = "review whether user input should reach this LLM node directly without an explicit sanitization step"
	case domain.Info:
		msg = fmt.Sprintf(
			"prompt-injection sink: user-input node %q reaches LLM %q via Config[%q] (non-system template); ensure user content is rendered into a `user`-role message rather than concatenated into a system instruction",
			source.ID, sink.ID, key,
		)
		suggestion = "keep user content in a `role: user` message and reserve system templates for trusted strings"
	}

	return domain.Finding{
		RuleName:         "prompt_injection_sink",
		Severity:         severity,
		NodeID:           sink.ID,
		Message:          msg,
		Suggestion:       suggestion,
		Confidence:       confidence,
		ConfidenceReason: domain.ReasonHeuristicPattern,
	}
}

// summariseSubstitution returns the first matched substitution token from
// template, or the literal string "{{...}}" when no match (defensive — the
// caller has already verified hasSubstitution).
func summariseSubstitution(template string) string {
	loc := substitutionPattern.FindStringIndex(template)
	if loc == nil {
		return "{{...}}"
	}
	tok := template[loc[0]:loc[1]]
	// Trim oversized captures so error messages stay readable.
	if len(tok) > 32 {
		tok = tok[:29] + "..."
	}
	return strings.TrimSpace(tok)
}

func init() {
	registerBuiltin(NewPromptInjectionSink())
}
