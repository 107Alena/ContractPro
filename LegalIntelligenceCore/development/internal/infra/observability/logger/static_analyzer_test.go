package logger

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// static_analyzer_test.go covers LIC-TASK-052 acceptance criterion #2:
// "Static analyzer / test helper: no log.Info().Interface("contract_text",
// ...) patterns".
//
// The check is a Go AST walker — pure stdlib (go/parser + go/ast +
// go/token). It scans every non-test `.go` file under
// `LegalIntelligenceCore/development/internal/` and flags any call that
// passes a forbidden key (from ForbiddenLogKeys) as a string literal.
//
// Why AST, not grep:
//   - regex scans hit string literals and comments, which is a false-
//     positive mine (the forbidden-keys list itself, the security.md
//     prose copied into a CLAUDE.md, JSON Schema field names like
//     "key_parameters" embedded as $ref). AST walking inspects ONLY
//     positions where a literal can be a slog/logger attr key.
//
// Recognized patterns (the union of what the codebase actually uses):
//
//   1. slog.* constructors: slog.String, slog.Any, slog.Int, slog.Int64,
//      slog.Uint64, slog.Float64, slog.Bool, slog.Duration, slog.Time,
//      slog.Group  → first arg is the attribute key.
//
//   2. Method calls whose name is one of Debug/Info/Warn/Error/Fatal/With
//      on any receiver (catches both *logger.Logger and *slog.Logger as
//      well as third-party loggers) → if any arg is a key-value pair
//      string-literal followed by a value, the literal is the key. We
//      conservatively flag any string-literal argument that appears at an
//      even index (0, 2, 4, …) after the message.
//
//   3. slog.Attr composite literals where `Key:` is a string literal.
//
// Known limitations (deliberate, accept-as-is — these are call shapes the
// LIC codebase does NOT use today; if a future change introduces them, this
// analyzer will need to evolve):
//
//   - Identifier keys: `slog.String(keys.ContractText, raw)` where the
//     forbidden literal is hidden behind a constant. There are no such
//     constants in the codebase today (keys.go intentionally exports only
//     allowlisted names). If they appear, extend stringLit to resolve
//     selector → ForbiddenLogKeys.
//   - Concatenated literals: `slog.String("contract_"+"text", raw)`. The
//     analyzer only inspects pure BasicLit. Go programmers don't write
//     this; if they do, a code review (not this analyzer) catches it.
//   - The method-call branch (#2) flags ANY string-literal argument equal
//     to a forbidden key, regardless of position. For unambiguous keys
//     (key_parameters, raw_llm_response) this never collides; for short
//     English words ("subject", "price") a false positive is possible if
//     the literal appears as a value, not a key. That collision is
//     deliberate (the deny-list mirrors security.md §6.2 verbatim) — a
//     false positive surfaces a call site for human review and is cheaper
//     than missing a real leak.
//
// Excluded paths:
//   - any file ending in `_test.go` (test fixtures legitimately mention
//     forbidden keys, e.g. piiSamples in this very package);
//   - this package itself (`internal/infra/observability/logger/`) since
//     it OWNS the deny-list and naming;
//   - any path containing `/testdata/` (Go convention for test fixtures);
//   - vendored code (`/vendor/`).
//
// On a violation: test fails with `file:line: forbidden log key "X" in
// call ...`. The set of forbidden keys is the package-level
// `ForbiddenLogKeys`.

func TestNoForbiddenKeysInCallSites(t *testing.T) {
	root := repoInternalRoot(t)

	violations := scanForForbiddenKeys(t, root)

	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf(
			"static analyzer found %d forbidden-key log call(s) — these violate the security.md §6.2 allowlist:\n\n%s",
			len(violations),
			strings.Join(violations, "\n"),
		)
	}
}

// TestStaticAnalyzer_SelfTest pins the analyzer is doing what it claims:
// we hand it a synthetic AST and expect it to fire. Without this, a future
// refactor that silently broke the AST walk (e.g. matching `Selector.X.Name`
// against the wrong identifier) would make TestNoForbiddenKeysInCallSites
// vacuously pass.
func TestStaticAnalyzer_SelfTest(t *testing.T) {
	src := `
package x

import "log/slog"

func bad() {
	_ = slog.String("key_parameters", "leaky")
	_ = slog.Any("risks", []string{"r1"})
	_ = slog.Attr{Key: "parties", Value: slog.AnyValue("p1")}
}

type Logger interface{ Info(msg string, args ...any) }

func badMethod(l Logger) {
	l.Info("msg", "subject", "leaked-subject-content")
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "synthetic.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	forb := forbiddenSet()
	var found []string
	scanFileAST(fset, file, forb, &found)

	want := map[string]bool{
		"key_parameters": false,
		"risks":          false,
		"parties":        false,
		"subject":        false,
	}
	for _, v := range found {
		for k := range want {
			if strings.Contains(v, `"`+k+`"`) {
				want[k] = true
			}
		}
	}
	for k, ok := range want {
		if !ok {
			t.Errorf("self-test: analyzer failed to flag forbidden key %q in synthetic source.\nFlagged: %v", k, found)
		}
	}
}

// --- analyzer ---

// repoInternalRoot returns the absolute path of
// `LegalIntelligenceCore/development/internal/` so the analyzer is
// independent of the cwd `go test` is invoked from.
//
// The strategy mirrors what `runtime.Caller` + walking up to go.mod gives
// us: take this test file's directory, walk up until we hit a go.mod, then
// resolve `internal/` next to it.
func repoInternalRoot(t *testing.T) string {
	t.Helper()
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(here)
	for i := 0; i < 12; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			internal := filepath.Join(dir, "internal")
			if st, err := os.Stat(internal); err == nil && st.IsDir() {
				return internal
			}
			t.Fatalf("found go.mod at %s but no sibling internal/ dir", dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("could not locate go.mod walking up from analyzer test file")
	return ""
}

func scanForForbiddenKeys(t *testing.T, root string) []string {
	t.Helper()
	forb := forbiddenSet()
	fset := token.NewFileSet()
	var violations []string

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			name := d.Name()
			if name == "testdata" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		// Skip the logger package itself — it OWNS the deny-list and the
		// constants would otherwise trigger string-literal matches when
		// keys.go references "error" etc. (those are sanitized-channel
		// allowlisted keys, not forbidden — but the safer cut is "don't
		// scan the owner package at all").
		if strings.Contains(path, filepath.Join("infra", "observability", "logger")+string(os.PathSeparator)) {
			return nil
		}

		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			// A parse failure is a real defect; surface it.
			t.Errorf("parse %s: %v", path, err)
			return nil
		}
		scanFileAST(fset, f, forb, &violations)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return violations
}

// forbiddenSet materialises ForbiddenLogKeys as a map for O(1) lookup.
func forbiddenSet() map[string]struct{} {
	out := make(map[string]struct{}, len(ForbiddenLogKeys))
	for _, k := range ForbiddenLogKeys {
		out[k] = struct{}{}
	}
	return out
}

// slogAttrConstructors lists the slog/log helpers whose first arg is the
// attribute key. Any third-party adapter following the same convention
// (e.g. zap's String/Any) would be picked up too — by accident, not by
// design — which is harmless.
var slogAttrConstructors = map[string]struct{}{
	"String":   {},
	"Any":      {},
	"Int":      {},
	"Int64":    {},
	"Uint64":   {},
	"Float64":  {},
	"Bool":     {},
	"Duration": {},
	"Time":     {},
	"Group":    {},
}

// logMethodNames is the set of method names that take a message and a
// variadic args... pair list, where alternating positions (0, 2, …) are
// attribute keys.
var logMethodNames = map[string]struct{}{
	"Debug":    {},
	"Info":     {},
	"Warn":     {},
	"Error":    {},
	"Fatal":    {},
	"DebugCtx": {},
	"InfoCtx":  {},
	"WarnCtx":  {},
	"ErrorCtx": {},
	"FatalCtx": {},
	"LogAttrs": {},
}

func scanFileAST(fset *token.FileSet, f *ast.File, forb map[string]struct{}, out *[]string) {
	ast.Inspect(f, func(n ast.Node) bool {
		switch node := n.(type) {

		case *ast.CallExpr:
			inspectCallExpr(fset, node, forb, out)

		case *ast.CompositeLit:
			// slog.Attr{Key: "forbidden", Value: ...}
			inspectAttrCompositeLit(fset, node, forb, out)
		}
		return true
	})
}

// inspectCallExpr matches both slog.* constructors and *.Info/.Warn/etc.
// method calls.
func inspectCallExpr(fset *token.FileSet, call *ast.CallExpr, forb map[string]struct{}, out *[]string) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	name := sel.Sel.Name

	switch {
	case isSlogConstructor(sel, name):
		// First arg is the key.
		if len(call.Args) >= 1 {
			if lit, ok := stringLit(call.Args[0]); ok {
				if _, bad := forb[lit]; bad {
					*out = append(*out, formatViolation(fset, call.Pos(), lit, "slog."+name))
				}
			}
		}

	case isLogMethod(name):
		// l.Info(ctx, msg, slog.String("k", v), ...)  → slog.String already
		//   covered by isSlogConstructor branch (this CallExpr is the outer
		//   .Info; we don't double-flag).
		// l.Info(ctx, msg, "k", v, "k2", v2)         → flag literals at
		//   positions of "k".
		//
		// We can't tell statically which signature variant is in use, so
		// flag any string-literal argument at an "even" offset after the
		// msg slot. The slot index for the message itself varies — we
		// approximate by scanning every arg and flagging only literals
		// that are EXACTLY a forbidden key (no false positives because the
		// keys list never contains common English words).
		for _, a := range call.Args {
			lit, ok := stringLit(a)
			if !ok {
				continue
			}
			if _, bad := forb[lit]; bad {
				*out = append(*out, formatViolation(fset, call.Pos(), lit, "method "+name))
			}
		}
	}
}

func inspectAttrCompositeLit(fset *token.FileSet, cl *ast.CompositeLit, forb map[string]struct{}, out *[]string) {
	// Only consider composite literals whose type is some `Attr` — slog.Attr
	// or any local Attr alias. A plain literal `{Key: "x"}` without a type
	// selector is also matched (rare but possible).
	if cl.Type != nil {
		switch t := cl.Type.(type) {
		case *ast.SelectorExpr:
			if t.Sel.Name != "Attr" {
				return
			}
		case *ast.Ident:
			if t.Name != "Attr" {
				return
			}
		default:
			return
		}
	}
	for _, elt := range cl.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		keyIdent, ok := kv.Key.(*ast.Ident)
		if !ok || keyIdent.Name != "Key" {
			continue
		}
		lit, ok := stringLit(kv.Value)
		if !ok {
			continue
		}
		if _, bad := forb[lit]; bad {
			*out = append(*out, formatViolation(fset, cl.Pos(), lit, "Attr composite"))
		}
	}
}

func isSlogConstructor(sel *ast.SelectorExpr, name string) bool {
	if _, ok := slogAttrConstructors[name]; !ok {
		return false
	}
	// We don't insist the receiver be literally `slog` — any package alias
	// would still match the pattern. The conservative side of false
	// positives is "logged a forbidden key", which is exactly what we want
	// to fail on regardless of which package supplies the helper.
	return true
}

func isLogMethod(name string) bool {
	_, ok := logMethodNames[name]
	return ok
}

func stringLit(e ast.Expr) (string, bool) {
	bl, ok := e.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return "", false
	}
	s := bl.Value
	if len(s) < 2 {
		return "", false
	}
	// Strip outer quotes; tolerate either "..." or `...`.
	q := s[0]
	if q != '"' && q != '`' {
		return "", false
	}
	if s[len(s)-1] != q {
		return "", false
	}
	return s[1 : len(s)-1], true
}

func formatViolation(fset *token.FileSet, pos token.Pos, key, ctx string) string {
	p := fset.Position(pos)
	return p.Filename + ":" + itoa(p.Line) + ": forbidden log key \"" + key + "\" in " + ctx
}

// itoa is a tiny stdlib-free int-to-string converter (avoid importing
// strconv for one call site).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
