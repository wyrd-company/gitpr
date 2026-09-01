package app

import (
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"strings"
	"testing"
	"unicode"
)

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
