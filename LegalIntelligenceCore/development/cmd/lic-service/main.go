// Package main is the entrypoint of the Legal Intelligence Core (LIC) service.
//
// LIC-TASK-001 introduced this as a scaffolding stub. LIC-TASK-003 wires in a
// minimal fail-fast guard: the binary now loads configuration on startup and
// exits non-zero if any required env var is missing or invalid. Full
// application wiring (broker, consumer, pipeline, publishers, HTTP handlers)
// is the deliverable of LIC-TASK-047 — intentionally out of scope here.
package main

import (
	"log"

	"contractpro/legal-intelligence-core/internal/config"
)

// version is overridden via -ldflags "-X main.version=..." at build time
// (see Makefile docker-build target and Dockerfile ARG VERSION).
var version = "dev"

func main() {
	log.Printf("lic-service starting (version=%s)", version)

	cfg, err := config.Load()
	if err != nil {
		// log.Fatalf prints to stderr and calls os.Exit(1) — exactly the
		// fail-fast behaviour required by LIC-TASK-003 acceptance test step 3
		// (docker run lic:test without env → non-zero exit).
		log.Fatalf("lic-service: configuration load failed: %v", err)
	}

	log.Printf("lic-service: config loaded (env=%s) — runtime wiring deferred to LIC-TASK-047", cfg.App.Env)
}
