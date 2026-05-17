package recommendation

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
// code-architect D1): stdlib + internal/domain/{model,port} + the sibling
// agent packages it composes (base, promptbuilder — for Content only; Agent 6
// mints no structural block) + the embedded-asset loaders (prompts, schemas).
//
// DELIBERATE DIVERGENCE (code-architect MF-D1.2 — this allowlist is NOT
// "byte-identical to mandatoryconditions"): it is a STRICT 6-entry SUBSET of
// the Agent-4/5 allowlist with internal/agents/artifacts DELIBERATELY DROPPED.
// Agent 6 is the FIRST per-agent package that consumes NO EXTRACTED_TEXT — the
// §6 envelope (recommendation.txt:32-37) has no <contract_document> block and
// §6 "Зависимости" lists only SEMANTIC_TREE from DM — so the shared
// DP-faithful artifacts.ExtractedText decoder is dead weight here. The
// artifacts-drop is the deliberate "non-EXTRACTED_TEXT consumer" class, NOT an
// omission: re-adding artifacts to "match" Agents 4/5 is a regression a
// reviewer must NOT make (the riskdetection "deliberate absence is a class,
// not an omission" house style). Also DELIBERATELY excludes internal/config
// (resolved values are constructor params, the config→value mapping is
// LIC-TASK-047's job — the router.RouterConfig hermeticity precedent),
// internal/infra/* and internal/llm/* (telemetry/router are seamed inside
// base), and the DocumentProcessing module (SEMANTIC_TREE is a byte-faithful
// passthrough).
var allowedInternal = map[string]struct{}{
	"contractpro/legal-intelligence-core/internal/agents/base":          {},
	"contractpro/legal-intelligence-core/internal/agents/promptbuilder": {},
	"contractpro/legal-intelligence-core/internal/agents/prompts":       {},
	"contractpro/legal-intelligence-core/internal/agents/schemas":       {},
	"contractpro/legal-intelligence-core/internal/domain/model":         {},
	"contractpro/legal-intelligence-core/internal/domain/port":          {},
}

// TestHermeticImports pins that non-test files import only stdlib + the
// allowlist — no internal/config, no concrete infra/llm, no third-party, no
// DocumentProcessing module, and CRUCIALLY no internal/agents/artifacts (the
// deliberate Agent-6 divergence; mirrors base / typeclassifier / keyparams /
// partyconsistency / mandatoryconditions / riskdetection TestHermeticImports).
func TestHermeticImports(t *testing.T) {
	const artifactsPkg = "contractpro/legal-intelligence-core/internal/agents/artifacts"
	if _, present := allowedInternal[artifactsPkg]; present {
		t.Fatalf("allowedInternal must NOT contain %q — Agent 6 consumes no EXTRACTED_TEXT; the artifacts-drop is the deliberate non-consumer divergence (D1)", artifactsPkg)
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
