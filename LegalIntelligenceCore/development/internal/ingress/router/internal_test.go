package router

import (
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// allowedInternal is the EXACT 3-entry set of first-party packages the
// Router's non-test source may import (build-spec D10). It is hermetic
// against internal/infra/* (broker / kvstore / observability concretes),
// internal/application/pendingconfirmation (inverted via the local
// PendingConfirmationManager seam) and internal/ingress/{consumer,
// idempotency} (inverted via local seams). Frozen ports + DTOs come from
// domain/{model,port}; the SINGLE identity-comparable sentinel
// pipeline.ErrPipelinePaused (+ pipeline.IsPaused) comes from
// internal/application/pipeline — the same pattern the orchestrator's
// Config.PausedSentinel uses to communicate paused-ness across the
// pendingconfirmation boundary without a circular import.
var allowedInternal = map[string]struct{}{
	"contractpro/legal-intelligence-core/internal/domain/model":         {},
	"contractpro/legal-intelligence-core/internal/domain/port":          {},
	"contractpro/legal-intelligence-core/internal/application/pipeline": {},
}

// forbiddenInternal is the build-spec D10 active-fail set: packages whose
// presence in allowedInternal (a future "consistency" edit) would silently
// break the hermeticity contract. TestHermeticImports asserts they are NOT
// in the allowlist BEFORE scanning, so a regression fails loudly here rather
// than passing the import scan because someone widened the allowlist.
//
// Notably:
//   - internal/application/pendingconfirmation — inverted via the
//     PendingConfirmationManager seam.
//   - internal/ingress/consumer — inverted via the consumer.EventRouter
//     seam at the consumer side; the wiring is 047's job.
//   - internal/ingress/idempotency — inverted via the IdempotencyGuard
//     seam; the wiring is 047's job (the var _ assertion lives in 047).
//   - internal/infra/* — broker, kvstore, observability concretes
//     forbidden (the Router does not bind to amqp/redis/prometheus/otel).
//   - internal/config — Config is a ctor param (D3/D10).
//   - internal/egress/* — DLQ publishing is NOT a Router concern (R4).
//   - internal/application/{aggregator,stages} — out of scope.
//   - internal/agents/* + internal/agents/llm — out of scope.
//   - internal/app / cmd — wiring layers, depend on us not vice-versa.
var forbiddenInternal = []string{
	"contractpro/legal-intelligence-core/internal/application/pendingconfirmation",
	"contractpro/legal-intelligence-core/internal/application/aggregator",
	"contractpro/legal-intelligence-core/internal/application/stages",
	"contractpro/legal-intelligence-core/internal/ingress/consumer",
	"contractpro/legal-intelligence-core/internal/ingress/idempotency",
	"contractpro/legal-intelligence-core/internal/infra/broker",
	"contractpro/legal-intelligence-core/internal/infra/kvstore",
	"contractpro/legal-intelligence-core/internal/infra/observability",
	"contractpro/legal-intelligence-core/internal/infra/observability/logger",
	"contractpro/legal-intelligence-core/internal/infra/observability/metrics",
	"contractpro/legal-intelligence-core/internal/infra/observability/tracer",
	"contractpro/legal-intelligence-core/internal/config",
	"contractpro/legal-intelligence-core/internal/egress",
	"contractpro/legal-intelligence-core/internal/egress/dlq",
	"contractpro/legal-intelligence-core/internal/egress/publisher",
	"contractpro/legal-intelligence-core/internal/agents/base",
	"contractpro/legal-intelligence-core/internal/agents/llm",
	"contractpro/legal-intelligence-core/internal/app",
	"contractpro/legal-intelligence-core/cmd/lic-worker",
}

// forbiddenThirdParty is the build-spec D10 active-fail third-party set: the
// Router permits ZERO third-party (the idempotency / pendingconfirmation
// hermetic precedent). The generic "contains a dot ⇒ third-party" rule
// below already rejects any third-party; this list pins the specific notable
// offenders so a reviewer sees the intent.
var forbiddenThirdParty = []string{
	"github.com/rabbitmq/amqp091-go",
	"github.com/redis/go-redis/v9",
	"github.com/redis/go-redis",
	"github.com/prometheus/client_golang",
	"go.opentelemetry.io/otel",
	"go.opentelemetry.io/otel/trace",
	"golang.org/x/sync/errgroup",
	"golang.org/x/sync/semaphore",
}

// TestHermeticImports pins that non-test files import only stdlib + the
// 3-entry {domain/model, domain/port, application/pipeline} allowlist with
// ZERO permitted third-party. It ACTIVELY fails if any forbidden internal
// (notably internal/application/pendingconfirmation, internal/ingress/
// consumer, internal/ingress/idempotency, internal/infra/*, internal/config,
// internal/egress/*) ever lands in the allowlist, and flags any third-party
// import.
func TestHermeticImports(t *testing.T) {
	for _, forbidden := range forbiddenInternal {
		if _, present := allowedInternal[forbidden]; present {
			t.Fatalf("allowedInternal must NOT contain %q — the Event Router is hermetic (stdlib + domain/{model,port} + application/pipeline for ErrPipelinePaused/IsPaused only); broker/kvstore/observability/pendingconfirmation/consumer/idempotency/config are all seamed (build-spec D10)", forbidden)
		}
	}
	for _, forbidden := range forbiddenThirdParty {
		if _, present := allowedInternal[forbidden]; present {
			t.Fatalf("allowedInternal must NOT contain third-party %q — the Event Router permits ZERO third-party imports (build-spec D10)", forbidden)
		}
	}

	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read pkg dir: %v", err)
	}
	checked := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		checked++
		f, perr := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if perr != nil {
			t.Fatalf("parse %s: %v", name, perr)
		}
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if strings.HasPrefix(path, "contractpro/") {
				if _, ok := allowedInternal[path]; !ok {
					t.Errorf("%s imports forbidden internal package %q (hermeticity breach)", name, path)
				}
				continue
			}
			if strings.Contains(path, ".") {
				t.Errorf("%s imports third-party package %q (this package must stay third-party-free — amqp091/go-redis/prometheus/otel/golang.org-x-sync included)", name, path)
			}
			// else: stdlib — allowed.
		}
	}
	if checked == 0 {
		t.Fatal("no non-test .go files found to check")
	}
}

// TestGofmtClean is a self-check: the sandbox blocks gofmt/`go fmt`, so
// canonical formatting is asserted in-process via go/format over every .go
// file in the package (the consumer/idempotency/pendingconfirmation
// TestGofmtClean approach — build-spec D14).
func TestGofmtClean(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read pkg dir: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		src, rerr := os.ReadFile(name)
		if rerr != nil {
			t.Fatalf("read %s: %v", name, rerr)
		}
		want, ferr := format.Source(src)
		if ferr != nil {
			t.Fatalf("format %s: %v", name, ferr)
		}
		if string(want) != string(src) {
			t.Errorf("%s is not gofmt-clean", name)
		}
	}
}
