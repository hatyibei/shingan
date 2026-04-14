// Package parser provides WorkflowParser implementations for different input formats.
package parser

import (
	"encoding/json"
	"fmt"

	"github.com/hatyibei/shingan/domain"
)

// JSONParser parses WorkflowGraph from the Shingan JSON format.
// The JSON format stores nodes as an array (easier hand-authoring)
// and the domain type uses map[string]*Node — WorkflowGraph.UnmarshalJSON handles the conversion.
type JSONParser struct{}

// NewJSONParser returns a ready-to-use JSONParser.
func NewJSONParser() *JSONParser {
	return &JSONParser{}
}

// SupportedFormat implements application.WorkflowParser.
func (p *JSONParser) SupportedFormat() string {
	return "json"
}

// Parse deserializes a WorkflowGraph from Shingan JSON format bytes.
func (p *JSONParser) Parse(input []byte) (*domain.WorkflowGraph, error) {
	var graph domain.WorkflowGraph
	if err := json.Unmarshal(input, &graph); err != nil {
		return nil, fmt.Errorf("json parser: unmarshal WorkflowGraph: %w", err)
	}
	return &graph, nil
}
