// Package domain is a minimal stand-in for the real domain package, used only
// by the analyzer's analysistest fixtures (GOPATH mode).
package domain

type ConfidenceReason string

type Severity int

type Finding struct {
	RuleName         string
	Severity         Severity
	NodeID           string
	Message          string
	ConfidenceReason ConfidenceReason
}
