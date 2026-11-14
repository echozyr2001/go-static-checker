package checker

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"github.com/echozyr2001/go-static-checker/internal/config"
)

func testWaiting(r config.Rule, files []testFile) []Diagnostic {
	functions := map[*types.Func]testBody{}
	var roots []testBody
	for _, file := range files {
		for _, decl := range file.node.Decls {
			d, ok := decl.(*ast.FuncDecl)
			if !ok || d.Body == nil {
				continue
			}
			f, ok := file.pkg.TypesInfo.Defs[d.Name].(*types.Func)
			if !ok {
				continue
			}
			body := testBody{file: file, body: d.Body, pos: d.Pos()}
			functions[f] = body
			if testEntry(d, file.pkg.TypesInfo) {
				roots = append(roots, body)
			}
		}
	}
	type visit struct {
		body    *ast.BlockStmt
		virtual bool
	}
	visited := map[visit]bool{}
	reported := map[token.Pos]bool{}
	var ds []Diagnostic
	var walk func(testBody, bool)
	var callback func(ast.Expr, testBody, bool)
	callback = func(expr ast.Expr, body testBody, virtual bool) {
		if literal, ok := expr.(*ast.FuncLit); ok {
			walk(testBody{file: body.file, body: literal.Body, pos: literal.Pos()}, virtual)
		} else if target, ok := functions[calledFunction(body.file.pkg.TypesInfo, expr)]; ok {
			walk(target, virtual)
		}
	}
	walk = func(body testBody, virtual bool) {
		state := visit{body.body, virtual}
		if visited[state] {
			return
		}
		visited[state] = true
		info := body.file.pkg.TypesInfo
		ast.Inspect(body.body, func(node ast.Node) bool {
			if _, ok := node.(*ast.FuncLit); ok {
				return false
			}
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			fn := calledFunction(info, call.Fun)
			if isFunction(fn, "time", "Sleep") && !virtual && len(call.Args) == 1 {
				value := info.Types[call.Args[0]].Value
				nonpositive := value != nil && constant.Compare(value, token.LEQ, constant.MakeInt64(0))
				if !nonpositive && !reported[call.Pos()] {
					reported[call.Pos()] = true
					message := "time.Sleep may wait on real time; use synchronization or testing/synctest"
					ds = append(ds, testDiagnostic(r, body.file, call.Pos(), "real_time_wait", message))
				}
			}
			if isFunction(fn, "testing/synctest", "Test") && len(call.Args) == 2 {
				callback(call.Args[1], body, true)
				return true
			}
			if isTestRun(info, call) && len(call.Args) == 2 {
				callback(call.Args[1], body, virtual)
			}
			if literal, ok := call.Fun.(*ast.FuncLit); ok {
				callback(literal, body, virtual)
			}
			if target, ok := functions[fn]; ok {
				walk(target, virtual)
			}
			return true
		})
	}
	for _, root := range roots {
		walk(root, false)
	}
	return ds
}
