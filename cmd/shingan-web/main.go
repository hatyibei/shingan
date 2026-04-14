// Package main is the entry point for shingan-web.
//
// shingan-web starts an ADK Web UI server with a Shingan pre-execution guard
// middleware injected into the Run API. When the ADK Web UI sends a run
// request for an agent with Critical static-analysis findings, the middleware
// returns HTTP 403 before the request ever reaches the ADK runtime.
//
// Usage:
//
//	./shingan-web
//
// Environment variables (required):
//
//	GOOGLE_CLOUD_PROJECT   — GCP project ID for Vertex AI
//	GOOGLE_CLOUD_LOCATION  — Vertex AI region (e.g. us-central1)
//	GOOGLE_GENAI_USE_VERTEXAI=true
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/adk/artifact"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/cmd/launcher/web"
	"google.golang.org/adk/cmd/launcher/web/webui"
	"google.golang.org/adk/server/adkrest"
	"google.golang.org/adk/session"
)

const (
	listenPort   = ":8080"
	projectID    = "axial-mercury-486503-j5"
	location     = "us-central1"
	geminiModel  = "gemini-2.0-flash-001"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "shingan-web:", err)
		os.Exit(1)
	}
}

// addCORSHeaders wraps an http.Handler adding permissive CORS headers.
// Since the UI and API share the same origin (localhost:8080), this is
// for cross-tool compatibility only.
func addCORSHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "http://localhost:8080")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func run(ctx context.Context) error {
	log.Println("=== Shingan Web UI Demo ===")
	log.Println("Building demo agents...")

	agentLoader, sourceMap, err := buildDemoLoader(ctx)
	if err != nil {
		return fmt.Errorf("build demo agents: %w", err)
	}

	config := &launcher.Config{
		SessionService:  session.InMemoryService(),
		ArtifactService: artifact.InMemoryService(),
		AgentLoader:     agentLoader,
	}

	// Build base gorilla/mux router (exported from web package).
	router := web.BuildBaseRouter()

	// Register Shingan guard middleware BEFORE ADK routes are added.
	// router.Use applies middleware to all routes registered afterwards.
	router.Use(shinganGuardMiddleware(sourceMap))

	// Set up ADK REST API server directly (api.SetupSubrouters panics in v1.1.0
	// because it passes nil DebugConfig to adkrest.NewServer; we bypass it by
	// calling adkrest.NewServer ourselves with an explicit empty DebugConfig).
	restServer, err := adkrest.NewServer(adkrest.ServerConfig{
		SessionService:  config.SessionService,
		MemoryService:   config.MemoryService,
		AgentLoader:     config.AgentLoader,
		ArtifactService: config.ArtifactService,
		SSEWriteTimeout: 120 * time.Second, // match api.NewLauncher default; zero causes instant SSE timeout
		DebugConfig:     &adkrest.DebugTelemetryConfig{}, // avoid nil pointer in ADK v1.1.0
	})
	if err != nil {
		return fmt.Errorf("create ADK REST server: %w", err)
	}

	// Mount REST API under /api prefix with CORS for same-origin (localhost:8080).
	router.Methods("GET", "POST", "DELETE", "OPTIONS").
		PathPrefix("/api").
		Handler(http.StripPrefix("/api", addCORSHeaders(restServer)))

	// Initialize and configure the WebUI sublauncher.
	webuiSL := webui.NewLauncher()
	if _, err := webuiSL.Parse([]string{}); err != nil {
		return fmt.Errorf("webui sublauncher parse: %w", err)
	}
	if err := webuiSL.SetupSubrouters(router, config); err != nil {
		return fmt.Errorf("webui sublauncher setup: %w", err)
	}

	srv := &http.Server{
		Addr:         listenPort,
		Handler:      router,
		WriteTimeout: 120 * time.Second,
		ReadTimeout:  15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("ADK Web UI + Shingan guard started on http://localhost%s", listenPort)
	log.Println()
	log.Println("Demo agents:")
	log.Println("  - infinite_loop_unbounded  → Shingan BLOCKS (Critical: no MaxIterations)")
	log.Println("  - infinite_loop_bounded    → Shingan PASSES (MaxIterations=3)")
	log.Println("  - simple_hello             → Shingan PASSES (clean LlmAgent)")
	log.Println()
	log.Printf("Open http://localhost%s in your browser", listenPort)

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		log.Println("Shutting down...")
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutCancel()
		return srv.Shutdown(shutCtx)
	case err, ok := <-errCh:
		if !ok {
			return nil
		}
		return fmt.Errorf("server error: %w", err)
	}
}
