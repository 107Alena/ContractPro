//go:build manual
// +build manual

// Run with: go test -tags=manual -v -run TestRealPDF ./internal/pdf/...
package pdf

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// testdataDir returns the absolute path to the data/ directory
// relative to this test file.
func testdataDir(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to determine test file location")
	}
	return filepath.Join(filepath.Dir(filename), "data")
}

func TestRealPDF(t *testing.T) {
	dir := testdataDir(t)

	files := []string{"first.pdf", "second.pdf"}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("failed to read %s: %v", path, err)
			}

			fi, _ := os.Stat(path)
			fmt.Printf("\n=== %s ===\n", name)
			fmt.Printf("Size: %d bytes\n", fi.Size())

			u := NewUtil()

			// IsValidPDF
			valid := u.IsValidPDF(bytes.NewReader(data))
			fmt.Printf("IsValidPDF: %v\n", valid)
			if !valid {
				t.Fatal("expected valid PDF")
			}

			// Analyze
			info, err := u.Analyze(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("Analyze error: %v", err)
			}
			fmt.Printf("PageCount: %d\n", info.PageCount)
			fmt.Printf("IsTextPDF: %v\n", info.IsTextPDF)

			// ExtractText
			pages, err := u.ExtractText(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("ExtractText error: %v", err)
			}
			fmt.Printf("Extracted %d pages:\n", len(pages))
			for _, p := range pages {
				preview := p.Text
				if len(preview) > 300 {
					preview = preview[:300] + "..."
				}
				fmt.Printf("\n--- Page %d (%d chars) ---\n%s\n", p.PageNumber, len(p.Text), preview)
			}
		})
	}
}
