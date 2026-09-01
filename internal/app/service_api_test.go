package app

import (
	"bytes"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
	"unicode"
)

func TestPlainServiceSuiteKeepsLegacyCoverageUntagged(t *testing.T) {
	source, err := parser.ParseFile(token.NewFileSet(), "service_test.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("service_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("//go:build")) {
		t.Fatal("service_test.go has a build constraint; plain go test would skip legacy coverage")
	}
	want := map[string]bool{
		"TestAddCommentAtSameAnchorAppends":                     false,
		"TestUpdateCommentReplacesByIndex":                      false,
		"TestUpdateCommentRejectsAnchorMismatch":                false,
		"TestUpdateCommentRejectsInvalidIndex":                  false,
		"TestLegacyCommentMutationsPreserveOrderAndCardinality": false,
		"TestMergePRMergesMatchingSourceHeadAndCleansUp":        false,
		"TestMergePRRefusesMovedSourceHead":                     false,
		"TestMergePRRefusesDeletedSourceBranch":                 false,
		"TestMergePRRefusesConcurrentCloseBeforeBranchMerge":    false,
	}
	for _, declaration := range source.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok {
			if _, required := want[function.Name.Name]; required {
				want[function.Name.Name] = true
			}
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("plain service suite is missing %s", name)
		}
	}
}

func TestProductionServiceAPIExcludesMergeHooks(t *testing.T) {
	pkg, err := build.Default.ImportDir(".", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range pkg.GoFiles {
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch declaration := node.(type) {
			case *ast.FuncDecl:
				if declaration.Recv != nil && strings.Contains(declaration.Name.Name, "Hook") && declaration.Name.IsExported() {
					t.Errorf("production file %s exports hook method %s", name, declaration.Name.Name)
				}
			case *ast.Field:
				for _, fieldName := range declaration.Names {
					if strings.Contains(fieldName.Name, "Hook") && unicode.IsUpper(rune(fieldName.Name[0])) {
						t.Errorf("production file %s exports hook field %s", name, fieldName.Name)
					}
				}
			}
			return true
		})
	}
}
