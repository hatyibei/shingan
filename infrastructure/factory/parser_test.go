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
