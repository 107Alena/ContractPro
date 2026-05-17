package summary

import (
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// allowedInternal is the EXACT set of first-party packages this per-agent
// package's non-test source may import. Hermeticity invariant (CLAUDE.md /
// code-architect): stdlib + internal/domain/{model,port} + the sibling agent
// packages it composes (base, promptbuilder — for Content only; Agent 7 mints
// no structural block) + the embedded-asset loaders (prompts, schemas) + the
// shared DP-faithful EXTRACTED_TEXT decoder (internal/agents/artifacts).
//
// 7-entry, artifacts-PRESENT — the Agent-4/5 EXTRACTED_TEXT-consumer CLASS.
// Agent 7 RE-ADDS internal/agents/artifacts: the deliberate Agent-6 DROP (its
// 6-entry artifacts-free set) is REVERSED here because §7 "Зависимости" lists
// EXTRACTED_TEXT and the §7 envelope has a <contract_document> block. This is
// NOT a regression toward Agents 4/5 — it is §7 putting Agent 7 back in the
// EXTRACTED_TEXT-consumer class (artifacts/CLAUDE.md "Consumers" names
// "agents 4,5 — and 7 with its own §7 compaction" as a reuse consumer). It is
// NOT "byte-identical to mandatoryconditions" (that phrase was a
// riskdetection-specific claim); it is the Agent-4/5 class with artifacts
// PRESENT. Also DELIBERATELY excludes internal/config (resolved values are
// constructor params, the config→value mapping is LIC-TASK-047's job — the
// router.RouterConfig hermeticity precedent), internal/infra/* and
// internal/llm/* (telemetry/router are seamed inside base), and the
// DocumentProcessing module (the artifacts package owns the DP-faithful
// EXTRACTED_TEXT decoder; no other artifact is structurally decoded here).
var allowedInternal = map[string]struct{}{
	"contractpro/legal-intelligence-core/internal/agents/artifacts":     {},
	"contractpro/legal-intelligence-core/internal/agents/base":          {},
	"contractpro/legal-intelligence-core/internal/agents/promptbuilder": {},
	"contractpro/legal-intelligence-core/internal/agents/prompts":       {},
	"contractpro/legal-intelligence-core/internal/agents/schemas":       {},
	"contractpro/legal-intelligence-core/internal/domain/model":         {},
	"contractpro/legal-intelligence-core/internal/domain/port":          {},
}

// TestHermeticImports pins that non-test files import only stdlib + the
// 7-entry allowlist — no internal/config, no concrete infra/llm, no
// third-party, no DocumentProcessing module, and CRUCIALLY artifacts MUST be
// PRESENT (the deliberate Agent-7 re-add vs. Agent 6's drop; mirrors base /
// typeclassifier / keyparams / partyconsistency / mandatoryconditions /
// riskdetection / recommendation TestHermeticImports).
func TestHermeticImports(t *testing.T) {
	const artifactsPkg = "contractpro/legal-intelligence-core/internal/agents/artifacts"
	if _, present := allowedInternal[artifactsPkg]; !present {
		t.Fatalf("allowedInternal MUST contain %q — Agent 7 consumes EXTRACTED_TEXT (§7 <contract_document>); the artifacts re-add is the deliberate Agent-7 vs. Agent-6 divergence", artifactsPkg)
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
				t.Errorf("%s imports third-party package %q (this package must stay third-party-free)", name, path)
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
// file in the package (same approach as base.TestGofmtClean).
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
