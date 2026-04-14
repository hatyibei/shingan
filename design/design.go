package design

import (
	. "goa.design/goa/v3/dsl"
)

var _ = API("shingan", func() {
	Title("Shingan — AI Agent Workflow Static Analyzer API")
	Description("Static analysis for AI agent workflows")
	Version("0.1.0")
	Server("shingan", func() {
		Host("localhost", func() {
			URI("http://localhost:8080")
		})
	})
})

var _ = Service("analyzer", func() {
	Description("Analyze AI agent workflow definitions for structural issues")

	Method("analyze", func() {
		Payload(func() {
			Field(1, "format", String, "Input format: json or adk-go", func() {
				Enum("json", "adk-go")
				Example("json")
			})
			Field(2, "content", Bytes, "Raw workflow definition content")
			Field(3, "output", String, "Output format: json or markdown", func() {
				Enum("json", "markdown")
				Default("json")
			})
			Required("format", "content")
		})
		Result(AnalysisResult)
		HTTP(func() {
			POST("/analyze")
			Response(StatusOK)
			Response("invalid_format", StatusBadRequest)
			Response("parse_error", StatusUnprocessableEntity)
		})
		Error("invalid_format", ErrorResult)
		Error("parse_error", ErrorResult)
	})

	Method("health", func() {
		Result(String)
		HTTP(func() {
			GET("/healthz")
			Response(StatusOK)
		})
	})
})

var AnalysisResult = Type("AnalysisResult", func() {
	Field(1, "findings", ArrayOf(FindingOut), "Detected issues")
	Field(2, "summary", SummaryOut, "Severity counts")
	Field(3, "exit_code", Int, "Exit code analog: 0/1/2")
	Required("findings", "summary", "exit_code")
})

var FindingOut = Type("Finding", func() {
	Field(1, "rule", String)
	Field(2, "severity", String, func() { Enum("critical", "warning", "info") })
	Field(3, "node_id", String)
	Field(4, "message", String)
	Field(5, "suggestion", String)
	Required("rule", "severity", "node_id", "message")
})

var SummaryOut = Type("Summary", func() {
	Field(1, "total", Int)
	Field(2, "critical", Int)
	Field(3, "warning", Int)
	Field(4, "info", Int)
	Required("total", "critical", "warning", "info")
})
