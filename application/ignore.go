// Package application — `shingan: ignore` comment support.
//
// Static analysers earn developer trust by letting users opt out of specific
// findings without modifying CI policy. We support two markers, in the
// ESLint / golangci-lint tradition:
//
//	# shingan: ignore <rule_name>            # current line
//	# shingan: ignore-next-line <rule_name>  # next line
//	# shingan: ignore-file <rule_name>       # entire file
//
// Multiple rule names may be comma-separated. Omitting the rule name
// disables ALL rules for that scope. Both `#` (Python / YAML) and `//`
// (Go / TS) comment prefixes are recognised so the same syntax works
// across every framework.
//
// Filtering happens in the orchestrator after rules run. Findings with
// SourcePos populated are matched against the ignore index built from
// the source file; findings without source position fall through (per-
// file ignore still works because `Finding.SourceFile` is set by
// `AnalyzeMulti`).

package application

import (
	"bufio"
	"os"
	"regexp"
	"strings"

	"github.com/hatyibei/shingan/domain"
)

// IgnoreIndex holds the parsed `shingan: ignore` markers for a single
// source file. Per-line and file-level scopes are stored separately so
// the orchestrator can look up "is this finding suppressed?" cheaply.
type IgnoreIndex struct {
	// fileLevel rules suppressed for the entire file.
	// Special value "*" means "ignore all rules".
	fileLevel map[string]bool
	// perLine[lineNumber] = set of rule names disabled on that line.
	perLine map[int]map[string]bool
}

// IgnoreSets a finding be suppressed under this index?
func (idx *IgnoreIndex) Suppressed(rule string, line int) bool {
	if idx == nil {
		return false
	}
	if idx.fileLevel["*"] || idx.fileLevel[rule] {
		return true
	}
	if line <= 0 {
		return false
	}
	if rules, ok := idx.perLine[line]; ok {
		if rules["*"] || rules[rule] {
			return true
		}
	}
	return false
}

// Regex captures `shingan: ignore[-file|-next-line|-line] [rule, rule, …]`
// after either `#` or `//`. The leading whitespace lets the marker sit
// anywhere on a line (end-of-line or its own line).
//
// Groups:
//
//	1: scope ("file", "next-line", "line", or "" for current line)
//	2: comma-separated rule list, or "" if "ignore" alone (= all rules)
var ignoreMarkerRe = regexp.MustCompile(
	`(?:#|//)\s*shingan\s*:\s*ignore(?:-(file|next-line|line))?\b\s*(.*)$`,
)

// LoadIgnoreIndex parses `path` and returns its IgnoreIndex. Unreadable
// files yield an empty (non-nil) index — ignore is best-effort and
// must not break the analysis.
func LoadIgnoreIndex(path string) *IgnoreIndex {
	idx := &IgnoreIndex{
		fileLevel: map[string]bool{},
		perLine:   map[int]map[string]bool{},
	}
	if path == "" {
		return idx
	}
	fp, err := os.Open(path)
	if err != nil {
		return idx
	}
	defer fp.Close()

	scanner := bufio.NewScanner(fp)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		matches := ignoreMarkerRe.FindStringSubmatch(line)
		if matches == nil {
			continue
		}
		scope := matches[1]
		rules := parseRuleList(matches[2])

		switch scope {
		case "file":
			for _, r := range rules {
				idx.fileLevel[r] = true
			}
		case "next-line":
			target := lineNum + 1
			if idx.perLine[target] == nil {
				idx.perLine[target] = map[string]bool{}
			}
			for _, r := range rules {
				idx.perLine[target][r] = true
			}
		default:
			// "line" or empty (= same line)
			if idx.perLine[lineNum] == nil {
				idx.perLine[lineNum] = map[string]bool{}
			}
			for _, r := range rules {
				idx.perLine[lineNum][r] = true
			}
		}
	}
	return idx
}

// parseRuleList accepts the trailing portion of the marker comment and
// returns a slice of rule names. Empty input → ["*"] (ignore-all).
func parseRuleList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{"*"}
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})
	if len(parts) == 0 {
		return []string{"*"}
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		// Strip trailing punctuation (period, semicolon) — common when
		// the comment ends a sentence.
		p = strings.Trim(p, ".;:")
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// PosResolver returns (filePath, lineNumber) for a (sourceFile, nodeID)
// tuple. Concrete implementations look up `nodeID` in the originating
// graph's node table and read its `Pos`. Returning ("", 0) means no
// position is available — file-level ignore still works.
type PosResolver func(sourceFile, nodeID string) (string, int)

// FilterIgnoredFindings removes findings whose source line carries a
// matching `shingan: ignore` marker. The caller supplies a PosResolver
// so the application layer doesn't need access to graph internals.
//
// Source files are loaded lazily and cached per call so a directory
// walk doesn't re-read the same file for every finding.
func FilterIgnoredFindings(findings []domain.Finding, resolve PosResolver) []domain.Finding {
	if len(findings) == 0 {
		return findings
	}
	cache := map[string]*IgnoreIndex{}
	out := findings[:0]
	for _, f := range findings {
		filePath, line := "", 0
		if resolve != nil {
			filePath, line = resolve(f.SourceFile, f.NodeID)
		}
		if filePath == "" {
			filePath = f.SourceFile
		}
		if filePath != "" {
			idx, ok := cache[filePath]
			if !ok {
				idx = LoadIgnoreIndex(filePath)
				cache[filePath] = idx
			}
			if idx.Suppressed(f.RuleName, line) {
				continue
			}
		}
		out = append(out, f)
	}
	return out
}

// MakeGraphPosResolver builds a PosResolver from a graph + sourceFile
// pair. Use AnalyzeMulti's internal map to wire several sources at once.
func MakeGraphPosResolver(sourceFile string, graph *domain.WorkflowGraph) PosResolver {
	if graph == nil || graph.Nodes == nil {
		return func(_ string, _ string) (string, int) { return sourceFile, 0 }
	}
	return func(_ string, nodeID string) (string, int) {
		n, ok := graph.Nodes[nodeID]
		if !ok || n == nil {
			return sourceFile, 0
		}
		file := n.Pos.File
		if file == "" {
			file = sourceFile
		}
		return file, n.Pos.Line
	}
}
