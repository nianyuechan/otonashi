package sdk

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestStabilityIndexCoversExportedSymbols is the contract for Q3: every
// exported top-level symbol in this package must be classified, and the index
// must not claim symbols that no longer exist. Adding an exported type without
// declaring its level fails here, so the table cannot drift from the code.
func TestStabilityIndexCoversExportedSymbols(t *testing.T) {
	exported := exportedTopLevelNames(t)
	missing := make([]string, 0)
	for name := range exported {
		if _, ok := StabilityOf(name); !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("exported symbols without a declared stability level: %s", strings.Join(missing, ", "))
	}

	stale := make([]string, 0)
	for name := range stabilityIndex {
		if !exported[name] {
			stale = append(stale, name)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("stability index declares symbols that are not exported: %s", strings.Join(stale, ", "))
	}
}

func TestStabilityOfKnownAndUnknownNames(t *testing.T) {
	for name, want := range map[string]Stability{
		"Plugin":      StabilityStable,
		"KVStore":     StabilityStable,
		"Clock":       StabilityReserved,
		"StabilityOf": StabilityStable,
	} {
		got, ok := StabilityOf(name)
		if !ok || got != want {
			t.Errorf("StabilityOf(%q) = %v, %v; want %v, true", name, got, ok, want)
		}
	}
	if level, ok := StabilityOf("noSuchSymbol"); ok || level != StabilityStable {
		t.Errorf("StabilityOf(unknown) = %v, %v; want zero level and false", level, ok)
	}
}

func TestStabilityString(t *testing.T) {
	for level, want := range map[Stability]string{
		StabilityStable:       "stable",
		StabilityExperimental: "experimental",
		StabilityReserved:     "reserved",
		Stability(42):         "unknown",
	} {
		if got := level.String(); got != want {
			t.Errorf("Stability(%d).String() = %q; want %q", uint8(level), got, want)
		}
	}
}

// exportedTopLevelNames lists exported types, constants, variables, and
// functions declared in non-test files of this package. Struct fields and
// methods are not enumerated: they are governed by the stability of their
// owning type, which keeps the table readable.
func exportedTopLevelNames(t *testing.T) map[string]bool {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob package files: %v", err)
	}
	fset := token.NewFileSet()
	names := make(map[string]bool)
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		for _, decl := range parsed.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if s.Name.IsExported() {
							names[s.Name.Name] = true
						}
					case *ast.ValueSpec:
						for _, name := range s.Names {
							if name.IsExported() {
								names[name.Name] = true
							}
						}
					}
				}
			case *ast.FuncDecl:
				if d.Recv == nil && d.Name.IsExported() {
					names[d.Name.Name] = true
				}
			}
		}
	}
	return names
}
