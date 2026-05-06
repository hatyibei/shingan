package factory_test

import (
	"testing"

	"github.com/hatyibei/shingan/infrastructure/factory"
	"github.com/hatyibei/shingan/infrastructure/parser"
)

func TestParserFactory_JSON(t *testing.T) {
	f := factory.NewParserFactory()
	p, err := f.Create("json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := p.(*parser.JSONParser); !ok {
		t.Errorf("expected *parser.JSONParser, got %T", p)
	}
	if got := p.SupportedFormat(); got != "json" {
		t.Errorf("SupportedFormat() = %q, want \"json\"", got)
	}
}

func TestParserFactory_ADKGo(t *testing.T) {
	f := factory.NewParserFactory()
	p, err := f.Create("adk-go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := p.(*parser.ADKGoParser); !ok {
		t.Errorf("expected *parser.ADKGoParser, got %T", p)
	}
	if got := p.SupportedFormat(); got != "adk-go" {
		t.Errorf("SupportedFormat() = %q, want \"adk-go\"", got)
	}
}

func TestParserFactory_Samurai(t *testing.T) {
	f := factory.NewParserFactory()
	p, err := f.Create("samurai")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := p.(*parser.SamuraiParser); !ok {
		t.Errorf("expected *parser.SamuraiParser, got %T", p)
	}
	if got := p.SupportedFormat(); got != "samurai" {
		t.Errorf("SupportedFormat() = %q, want \"samurai\"", got)
	}
}

func TestParserFactory_SamuraiParserCanParseValidInput(t *testing.T) {
	f := factory.NewParserFactory()
	p, err := f.Create("samurai")
	if err != nil {
		t.Fatalf("Create(samurai): %v", err)
	}
	input := []byte(`{
		"version": "1.0",
		"workflow_id": "wf_test",
		"entry_node": "n1",
		"nodes": [{"id": "n1", "type": "llm", "name": "テスト", "config": {}}],
		"edges": []
	}`)
	graph, err := p.Parse(input)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}
	if graph.EntryNodeID != "n1" {
		t.Errorf("EntryNodeID = %q, want \"n1\"", graph.EntryNodeID)
	}
}

func TestParserFactory_N8n(t *testing.T) {
	f := factory.NewParserFactory()
	p, err := f.Create("n8n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := p.(*parser.N8nParser); !ok {
		t.Errorf("expected *parser.N8nParser, got %T", p)
	}
	if got := p.SupportedFormat(); got != "n8n" {
		t.Errorf("SupportedFormat() = %q, want \"n8n\"", got)
	}
}

func TestParserFactory_N8nParserCanParseValidInput(t *testing.T) {
	f := factory.NewParserFactory()
	p, err := f.Create("n8n")
	if err != nil {
		t.Fatalf("Create(n8n): %v", err)
	}
	input := []byte(`{
		"name": "wf",
		"nodes": [{"id": "1", "name": "Webhook", "type": "n8n-nodes-base.webhook", "parameters": {}, "position": [0, 0]}],
		"connections": {}
	}`)
	graph, err := p.Parse(input)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}
	if graph.EntryNodeID != "Webhook" {
		t.Errorf("EntryNodeID = %q, want \"Webhook\"", graph.EntryNodeID)
	}
}

func TestParserFactory_UnknownFormat(t *testing.T) {
	f := factory.NewParserFactory()
	p, err := f.Create("yaml")
	if err == nil {
		t.Fatal("expected error for unknown format, got nil")
	}
	if p != nil {
		t.Errorf("expected nil parser on error, got %T", p)
	}
}

func TestParserFactory_EmptyFormat(t *testing.T) {
	f := factory.NewParserFactory()
	p, err := f.Create("")
	if err == nil {
		t.Fatal("expected error for empty format, got nil")
	}
	if p != nil {
		t.Errorf("expected nil parser on error, got %T", p)
	}
}

func TestParserFactory_JSONParserCanParseValidInput(t *testing.T) {
	f := factory.NewParserFactory()
	p, err := f.Create("json")
	if err != nil {
		t.Fatalf("Create(json): %v", err)
	}
	input := []byte(`{"nodes":[{"id":"n1","name":"N","type":"llm"}],"edges":[],"entry_node_id":"n1"}`)
	graph, err := p.Parse(input)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}
	if graph.EntryNodeID != "n1" {
		t.Errorf("EntryNodeID = %q, want %q", graph.EntryNodeID, "n1")
	}
}
