package checker

import (
	"context"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/echozyr2001/go-static-checker/internal/config"
	"golang.org/x/tools/go/packages"
)

type testFile struct {
	pkg         *packages.Package
	node        *ast.File
	name, owner string
}

type testBody struct {
	file  testFile
	body  *ast.BlockStmt
	param *ast.Ident
	pos   token.Pos
}

func testPackageSelected(r config.Rule, relative string) bool {
	if r.Tests == nil {
		return false
	}
	for _, pattern := range r.Tests.Packages {
		if config.Match(pattern, relative) {
			return true
		}
	}
	return false
}

func loadTestAnalysis(ctx context.Context, c config.Config, env []string,
	own map[string]*packages.Package) ([]testFile, error) {
	var patterns []string
	for path, p := range own {
		relative := strings.TrimPrefix(strings.TrimPrefix(path, p.Module.Path), "/")
		if relative == "" {
			relative = "."
		}
		for _, r := range c.Rules {
			if testPackageSelected(r, relative) {
				patterns = append(patterns, path)
				break
			}
		}
	}
	if len(patterns) == 0 {
		return nil, nil
	}
	sort.Strings(patterns)
	pkgs, err := packages.Load(&packages.Config{Context: ctx, Dir: c.Project.Root, Env: env, Tests: true,
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax |
			packages.NeedModule | packages.NeedForTest,
		BuildFlags: []string{"-mod=readonly", "-tags=" + strings.Join(c.Project.BuildTags, ",")}}, patterns...)
	if err != nil {
		return nil, fmt.Errorf("load test analysis: %w", err)
	}
	var errors []string
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		for _, e := range p.Errors {
			errors = append(errors, e.Error())
		}
	})
	if len(errors) > 0 {
		sort.Strings(errors)
		return nil, fmt.Errorf("load test analysis: %s", strings.Join(errors, "\n"))
	}
	var files []testFile
	seen := map[string]bool{}
	for _, p := range pkgs {
		if own[p.ForTest] == nil || p.PkgPath != p.ForTest && p.PkgPath != p.ForTest+"_test" {
			continue
		}
		if p.TypesInfo == nil || p.Types == nil {
			return nil, fmt.Errorf("missing test type information for %s", p.ID)
		}
		for _, node := range p.Syntax {
			filename := p.Fset.PositionFor(node.Pos(), false).Filename
			if !strings.HasSuffix(filename, "_test.go") || seen[filename] {
				continue
			}
			name, err := filepath.Rel(c.Project.Root, filename)
			if err != nil {
				return nil, err
			}
			files = append(files, testFile{pkg: p, node: node, name: filepath.ToSlash(name), owner: p.ForTest})
			seen[filename] = true
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
	return files, nil
}

func testParam(ft *ast.FuncType, info *types.Info) (*ast.Ident, bool) {
	if ft.Params == nil || len(ft.Params.List) != 1 || ft.Results != nil && len(ft.Results.List) != 0 {
		return nil, false
	}
	field := ft.Params.List[0]
	if len(field.Names) > 1 {
		return nil, false
	}
	ptr, ok := types.Unalias(info.TypeOf(field.Type)).(*types.Pointer)
	if !ok {
		return nil, false
	}
	named, ok := types.Unalias(ptr.Elem()).(*types.Named)
	if !ok || named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != "testing" || named.Obj().Name() != "T" {
		return nil, false
	}
	if len(field.Names) == 0 {
		return nil, true
	}
	return field.Names[0], true
}

func testEntry(d *ast.FuncDecl, info *types.Info) bool {
	if d.Recv != nil || d.Body == nil || !strings.HasPrefix(d.Name.Name, "Test") {
		return false
	}
	suffix := strings.TrimPrefix(d.Name.Name, "Test")
	next, _ := utf8.DecodeRuneInString(suffix)
	_, valid := testParam(d.Type, info)
	return (suffix == "" || !unicode.IsLower(next)) && valid
}

func calledFunction(info *types.Info, expr ast.Expr) *types.Func {
	switch e := expr.(type) {
	case *ast.Ident:
		f, _ := info.Uses[e].(*types.Func)
		return f
	case *ast.SelectorExpr:
		f, _ := info.Uses[e.Sel].(*types.Func)
		return f
	case *ast.ParenExpr:
		return calledFunction(info, e.X)
	}
	return nil
}

func isFunction(f *types.Func, path, name string) bool {
	return f != nil && f.Pkg() != nil && f.Pkg().Path() == path && f.Name() == name
}

func isTestRun(info *types.Info, call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	selection := info.Selections[selector]
	if selection == nil || selection.Kind() != types.MethodVal {
		return false
	}
	f := calledFunction(info, call.Fun)
	if !isFunction(f, "testing", "Run") {
		return false
	}
	sig := f.Type().(*types.Signature)
	ptr, ok := sig.Recv().Type().(*types.Pointer)
	if !ok {
		return false
	}
	named, ok := ptr.Elem().(*types.Named)
	return ok && named.Obj().Name() == "T"
}

func testDiagnostic(r config.Rule, file testFile, pos token.Pos, code, message string) Diagnostic {
	location := file.pkg.Fset.PositionFor(pos, false)
	return Diagnostic{RuleID: r.ID, Code: code, Severity: r.Severity, Message: message,
		File: file.name, Line: location.Line, Column: location.Column, SourcePackage: file.pkg.PkgPath}
}

func checkTestQuality(r config.Rule, files []testFile) ([]Diagnostic, error) {
	var selected []testFile
	for _, file := range files {
		relative := strings.TrimPrefix(strings.TrimPrefix(file.owner, file.pkg.Module.Path), "/")
		if relative == "" {
			relative = "."
		}
		if testPackageSelected(r, relative) && (*r.Tests.IncludeGenerated || !ast.IsGenerated(file.node)) {
			selected = append(selected, file)
		}
	}
	if r.Kind == "test_waiting" {
		return testWaiting(r, selected), nil
	}
	var ds []Diagnostic
	if *r.Tests.DuplicateBodies {
		found, err := duplicateTestBodies(r, selected)
		if err != nil {
			return nil, err
		}
		ds = append(ds, found...)
	}
	if *r.Tests.DuplicateCases {
		ds = append(ds, duplicateTableCases(r, selected)...)
	}
	return ds, nil
}
