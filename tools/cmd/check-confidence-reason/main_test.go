package main

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestConfidenceReasonAnalyzer(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Analyzer, "a")
}
