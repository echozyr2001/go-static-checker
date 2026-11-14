package checker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/echozyr2001/go-static-checker/internal/config"
	"golang.org/x/tools/go/packages"
)

// loadFormatTestSources discovers build-selected tests without their imports.
func loadFormatTestSources(ctx context.Context, c config.Config, env, patterns []string,
	own map[string]*packages.Package) ([]source, error) {
	enabled := false
	for _, r := range c.Rules {
		if r.Format != nil && r.Format.IncludeTests || r.Comments != nil && *r.Comments.IncludeTests {
			enabled = true
		}
	}
	if !enabled {
		return nil, nil
	}
	args := append([]string{"list", "-json", "-mod=readonly",
		"-tags=" + strings.Join(c.Project.BuildTags, ",")}, patterns...)
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir, cmd.Env = c.Project.Root, env
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("list test sources: %w: %s", err, stderr.String())
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	var sources []source
	for {
		var p struct {
			ImportPath                string
			TestGoFiles, XTestGoFiles []string
		}
		err := decoder.Decode(&p)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		owner := own[p.ImportPath]
		if owner == nil {
			return nil, fmt.Errorf("unknown test source package %s", p.ImportPath)
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(p.ImportPath, owner.Module.Path), "/")
		if rel == "" {
			rel = "."
		}
		selected := false
		for _, r := range c.Rules {
			var patterns []string
			if r.Format != nil && r.Format.IncludeTests {
				patterns = r.Format.Packages
			}
			if r.Comments != nil && *r.Comments.IncludeTests {
				patterns = r.Comments.Packages
			}
			for _, pattern := range patterns {
				selected = selected || config.Match(pattern, rel)
			}
		}
		if !selected {
			continue
		}
		for _, name := range append(p.TestGoFiles, p.XTestGoFiles...) {
			filename := filepath.Join(owner.Module.Dir, filepath.FromSlash(rel), name)
			content, err := os.ReadFile(filename)
			if err != nil {
				return nil, err
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, filename, content, parser.ParseComments|parser.AllErrors)
			if err != nil {
				return nil, err
			}
			relative, err := filepath.Rel(c.Project.Root, filename)
			if err != nil {
				return nil, err
			}
			sources = append(sources, source{pkg: owner, file: filepath.ToSlash(relative), content: content,
				packageName: f.Name.Name, packagePosition: fset.PositionFor(f.Name.Pos(), false),
				node: f, fset: fset, isTest: true})
		}
	}
	return sources, nil
}
