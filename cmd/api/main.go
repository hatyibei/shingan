// Package main starts the Shingan HTTP API server using goa-generated handlers.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hatyibei/shingan/application"
	infraapi "github.com/hatyibei/shingan/infrastructure/api"
	"github.com/hatyibei/shingan/infrastructure/factory"

	analyzer "github.com/hatyibei/shingan/gen/analyzer"
	genserver "github.com/hatyibei/shingan/gen/http/analyzer/server"
	goahttp "goa.design/goa/v3/http"
)

func main() {
	// --- Dependency injection ---
	pf := factory.NewParserFactory()
	af := factory.NewAnalyzerFactory()
	orch := application.NewAnalysisOrchestrator()

	svc := infraapi.NewAnalyzerService(pf, af, orch)

	// --- goa endpoints & HTTP server ---
	endpoints := analyzer.NewEndpoints(svc)

	mux := goahttp.NewMuxer()
	srv := genserver.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil)
	srv.Mount(mux)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addr := ":" + port

	httpServer := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// --- Graceful shutdown ---
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		log.Printf("shingan API listening on %s", addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("ListenAndServe: %v", err)
		}
	}()

	<-quit
	log.Println("shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		log.Fatalf("shutdown error: %v", err)
	}
	log.Println("server stopped")
}
