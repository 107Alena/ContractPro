// Package main is the entrypoint of the Legal Intelligence Core (LIC) service.
//
// Lifecycle:
//
//  1. Load configuration (fail-fast on missing/invalid env vars).
//  2. Construct the App via app.New() (every dependency wired in one place).
//  3. Block in app.Run() until ctx is cancelled or the HTTP server errors.
//  4. On SIGINT / SIGTERM — cancel ctx and run app.Shutdown() with a bounded
//     deadline.
//  5. On SIGHUP — invoke app.ReloadSecrets() to refresh LLM API keys (best
//     effort; v1 logs a warning and requires a rolling restart).
//
// Errors at any stage exit non-zero so the orchestrator (Kubernetes /
// systemd) restarts the pod.
package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"contractpro/legal-intelligence-core/internal/app"
	"contractpro/legal-intelligence-core/internal/config"
)

// version / commit are overridden via -ldflags "-X main.version=... -X main.commit=..."
// at build time (see Makefile docker-build target and Dockerfile ARG VERSION).
var (
	version = "dev"
	commit  = "unknown"
)

// shutdownGrace is the wall-clock budget for the shutdown sequence on a fatal
// signal. The actual in-flight drain budget is cfg.App.ShutdownTimeout; this
// outer ctx caps the whole sequence (drain + broker close + redis close + OTel
// flush) so a stuck Close() cannot block indefinitely.
const shutdownGrace = 150 * time.Second

func main() {
	log.Printf("lic-service starting (version=%s commit=%s)", version, commit)

	cfg, err := config.Load()
	if err != nil {
		// fail-fast on misconfiguration (LIC-TASK-003 acceptance test step 3).
		log.Fatalf("lic-service: configuration load failed: %v", err)
	}

	// Root ctx is cancelled on SIGINT / SIGTERM.
	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	application, err := app.New(rootCtx, cfg, app.BuildInfo{
		Version:   version,
		Commit:    commit,
		GoVersion: runtime.Version(),
	})
	if err != nil {
		log.Fatalf("lic-service: app build failed: %v", err)
	}

	// signal handler — SIGTERM/SIGINT trigger Shutdown; SIGHUP triggers
	// ReloadSecrets (best-effort).
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)

	go func() {
		for sig := range signals {
			switch sig {
			case syscall.SIGHUP:
				if rerr := application.ReloadSecrets(rootCtx); rerr != nil {
					log.Printf("lic-service: SIGHUP reload failed: %v", rerr)
				}
			case syscall.SIGINT, syscall.SIGTERM:
				log.Printf("lic-service: received %s, initiating shutdown", sig)
				rootCancel()
				return
			}
		}
	}()

	runErr := application.Run(rootCtx)

	// Shutdown sequence under a bounded outer deadline.
	sdCtx, sdCancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer sdCancel()
	sdErr := application.Shutdown(sdCtx)

	// Determine final exit code: a non-ErrServerClosed Run error or a non-nil
	// Shutdown error → exit 1; otherwise exit 0.
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		log.Printf("lic-service: run error: %v", runErr)
	}
	if sdErr != nil {
		log.Printf("lic-service: shutdown errors: %v", sdErr)
	}
	if (runErr != nil && !errors.Is(runErr, context.Canceled)) || sdErr != nil {
		os.Exit(1)
	}
	log.Printf("lic-service: clean shutdown")
}
