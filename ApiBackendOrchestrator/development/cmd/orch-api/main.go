package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"contractpro/api-orchestrator/internal/app"
	"contractpro/api-orchestrator/internal/config"
)

// version is set at build time via -ldflags "-X main.version=...".
// It is available in /healthz responses and structured log output.
var version = "dev"

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	application, err := app.NewApp(cfg)
	if err != nil {
		log.Fatalf("init: %v", err)
	}

	// Listen for termination signals.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-sigCh
		log.Printf("received signal %s, shutting down gracefully (timeout %s)", sig, cfg.HTTP.ShutdownTimeout)

		ctx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
		defer cancel()

		if err := application.Shutdown(ctx); err != nil {
			log.Printf("shutdown error: %v", err)
			if ctx.Err() == context.DeadlineExceeded {
				log.Printf("shutdown timed out after %s, forcing exit", cfg.HTTP.ShutdownTimeout)
				os.Exit(1)
			}
		}

		// Second signal forces immediate exit.
		sig = <-sigCh
		log.Printf("received second signal %s, forcing exit", sig)
		os.Exit(1)
	}()

	// Start blocks until the server shuts down.
	if err := application.Start(); err != nil {
		// Start returns nil on graceful shutdown (http.ErrServerClosed
		// is handled internally by api.Server.Start).
		log.Fatalf("server: %v", err)
	}
}
