package consumer

import (
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// allowedInternal is the EXACT set of first-party packages the Event
// Consumer's non-test source may import (build-spec D16). It is an inbound
// ADAPTER (not an internal/application/* hermetic core) so it MAY import
// internal/infra/broker — but ONLY for broker.Delivery / broker.MessageHandler
// (the broker already inverted the amqp091 dependency, subscribe.go:31-35).
// The router is the local EventRouter seam (D8), the DLQ is a domain.port, the
// logger/metrics/tracer are seams (D6/D17/R4) and dlqHashKey is a ctor param
// (no internal/config — D2/D16).
var allowedInternal = map[string]struct{}{
	"contractpro/legal-intelligence-core/internal/domain/model": {},
	"contractpro/legal-intelligence-core/internal/domain/port":  {},
	"contractpro/legal-intelligence-core/internal/infra/broker": {},
}

// allowedThirdParty is the single permitted third-party import (build-spec
// D16/D10 — github.com/google/uuid is used directly by isCanonicalUUID). It
// is explicitly allowlisted so it is NOT flagged by the generic
// "contains a dot ⇒ third-party" rule below.
var allowedThirdParty = map[string]struct{}{
	"github.com/google/uuid": {},
}

// forbiddenInternal is the build-spec D16 active-fail set: packages whose
// presence in allowedInternal (a future "consistency" edit) would silently
// break the hermeticity contract. TestHermeticImports asserts they are NOT in
// the allowlist BEFORE scanning, so a regression fails loudly here rather than
// passing the import scan because someone widened the allowlist. Notably
// internal/ingress/router (the consumer's own downstream — the dependency is
// INVERTED via the EventRouter seam, D8) and internal/config (dlqHashKey is a
// ctor param — D2).
var forbiddenInternal = []string{
	"contractpro/legal-intelligence-core/internal/config",
	"contractpro/legal-intelligence-core/internal/infra/observability/logger",
	"contractpro/legal-intelligence-core/internal/infra/observability/metrics",
	"contractpro/legal-intelligence-core/internal/infra/observability/tracer",
	"contractpro/legal-intelligence-core/internal/infra/kvstore",
	"contractpro/legal-intelligence-core/internal/ingress/router",
	"contractpro/legal-intelligence-core/internal/application/pipeline",
	"contractpro/legal-intelligence-core/internal/application/pendingconfirmation",
	"contractpro/legal-intelligence-core/internal/application/aggregator",
	"contractpro/legal-intelligence-core/internal/agents/base",
}

// TestHermeticImports pins that non-test files import only stdlib + the
// 3-entry {model,port,broker} allowlist + the single
// github.com/google/uuid third-party. It ACTIVELY fails if any forbidden
// internal (notably internal/ingress/router, the concrete logger/metrics, or
// internal/config) ever lands in the allowlist, and flags any third-party
// import other than github.com/google/uuid (notably
// github.com/rabbitmq/amqp091-go — the broker shields it; subscribe.go:31-35).
func TestHermeticImports(t *testing.T) {
	for _, forbidden := range forbiddenInternal {
		if _, present := allowedInternal[forbidden]; present {
			t.Fatalf("allowedInternal must NOT contain %q — the Event Consumer is hermetic (stdlib + google/uuid + domain/{model,port} + infra/broker types only); the router is the local EventRouter seam, the DLQ is a domain.port, logger/metrics/tracer are seamed and config is a ctor param (build-spec D16)", forbidden)
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
			if _, ok := allowedThirdParty[path]; ok {
				continue // the single permitted third-party (build-spec D16)
			}
			if strings.Contains(path, ".") {
				t.Errorf("%s imports third-party package %q (only github.com/google/uuid is permitted — no amqp091/prometheus/otel/redis)", name, path)
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
// file in the package (the pendingconfirmation TestGofmtClean approach —
// build-spec D20).
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
