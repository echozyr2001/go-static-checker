package analysis

import (
	"context"
	"fmt"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

// LoadGo builds a production-only call model for the selected Go environment.
func LoadGo(ctx context.Context, root string, env, tags []string) (Program, error) {
	pkgs, err := packages.Load(&packages.Config{Context: ctx, Dir: root, Env: env,
		Mode:       packages.LoadSyntax | packages.NeedModule,
		BuildFlags: []string{"-mod=readonly", "-tags=" + strings.Join(tags, ",")}}, "./...")
	if err != nil {
		return Program{}, fmt.Errorf("load call model: %w", err)
	}
	var problems []string
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		for _, e := range p.Errors {
			problems = append(problems, e.Error())
		}
	})
	if len(problems) > 0 {
		sort.Strings(problems)
		return Program{}, fmt.Errorf("load call model: %s", strings.Join(problems, "\n"))
	}
	for _, p := range pkgs {
		if p.Module == nil || filepath.Clean(p.Module.GoMod) != filepath.Join(root, "go.mod") {
			return Program{}, fmt.Errorf("unexpected module in call model: %s", p.PkgPath)
		}
	}
	prog, initial := ssautil.Packages(pkgs, ssa.InstantiateGenerics)
	prog.Build()
	own := map[*types.Package]bool{}
	for _, pkg := range initial {
		if pkg != nil {
			own[pkg.Pkg] = true
		}
	}
	functions := ssautil.AllFunctions(prog)
	model := Program{Functions: map[string]Function{}}
	var add func(*ssa.Function)
	add = func(fn *ssa.Function) {
		id := fn.String()
		if _, ok := model.Functions[id]; ok {
			return
		}
		obj := fn.Object()
		entry := obj != nil && obj.Pkg() != nil && own[obj.Pkg()] &&
			fn.Parent() == nil && fn.Origin() == nil && fn.Synthetic == ""
		function := Function{ID: id, Symbol: functionSymbol(fn),
			Location: sourceLocation(root, prog.Fset, fn.Pos()), Entry: entry}
		model.Functions[id] = function
		if len(fn.Blocks) == 0 {
			function.Calls = append(function.Calls, Call{Location: function.Location,
				Unknown: "function body is unavailable"})
		}
		for _, block := range fn.Blocks {
			for _, instruction := range block.Instrs {
				if panicCall, ok := instruction.(*ssa.Panic); ok {
					function.Calls = append(function.Calls, Call{Symbol: "builtin::panic", Primitive: true,
						Location: sourceLocation(root, prog.Fset, panicCall.Pos())})
					continue
				}
				invoke, ok := instruction.(ssa.CallInstruction)
				if !ok {
					continue
				}
				common := invoke.Common()
				call := Call{Location: sourceLocation(root, prog.Fset, invoke.Pos())}
				if callee := common.StaticCallee(); callee != nil {
					call.Target, call.Symbol = callee.String(), functionSymbol(callee)
					calleeObject := callee.Object()
					if len(callee.Blocks) > 0 || (calleeObject != nil && own[calleeObject.Pkg()]) {
						add(callee)
					}
				} else if primitive, ok := common.Value.(*ssa.Builtin); ok {
					call.Symbol = "builtin::" + primitive.Name()
					call.Primitive = true
				} else {
					call.Unknown = "dynamic function target cannot be resolved"
					if common.IsInvoke() {
						call.Symbol = objectSymbol(common.Method)
						call.Unknown = "interface dispatch cannot be resolved"
					}
				}
				function.Calls = append(function.Calls, call)
			}
		}
		sort.SliceStable(function.Calls, func(i, j int) bool {
			a, b := function.Calls[i], function.Calls[j]
			if a.Location.File != b.Location.File {
				return a.Location.File < b.Location.File
			}
			if a.Location.Line != b.Location.Line {
				return a.Location.Line < b.Location.Line
			}
			if a.Location.Column != b.Location.Column {
				return a.Location.Column < b.Location.Column
			}
			return a.Symbol < b.Symbol
		})
		model.Functions[id] = function
	}
	var roots []*ssa.Function
	for fn := range functions {
		obj := fn.Object()
		if obj != nil && obj.Pkg() != nil && own[obj.Pkg()] && fn.Parent() == nil {
			roots = append(roots, fn)
		}
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].String() < roots[j].String() })
	for _, fn := range roots {
		add(fn)
	}
	return model, nil
}

func objectSymbol(obj *types.Func) string {
	if obj == nil || obj.Pkg() == nil {
		return ""
	}
	name := obj.Name()
	signature := obj.Type().(*types.Signature)
	if receiver := signature.Recv(); receiver != nil {
		typ := types.Unalias(receiver.Type())
		if pointer, ok := typ.(*types.Pointer); ok {
			typ = types.Unalias(pointer.Elem())
		}
		if named, ok := typ.(*types.Named); ok {
			name = named.Obj().Name() + "." + name
		}
	}
	return obj.Pkg().Path() + "::" + name
}

func functionSymbol(fn *ssa.Function) string {
	if origin := fn.Origin(); origin != nil {
		fn = origin
	}
	if obj, ok := fn.Object().(*types.Func); ok {
		return objectSymbol(obj)
	}
	return fn.String()
}

func sourceLocation(root string, fset *token.FileSet, pos token.Pos) Location {
	if !pos.IsValid() {
		return Location{}
	}
	location := fset.PositionFor(pos, false)
	filename := location.Filename
	if relative, err := filepath.Rel(root, filename); err == nil {
		filename = relative
	}
	return Location{File: filepath.ToSlash(filename), Line: location.Line, Column: location.Column}
}
