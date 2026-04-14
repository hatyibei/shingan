package factory_test

import (
	"testing"

	"github.com/hatyibei/shingan/infrastructure/factory"
	"github.com/hatyibei/shingan/infrastructure/reporter"
)

func TestReporterFactory_JSON(t *testing.T) {
	f := factory.NewReporterFactory()
	r, err := f.Create("json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := r.(*reporter.JSONReporter); !ok {
		t.Errorf("expected *reporter.JSONReporter, got %T", r)
	}
}

func TestReporterFactory_Markdown(t *testing.T) {
	f := factory.NewReporterFactory()
	r, err := f.Create("markdown")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := r.(*reporter.MarkdownReporter); !ok {
		t.Errorf("expected *reporter.MarkdownReporter, got %T", r)
	}
}

func TestReporterFactory_UnknownFormat(t *testing.T) {
	f := factory.NewReporterFactory()
	r, err := f.Create("xml")
	if err == nil {
		t.Fatal("expected error for unknown format, got nil")
	}
	if r != nil {
		t.Errorf("expected nil reporter on error, got %T", r)
	}
}

func TestReporterFactory_ContentType_JSON(t *testing.T) {
	f := factory.NewReporterFactory()
	r, err := f.Create("json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := r.ContentType(); got != "application/json" {
		t.Errorf("ContentType() = %q, want %q", got, "application/json")
	}
}

func TestReporterFactory_ContentType_Markdown(t *testing.T) {
	f := factory.NewReporterFactory()
	r, err := f.Create("markdown")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := r.ContentType(); got != "text/markdown" {
		t.Errorf("ContentType() = %q, want %q", got, "text/markdown")
	}
}

func TestReporterFactory_EmptyFormat(t *testing.T) {
	f := factory.NewReporterFactory()
	r, err := f.Create("")
	if err == nil {
		t.Fatal("expected error for empty format, got nil")
	}
	if r != nil {
		t.Errorf("expected nil reporter on error, got %T", r)
	}
}

func TestReporterFactory_SARIF(t *testing.T) {
	f := factory.NewReporterFactory()
	r, err := f.Create("sarif")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := r.(*reporter.SARIFReporter); !ok {
		t.Errorf("expected *reporter.SARIFReporter, got %T", r)
	}
}

func TestReporterFactory_ContentType_SARIF(t *testing.T) {
	f := factory.NewReporterFactory()
	r, err := f.Create("sarif")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := r.ContentType(); got != "application/sarif+json" {
		t.Errorf("ContentType() = %q, want %q", got, "application/sarif+json")
	}
}
