package checker

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"github.com/echozyr2001/go-static-checker/internal/config"
)

func tableLiteral(body testBody, loop *ast.RangeStmt) *ast.CompositeLit {
	if literal, ok := loop.X.(*ast.CompositeLit); ok {
		return literal
	}
	id, ok := loop.X.(*ast.Ident)
	if !ok {
		return nil
	}
	info := body.file.pkg.TypesInfo
	obj := info.Uses[id]
	if obj == nil {
		return nil
	}
	var literal *ast.CompositeLit
	uses := 0
	ast.Inspect(body.body, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.Ident:
			if info.Uses[n] == obj {
				uses++
			}
		case *ast.AssignStmt:
			if n.Tok == token.DEFINE && len(n.Lhs) == 1 && len(n.Rhs) == 1 {
				if name, ok := n.Lhs[0].(*ast.Ident); ok && info.Defs[name] == obj {
					literal, _ = n.Rhs[0].(*ast.CompositeLit)
				}
			}
		case *ast.ValueSpec:
			if len(n.Names) == 1 && len(n.Values) == 1 && info.Defs[n.Names[0]] == obj {
				literal, _ = n.Values[0].(*ast.CompositeLit)
			}
		}
		return true
	})
	if uses != 1 {
		return nil
	}
	return literal
}

func nodeParents(node ast.Node) map[ast.Node]ast.Node {
	parents := map[ast.Node]ast.Node{}
	var stack []ast.Node
	ast.Inspect(node, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		if len(stack) > 0 {
			parents[n] = stack[len(stack)-1]
		}
		stack = append(stack, n)
		return true
	})
	return parents
}

func tableNameField(body testBody, loop *ast.RangeStmt, row *types.Struct) (int, bool) {
	info := body.file.pkg.TypesInfo
	value, ok := loop.Value.(*ast.Ident)
	if !ok || loop.Tok != token.DEFINE {
		return -1, false
	}
	obj := info.Defs[value]
	var index types.Object
	if key, ok := loop.Key.(*ast.Ident); ok && key.Name != "_" {
		index = info.Defs[key]
	}
	parents := nodeParents(loop.Body)
	used := map[int]bool{}
	nameField := -1
	hasRun, safe, whole := false, true, false
	ast.Inspect(loop.Body, func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok && isTestRun(info, call) {
			hasRun = true
		}
		id, ok := node.(*ast.Ident)
		if !ok {
			return true
		}
		if index != nil && info.Uses[id] == index {
			safe = false
		}
		if info.Uses[id] != obj {
			return true
		}
		var expr ast.Expr = id
		selector, isSelector := parents[id].(*ast.SelectorExpr)
		field := -1
		if isSelector && selector.X == id {
			expr = selector
			for i := 0; i < row.NumFields(); i++ {
				if info.Uses[selector.Sel] == row.Field(i) {
					field = i
					break
				}
			}
		} else {
			whole = true
		}
		switch parent := parents[expr].(type) {
		case *ast.AssignStmt:
			for _, lhs := range parent.Lhs {
				if lhs == expr {
					safe = false
				}
			}
		case *ast.IncDecStmt:
			safe = false
		case *ast.UnaryExpr:
			if parent.Op == token.AND {
				safe = false
			}
		}
		if field >= 0 {
			call, ok := parents[selector].(*ast.CallExpr)
			if ok && isTestRun(info, call) && len(call.Args) == 2 && call.Args[0] == selector {
				nameField = field
			} else {
				used[field] = true
			}
		}
		return true
	})
	if whole || used[nameField] {
		nameField = -1
	}
	return nameField, hasRun && safe
}

func scalarCaseValue(expr ast.Expr, typ types.Type, info *types.Info) (string, bool) {
	if expr != nil {
		tv := info.Types[expr]
		if tv.IsNil() {
			return "nil", true
		}
		if tv.Value == nil {
			return "", false
		}
		prefix := ""
		if _, ok := typ.Underlying().(*types.Interface); ok {
			prefix = types.TypeString(types.Default(tv.Type), func(p *types.Package) string { return p.Path() }) + ":"
		}
		return prefix + tv.Value.ExactString(), true
	}
	switch underlying := typ.Underlying().(type) {
	case *types.Basic:
		switch {
		case underlying.Info()&types.IsBoolean != 0:
			return "false", true
		case underlying.Info()&types.IsString != 0:
			return strconv.Quote(""), true
		case underlying.Info()&types.IsNumeric != 0:
			return "0", true
		}
	case *types.Pointer, *types.Slice, *types.Map, *types.Chan, *types.Signature, *types.Interface:
		return "nil", true
	}
	return "", false
}

func rowFingerprint(literal *ast.CompositeLit, row *types.Struct, ignore int, info *types.Info) (string, bool) {
	values := make([]ast.Expr, row.NumFields())
	for i, element := range literal.Elts {
		index, value := i, element
		if keyed, ok := element.(*ast.KeyValueExpr); ok {
			name, ok := keyed.Key.(*ast.Ident)
			if !ok {
				return "", false
			}
			index = -1
			for j := 0; j < row.NumFields(); j++ {
				if row.Field(j).Name() == name.Name {
					index = j
					break
				}
			}
			value = keyed.Value
		}
		if index < 0 || index >= len(values) {
			return "", false
		}
		values[index] = value
	}
	var key strings.Builder
	for i, value := range values {
		text, ok := scalarCaseValue(value, row.Field(i).Type(), info)
		if !ok {
			return "", false
		}
		if i == ignore {
			continue
		}
		fmt.Fprintf(&key, "%s;", strconv.Quote(text))
	}
	return key.String(), true
}

func duplicateTableCases(r config.Rule, files []testFile) []Diagnostic {
	var ds []Diagnostic
	for _, body := range collectTestBodies(files) {
		ast.Inspect(body.body, func(node ast.Node) bool {
			if _, ok := node.(*ast.FuncLit); ok {
				return false
			}
			loop, ok := node.(*ast.RangeStmt)
			if !ok {
				return true
			}
			literal := tableLiteral(body.testBody, loop)
			if literal == nil {
				return true
			}
			info := body.file.pkg.TypesInfo
			var elem types.Type
			switch typ := info.TypeOf(literal).Underlying().(type) {
			case *types.Slice:
				elem = typ.Elem()
			case *types.Array:
				elem = typ.Elem()
			default:
				return true
			}
			row, ok := elem.Underlying().(*types.Struct)
			if !ok {
				return true
			}
			ignore, safe := tableNameField(body.testBody, loop, row)
			if !safe {
				return true
			}
			seen := map[string]token.Pos{}
			for _, element := range literal.Elts {
				entry, ok := element.(*ast.CompositeLit)
				if !ok {
					continue
				}
				key, known := rowFingerprint(entry, row, ignore, info)
				if !known {
					continue
				}
				if previous, ok := seen[key]; ok {
					pos := body.file.pkg.Fset.PositionFor(previous, false)
					message := fmt.Sprintf("static table row matches %s:%d:%d", body.file.name, pos.Line, pos.Column)
					if ignore >= 0 {
						message += "; ignored name-only field " + row.Field(ignore).Name()
					}
					ds = append(ds, testDiagnostic(r, body.file, entry.Pos(), "duplicate_test_case", message))
				} else {
					seen[key] = entry.Pos()
				}
			}
			return true
		})
	}
	return ds
}
