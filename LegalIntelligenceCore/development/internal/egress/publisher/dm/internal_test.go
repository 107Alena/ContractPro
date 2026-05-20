package dm

import (
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// allowedInternal is the EXACT set of first-party packages the DM Artifact
// Requester's non-test source may import (build-spec D13). Three entries —
// the domain model + ports (the cross-domain wire contracts) and the broker
// package (sentinel errors + the Publish signature seam contract; recorded
// as R2 — documented exception to the otherwise infra-free egress allowlist).
// PublishOutcome is a LOCAL MIRROR (seams.go) of metrics.PublishOutcome,
// pinned in seams_test.go against the metrics SSOT (the universal
// base.Outcome / router.CallOutcome / cost.Outcome / schemavalidator.
// RepairOutcome local-mirror precedent — keeps the production source
// hermetic; the metrics import lives in the _test file). There is NO
// internal/config import (local Config, 036/047-injected — build-spec D2),
// NO internal/infra/observability/metrics import in production (concrete
// prometheus is forbidden — Metrics is seamed away), NO internal/application/*
// (the requester is a stateless port implementation), NO third-party path.
// Reviewer gate: size == 3 EXACTLY.
var allowedInternal = map[string]struct{}{
	"contractpro/legal-intelligence-core/internal/domain/model": {},
	"contractpro/legal-intelligence-core/internal/domain/port":  {},
	"contractpro/legal-intelligence-core/internal/infra/broker": {},
}

// forbiddenInternal is the build-spec D13 active-fail set: packages whose
// presence in allowedInternal (a future "consistency" edit) would silently
// break the hermeticity contract. TestHermeticImports asserts they are NOT
// in the allowlist BEFORE scanning, so a regression fails loudly here
// rather than passing the import scan because someone widened the
// allowlist. Notably internal/infra/observability/metrics (the parent of
// labels): would pull a concrete prometheus dependency in. Notably
// internal/application/pipeline: the requester is a leaf port impl, the
// orchestrator imports IT, not vice versa.
var forbiddenInternal = []string{
	"contractpro/legal-intelligence-core/internal/config",
	"contractpro/legal-intelligence-core/internal/infra/observability/metrics",
	"contractpro/legal-intelligence-core/internal/infra/observability/tracer",
	"contractpro/legal-intelligence-core/internal/infra/observability/logger",
	"contractpro/legal-intelligence-core/internal/infra/kvstore",
	"contractpro/legal-intelligence-core/internal/infra/objectstorage",
	"contractpro/legal-intelligence-core/internal/application/pipeline",
	"contractpro/legal-intelligence-core/internal/application/pendingconfirmation",
	"contractpro/legal-intelligence-core/internal/application/aggregator",
	"contractpro/legal-intelligence-core/internal/application/dmawaiter",
	"contractpro/legal-intelligence-core/internal/application/pipeline/stages",
	"contractpro/legal-intelligence-core/internal/ingress/consumer",
	"contractpro/legal-intelligence-core/internal/ingress/idempotency",
	"contractpro/legal-intelligence-core/internal/ingress/router",
	"contractpro/legal-intelligence-core/internal/egress/dlq",
	"contractpro/legal-intelligence-core/internal/agents/base",
	"contractpro/legal-intelligence-core/internal/llm",
}

// TestHermeticImports pins that non-test files import only stdlib + the
// 3-entry allowlist (reviewer gate: size == 3 EXACTLY). It ACTIVELY fails
// if any forbidden internal (notably internal/infra/observability/metrics
// parent — would pull a prometheus dep — or internal/application/pipeline —
// would reverse the dep edge) ever lands in the allowlist, and flags any
// third-party import (notably github.com/prometheus/*, github.com/rabbitmq/*,
// go.opentelemetry.io/* — all behind seams or out of scope here).
func TestHermeticImports(t *testing.T) {
	if len(allowedInternal) != 3 {
		t.Fatalf("allowedInternal MUST have exactly 3 entries (build-spec D13 reviewer gate); got %d", len(allowedInternal))
	}
	for _, forbidden := range forbiddenInternal {
		if _, present := allowedInternal[forbidden]; present {
			t.Fatalf("allowedInternal must NOT contain %q — the DM Artifact Requester is hermetic (stdlib + model/port/broker(sentinels)/labels only); telemetry/clock/logger are seamed, broker.Publish is behind a seam, Config is a ctor param (build-spec D13)", forbidden)
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
				t.Errorf("%s imports third-party package %q (this package must stay third-party-free — prometheus/amqp091/otel included)", name, path)
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
// file in the package (the dmawaiter / pipeline / aggregator TestGofmtClean
// approach).
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
