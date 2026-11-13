package checker

import (
	"context"
	"github.com/echozyr2001/go-static-checker/internal/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, root, name, body string) {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}
func fixture(t *testing.T) config.Config {
	t.Helper()
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/app\n\ngo 1.24\n")
	write(t, root, "handler/a.go", "package handler\nimport alias \"example.com/app/repo\"\nvar _ = alias.Value\n")
	write(t, root, "handler/b.go", "package handler\nimport . \"example.com/app/repo\"\nvar _ = Value\n")
	write(t, root, "handler/c.go", "package handler\nimport _ \"example.com/app/repo\"\n")
	write(t, root, "handler/a_test.go", "package handler\nimport _ \"missing.invalid/pkg\"\n")
	write(t, root, "handler/tag.go", "//go:build custom\n\npackage handler\nimport _ \"fmt\"\n")
	write(t, root, "repo/repo.go", "package repo\nconst Value = 1\n")
	yes, no := true, false
	desc := "test"
	return config.Config{SchemaVersion: 1, Project: config.Project{Root: root, IncludeTests: &no, BuildTags: []string{}}, Rules: []config.Rule{{ID: "ARCH001", Kind: "layer_dependencies", Description: &desc, Severity: "error", Unmatched: "error", AllowSameLayer: &yes, AllowStdlib: &no, ExternalDefault: "deny", Layers: []config.Layer{{Name: "handler", Packages: []string{"handler/..."}, Allow: []string{}, AllowExternal: []string{}}, {Name: "repo", Packages: []string{"repo/..."}, Allow: []string{}, AllowExternal: []string{}}}}}}
}
func TestDirectImports(t *testing.T) {
	c := fixture(t)
	r, err := Run(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if r.Packages != 2 || r.Files != 4 || len(r.Diagnostics) != 3 || r.ExitCode() != 1 {
		t.Fatalf("unexpected report: %+v", r)
	}
	for i, d := range r.Diagnostics {
		if d.Code != "forbidden_layer_dependency" || d.Line != 2 || d.Column == 0 || d.TargetLayer != "repo" || d.SourceLayer != "handler" || d.File != []string{"handler/a.go", "handler/b.go", "handler/c.go"}[i] {
			t.Fatalf("bad diagnostic: %+v", d)
		}
	}
	c.Rules[0].Layers[0].Allow = []string{"repo"}
	r, err = Run(context.Background(), c)
	if err != nil || r.ExitCode() != 0 || len(r.Diagnostics) != 0 {
		t.Fatalf("allowed: %+v %v", r, err)
	}
	c.Project.BuildTags = []string{"custom"}
	r, err = Run(context.Background(), c)
	if err != nil || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "forbidden_stdlib_dependency" || r.Files != 5 {
		t.Fatalf("tags: %+v %v", r, err)
	}
	c.Rules[0].Severity = "warning"
	r, err = Run(context.Background(), c)
	if err != nil || r.ExitCode() != 0 || len(r.Diagnostics) != 1 {
		t.Fatalf("warning: %+v %v", r, err)
	}
}
func TestIncompleteAndUnclassified(t *testing.T) {
	for _, mode := range []string{"nested", "missing", "syntax", "ambiguous", "unclassified"} {
		t.Run(mode, func(t *testing.T) {
			c := fixture(t)
			switch mode {
			case "nested":
				write(t, c.Project.Root, "nested/go.mod", "module example.com/nested\n")
			case "missing":
				write(t, c.Project.Root, "repo/repo.go", "package repo\nimport _ \"missing.invalid/pkg\"\nconst Value=1\n")
			case "syntax":
				write(t, c.Project.Root, "repo/repo.go", "package repo\nfunc broken( {\n")
			case "ambiguous":
				c.Rules[0].Layers[1].Packages = []string{"..."}
			case "unclassified":
				c.Rules[0].Layers[1].Packages = []string{"elsewhere"}
			}
			r, err := Run(context.Background(), c)
			if mode == "unclassified" {
				if err != nil || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "unclassified_package" {
					t.Fatalf("%+v %v", r, err)
				}
			} else if err == nil {
				t.Fatalf("%s should fail: %+v", mode, r)
			}
		})
	}
}
func TestExternalAndSameLayer(t *testing.T) {
	c := fixture(t)
	external := t.TempDir()
	write(t, external, "go.mod", "module example.com/app/external\n\ngo 1.24\n")
	write(t, external, "lib.go", "package external\n")
	write(t, c.Project.Root, "go.mod", "module example.com/app\n\ngo 1.24\nrequire example.com/app/external v0.0.0\nreplace example.com/app/external => "+filepath.ToSlash(external)+"\n")
	write(t, c.Project.Root, "handler/ext.go", "package handler\nimport _ \"example.com/app/external\"\n")
	c.Rules[0].Layers[0].Allow = []string{"repo"}
	r, err := Run(context.Background(), c)
	if err != nil || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "forbidden_external_dependency" {
		t.Fatalf("external: %+v %v", r, err)
	}
	c.Rules[0].Layers[0].AllowExternal = []string{"example.com/app/external/..."}
	r, err = Run(context.Background(), c)
	if err != nil || len(r.Diagnostics) != 0 {
		t.Fatalf("allow external: %+v %v", r, err)
	}
	c.Rules[0].Layers = c.Rules[0].Layers[:1]
	c.Rules[0].Layers[0].Packages = []string{"..."}
	c.Rules[0].Layers[0].Allow = []string{}
	*c.Rules[0].AllowSameLayer = false
	r, err = Run(context.Background(), c)
	if err != nil || len(r.Diagnostics) != 3 {
		t.Fatalf("same layer: %+v %v", r, err)
	}
	for _, d := range r.Diagnostics {
		if !strings.Contains(d.Code, "layer") {
			t.Fatal(d)
		}
	}
}
