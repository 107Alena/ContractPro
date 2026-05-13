// Package main is the entrypoint of the Legal Intelligence Core (LIC) service.
//
// LIC-TASK-001 scaffolding: this stub starts up, logs a banner, and exits with
// code 0. Configuration loading (LIC-TASK-002), application wiring
// (LIC-TASK-047), broker/consumer/publisher subsystems and the 9-agent pipeline
// are added by subsequent tasks; until then the binary intentionally has no
// runtime behaviour beyond announcing itself.
package main

import (
	"log"
	"os"
)

// version is overridden via -ldflags "-X main.version=..." at build time
// (see Makefile docker-build target).
var version = "dev"

func main() {
	log.Printf("lic-service starting (version=%s) — scaffolding stub (LIC-TASK-001)", version)
	os.Exit(0)
}
