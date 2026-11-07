package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestEnqueueSwitchHasNoCapabilityID(t *testing.T) {
	src, err := os.ReadFile("handlers.go")
	if err != nil {
		t.Fatal("handlers")
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "handlers.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal("parse")
	}
	var fn *ast.FuncDecl
	for _, decl := range file.Decls {
		candidate, ok := decl.(*ast.FuncDecl)
		if ok && candidate.Name.Name == "enqueueForEngine" {
			fn = candidate
			break
		}
	}
	if fn == nil {
		t.Fatal("enqueueForEngine")
	}
	start := fset.Position(fn.Pos()).Offset
	end := fset.Position(fn.End()).Offset
	body := string(src[start:end])
	if strings.Contains(body, "octoconv.convert.imagepdf") {
		t.Fatal("capability id")
	}
	ast.Inspect(fn, func(n ast.Node) bool {
		clause, ok := n.(*ast.CaseClause)
		if !ok {
			return true
		}
		for _, expr := range clause.List {
			switch label := expr.(type) {
			case *ast.Ident:
				if label.Name == "external" {
					t.Fatal("external")
				}
			case *ast.BasicLit:
				if strings.Contains(label.Value, "external") {
					t.Fatal("external")
				}
			}
		}
		return true
	})
}
