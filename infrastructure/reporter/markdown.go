package reporter

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/hatyibei/shingan/domain"
)

// confidenceLabel converts a confidence value to a display string with optional warning mark.
func confidenceLabel(c float64) string {
	pct := int(c * 100)
	if c < 0.7 {
		return fmt.Sprintf("⚠ %d%%", pct)
	}
	return fmt.Sprintf("%d%%", pct)
}

// MarkdownReporter implements application.ReportFormatter for human-readable Markdown output.
type MarkdownReporter struct{}

// NewMarkdownReporter returns a new MarkdownReporter.
func NewMarkdownReporter() *MarkdownReporter {
	return &MarkdownReporter{}
}

// ContentType returns the MIME type for Markdown output.
func (r *MarkdownReporter) ContentType() string {
	return "text/markdown"
}

// Format serializes findings into Markdown bytes.
// Findings are grouped by severity (Critical → Warning → Info).
// Each group is rendered as a GFM table.
func (r *MarkdownReporter) Format(findings []domain.Finding) ([]byte, error) {
	var buf bytes.Buffer

	buf.WriteString("# Shingan Analysis Report\n\n")

	// Group by severity in display order: Critical first, then Warning, then Info.
	groups := []struct {
		severity domain.Severity
		label    string
	}{
		{domain.Critical, "Critical"},
		{domain.Warning, "Warning"},
		{domain.Info, "Info"},
	}

	total := len(findings)
	critCount, warnCount, infoCount := 0, 0, 0
	for _, f := range findings {
		switch f.Severity {
		case domain.Critical:
			critCount++
		case domain.Warning:
			warnCount++
		case domain.Info:
			infoCount++
		}
	}

	// Summary section
	fmt.Fprintf(&buf, "## Summary\n\n")
	fmt.Fprintf(&buf, "| Total | Critical | Warning | Info |\n")
	fmt.Fprintf(&buf, "|-------|----------|---------|------|\n")
	fmt.Fprintf(&buf, "| %d | %d | %d | %d |\n\n", total, critCount, warnCount, infoCount)

	if total == 0 {
		buf.WriteString("No findings. The workflow looks clean!\n")
		return buf.Bytes(), nil
	}

	// Per-severity sections
	for _, g := range groups {
		var section []domain.Finding
		for _, f := range findings {
			if f.Severity == g.severity {
				section = append(section, f)
			}
		}
		if len(section) == 0 {
			continue
		}

		fmt.Fprintf(&buf, "## %s\n\n", g.label)
		buf.WriteString("| Rule | Node | Confidence | Message | Suggestion |\n")
		buf.WriteString("|------|------|------------|---------|------------|\n")
		for _, f := range section {
			nodeID := f.NodeID
			if nodeID == "" {
				nodeID = "(graph)"
			}
			fmt.Fprintf(&buf, "| %s | %s | %s | %s | %s |\n",
				escapeMD(f.RuleName),
				escapeMD(nodeID),
				confidenceLabel(f.Confidence),
				escapeMD(f.Message),
				escapeMD(f.Suggestion),
			)
		}
		buf.WriteString("\n")
	}

	return buf.Bytes(), nil
}

// escapeMD escapes pipe characters inside table cells.
func escapeMD(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}
