package checker

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/printer"
	"go/scanner"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"github.com/echozyr2001/go-static-checker/internal/config"
)

type groupedTestBody struct {
	testBody
	group string
}

func collectTestBodies(files []testFile) []groupedTestBody {
	var bodies []groupedTestBody
	var collect func(testBody, string)
	collect = func(body testBody, group string) {
		bodies = append(bodies, groupedTestBody{testBody: body, group: group})
		ast.Inspect(body.body, func(node ast.Node) bool {
			if _, ok := node.(*ast.FuncLit); ok {
				return false
			}
			call, ok := node.(*ast.CallExpr)
			if !ok || !isTestRun(body.file.pkg.TypesInfo, call) || len(call.Args) != 2 {
				return true
			}
			callback, ok := call.Args[1].(*ast.FuncLit)
			if !ok {
				return true
			}
			param, valid := testParam(callback.Type, body.file.pkg.TypesInfo)
			if !valid {
				return true
			}
			child := testBody{file: body.file, body: callback.Body, param: param, pos: call.Pos()}
			collect(child, fmt.Sprintf("%s:%d", body.file.name, body.pos))
			return true
		})
	}
	for _, file := range files {
		for _, decl := range file.node.Decls {
			d, ok := decl.(*ast.FuncDecl)
			if !ok || !testEntry(d, file.pkg.TypesInfo) {
				continue
			}
			param, _ := testParam(d.Type, file.pkg.TypesInfo)
			body := testBody{file: file, body: d.Body, param: param, pos: d.Pos()}
			collect(body, file.pkg.PkgPath)
		}
	}
	return bodies
}

func bodyFingerprint(body testBody) (string, error) {
	var rendered bytes.Buffer
	if err := printer.Fprint(&rendered, body.file.pkg.Fset, body.body); err != nil {
		return "", err
	}
	var scan scanner.Scanner
	fset := token.NewFileSet()
	file := fset.AddFile("", -1, rendered.Len())
	scan.Init(file, rendered.Bytes(), nil, 0)
	var key strings.Builder
	for {
		_, tok, lit := scan.Scan()
		if tok == token.EOF {
			break
		}
		fmt.Fprintf(&key, "%d:%s;", tok, strconv.Quote(lit))
	}
	info := body.file.pkg.TypesInfo
	param := info.Defs[body.param]
	ast.Inspect(body.body, func(node ast.Node) bool {
		id, ok := node.(*ast.Ident)
		if !ok {
			return true
		}
		obj := info.Uses[id]
		if obj == nil || obj == param || obj.Pos() >= body.body.Pos() && obj.Pos() < body.body.End() {
			return true
		}
		if pkg, ok := obj.(*types.PkgName); ok {
			fmt.Fprintf(&key, "import:%s;", pkg.Imported().Path())
			return true
		}
		fmt.Fprintf(&key, "object:%s;", types.ObjectString(obj, func(p *types.Package) string { return p.Path() }))
		if _, ok := obj.(*types.Var); ok {
			pos := body.file.pkg.Fset.PositionFor(obj.Pos(), false)
			fmt.Fprintf(&key, "binding:%s:%d;", pos.Filename, pos.Offset)
		}
		return true
	})
	return key.String(), nil
}

func duplicateTestBodies(r config.Rule, files []testFile) ([]Diagnostic, error) {
	seen := map[string]testBody{}
	var ds []Diagnostic
	for _, body := range collectTestBodies(files) {
		if len(body.body.List) == 0 {
			continue
		}
		key, err := bodyFingerprint(body.testBody)
		if err != nil {
			return nil, err
		}
		key = body.group + "\x00" + key
		if previous, ok := seen[key]; ok {
			pos := previous.file.pkg.Fset.PositionFor(previous.pos, false)
			message := fmt.Sprintf("test body matches %s:%d:%d; compare setup and intent before removing",
				previous.file.name, pos.Line, pos.Column)
			ds = append(ds, testDiagnostic(r, body.file, body.pos, "duplicate_test_body", message))
		} else {
			seen[key] = body.testBody
		}
	}
	return ds, nil
}
