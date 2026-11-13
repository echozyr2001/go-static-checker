// Package checker evaluates project rules under the active Go build environment.
package checker

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/echozyr2001/go-static-checker/internal/config"
	"go/ast"
	"go/parser"
	"go/token"
	"golang.org/x/tools/go/packages"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type Diagnostic struct {
	RuleID        string `json:"rule_id"`
	Code          string `json:"code"`
	Severity      string `json:"severity"`
	Message       string `json:"message"`
	File          string `json:"file"`
	Line          int    `json:"line"`
	Column        int    `json:"column"`
	SourcePackage string `json:"source_package"`
	TargetPackage string `json:"target_package"`
	SourceLayer   string `json:"source_layer"`
	TargetLayer   string `json:"target_layer"`
}

type Report struct {
	Environment map[string]string `json:"environment"`
	BuildTags   []string          `json:"build_tags"`
	Packages    int               `json:"packages"`
	Files       int               `json:"files"`
	Diagnostics []Diagnostic      `json:"diagnostics"`
}

type source struct {
	pkg             *packages.Package
	file            string
	content         []byte
	packageName     string
	packagePosition token.Position
	node            *ast.File
	fset            *token.FileSet
	isTest          bool
	imports         []struct {
		target string
		pos    token.Position
	}
}

func (r Report) ExitCode() int {
	for _, d := range r.Diagnostics {
		if d.Severity == "error" {
			return 1
		}
	}
	return 0
}

func Run(ctx context.Context, c config.Config) (Report, error) {
	report := Report{Diagnostics: []Diagnostic{}, BuildTags: c.Project.BuildTags}
	if err := c.Validate(); err != nil {
		return report, err
	}
	root := c.Project.Root
	modfile := filepath.Join(root, "go.mod")
	if _, err := os.Stat(modfile); err != nil {
		return report, err
	}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "vendor") {
			return filepath.SkipDir
		}
		if !d.IsDir() && d.Name() == "go.mod" && p != modfile {
			return fmt.Errorf("nested modules are unsupported: %s", p)
		}
		return nil
	})
	if err != nil {
		return report, err
	}
	env := append(os.Environ(), "GOWORK=off", "GO111MODULE=on")
	cmd := exec.CommandContext(ctx, "go", "env", "-json", "GOOS", "GOARCH", "CGO_ENABLED", "GOVERSION")
	cmd.Dir, cmd.Env = root, env
	out, err := cmd.Output()
	if err != nil {
		return report, fmt.Errorf("read Go environment: %w", err)
	}
	if err := json.Unmarshal(out, &report.Environment); err != nil {
		return report, err
	}
	pkgs, err := packages.Load(&packages.Config{
		Context: ctx, Dir: root, Env: env,
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedImports |
			packages.NeedDeps | packages.NeedModule,
		BuildFlags: []string{"-mod=readonly", "-tags=" + strings.Join(c.Project.BuildTags, ",")},
	}, "./...")
	if err != nil {
		return report, err
	}
	var loadErrors []string
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		for _, e := range p.Errors {
			loadErrors = append(loadErrors, e.Error())
		}
	})
	if len(loadErrors) > 0 {
		sort.Strings(loadErrors)
		return report, fmt.Errorf("load project: %s", strings.Join(loadErrors, "\n"))
	}
	if len(pkgs) == 0 {
		return report, fmt.Errorf("no Go packages found")
	}
	own := map[string]*packages.Package{}
	for _, p := range pkgs {
		if p.Module == nil || filepath.Clean(p.Module.GoMod) != filepath.Clean(modfile) {
			return report, fmt.Errorf("unexpected module identity for %s", p.PkgPath)
		}
		own[p.PkgPath] = p
	}
	report.Packages = len(own)
	var sources []source
	for _, p := range pkgs {
		for _, filename := range p.GoFiles {
			fset := token.NewFileSet()
			content, err := os.ReadFile(filename)
			if err != nil {
				return report, err
			}
			f, err := parser.ParseFile(fset, filename, content, parser.ParseComments|parser.AllErrors)
			if err != nil {
				return report, err
			}
			rel, err := filepath.Rel(root, filename)
			if err != nil {
				return report, err
			}
			s := source{pkg: p, file: filepath.ToSlash(rel), content: content, node: f, fset: fset,
				packageName: f.Name.Name, packagePosition: fset.PositionFor(f.Name.Pos(), false)}
			for _, imp := range f.Imports {
				target, err := strconv.Unquote(imp.Path.Value)
				if err != nil {
					return report, err
				}
				s.imports = append(s.imports, struct {
					target string
					pos    token.Position
				}{target, fset.Position(imp.Path.Pos())})
			}
			sources = append(sources, s)
			report.Files++
		}
	}
	testSources, err := loadFormatTestSources(ctx, c, env, own)
	if err != nil {
		return report, err
	}
	sources = append(sources, testSources...)
	report.Files += len(testSources)
	for _, r := range c.Rules {
		switch r.Kind {
		case "source_format":
			diagnostics, err := checkSourceFormat(r, sources)
			if err != nil {
				return report, err
			}
			report.Diagnostics = append(report.Diagnostics, diagnostics...)
			continue
		case "file_layout":
			diagnostics, err := checkFileLayout(root, r)
			if err != nil {
				return report, err
			}
			report.Diagnostics = append(report.Diagnostics, diagnostics...)
			continue
		}
		layers := map[string]*config.Layer{}
		for _, p := range pkgs {
			rel := strings.TrimPrefix(p.PkgPath, p.Module.Path)
			rel = strings.TrimPrefix(rel, "/")
			if rel == "" {
				rel = "."
			}
			for i := range r.Layers {
				l := &r.Layers[i]
				matched := false
				for _, pat := range l.Packages {
					matched = matched || config.Match(pat, rel)
				}
				if matched {
					if layers[p.PkgPath] != nil {
						return report, fmt.Errorf("rule %s: package %s matches multiple layers", r.ID, p.PkgPath)
					}
					layers[p.PkgPath] = l
				}
			}
			if layers[p.PkgPath] == nil {
				report.Diagnostics = append(report.Diagnostics, Diagnostic{
					RuleID: r.ID, Code: "unclassified_package", Severity: r.Severity,
					Message: "package does not belong to a layer", SourcePackage: p.PkgPath,
				})
			}
		}
		for _, s := range sources {
			if s.isTest {
				continue
			}
			from := layers[s.pkg.PkgPath]
			if from == nil {
				continue
			}
			for _, imp := range s.imports {
				if imp.target == "C" {
					continue
				}
				target := s.pkg.Imports[imp.target]
				if target == nil {
					return report, fmt.Errorf("missing import metadata: %s -> %s", s.pkg.PkgPath, imp.target)
				}
				code, targetLayer := "", ""
				if own[target.PkgPath] != nil {
					to := layers[target.PkgPath]
					if to == nil {
						continue
					}
					targetLayer = to.Name
					allowed := false
					if from.Name == to.Name {
						allowed = *r.AllowSameLayer
					} else {
						for _, name := range from.Allow {
							allowed = allowed || name == to.Name
						}
					}
					if !allowed {
						code = "forbidden_layer_dependency"
					}
				} else if target.Module == nil {
					if !*r.AllowStdlib {
						code = "forbidden_stdlib_dependency"
					}
				} else {
					allowed := *r.ExternalDefault == "allow"
					for _, pat := range from.AllowExternal {
						allowed = allowed || config.Match(pat, imp.target)
					}
					if !allowed {
						code = "forbidden_external_dependency"
					}
				}
				if code != "" {
					report.Diagnostics = append(report.Diagnostics, Diagnostic{
						RuleID: r.ID, Code: code, Severity: r.Severity,
						Message: fmt.Sprintf("%s may not import %s", s.pkg.PkgPath, imp.target),
						File:    s.file, Line: imp.pos.Line, Column: imp.pos.Column,
						SourcePackage: s.pkg.PkgPath, TargetPackage: imp.target,
						SourceLayer: from.Name, TargetLayer: targetLayer,
					})
				}
			}
		}
	}
	sort.Slice(report.Diagnostics, func(i, j int) bool {
		a, b := report.Diagnostics[i], report.Diagnostics[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Column != b.Column {
			return a.Column < b.Column
		}
		if a.RuleID != b.RuleID {
			return a.RuleID < b.RuleID
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.SourcePackage < b.SourcePackage
	})
	return report, nil
}
