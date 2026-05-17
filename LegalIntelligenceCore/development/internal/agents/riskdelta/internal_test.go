package riskdelta

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
// code-architect D7): stdlib + internal/domain/{model,port} + the sibling
// agent packages it composes (base, promptbuilder — for Content only; Agent 9
// mints no structural block) + the embedded-asset loaders (prompts, schemas).
//
// 6-entry, artifacts-FREE — Agent 9 is the PUREST non-DM-artifact agent of the
// 9 (§9 "Зависимости", ai-agents-pipeline.md:1510-1511: input is only the two
// RiskAnalysis structs + the two version ids; ZERO SEMANTIC_TREE /
// EXTRACTED_TEXT / PROCESSING_WARNINGS — even more than Agent 8, which still
// consumed SEMANTIC_TREE). internal/agents/artifacts is DELIBERATELY dropped:
// the shared DP-faithful artifact decoders are dead weight here. The
// artifacts-drop is the deliberate "non-artifact-consumer" class, NOT an
// omission: re-adding artifacts is a regression a reviewer must NOT make (the
// riskdetection "deliberate absence is a class, not an omission" house style;
// the detailedreport-D10 precedent). Also DELIBERATELY excludes internal/config
// (resolved values are constructor params, the config→value mapping is
// LIC-TASK-047's job — the router.RouterConfig hermeticity precedent),
// internal/infra/* and internal/llm/* (telemetry/router are seamed inside
// base), and the DocumentProcessing module (no contract tree is consumed).
var allowedInternal = map[string]struct{}{
	"contractpro/legal-intelligence-core/internal/agents/base":          {},
	"contractpro/legal-intelligence-core/internal/agents/promptbuilder": {},
	"contractpro/legal-intelligence-core/internal/agents/prompts":       {},
	"contractpro/legal-intelligence-core/internal/agents/schemas":       {},
	"contractpro/legal-intelligence-core/internal/domain/model":         {},
	"contractpro/legal-intelligence-core/internal/domain/port":          {},
}

// TestHermeticImports pins that non-test files import only stdlib + the
// 6-entry allowlist — no internal/config, no concrete infra/llm, no
// third-party, no DocumentProcessing module, and CRUCIALLY no
// internal/agents/artifacts (the deliberate non-artifact-consumer divergence;
// mirrors base / typeclassifier / keyparams / partyconsistency /
// mandatoryconditions / riskdetection / recommendation / summary /
// detailedreport TestHermeticImports).
func TestHermeticImports(t *testing.T) {
	const artifactsPkg = "contractpro/legal-intelligence-core/internal/agents/artifacts"
	if _, present := allowedInternal[artifactsPkg]; present {
		t.Fatalf("allowedInternal must NOT contain %q — Agent 9 consumes ZERO DM artifacts (§9 input is only the two RiskAnalysis structs + the two version ids); the artifacts-drop is the deliberate non-artifact-consumer divergence (D7)", artifactsPkg)
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
