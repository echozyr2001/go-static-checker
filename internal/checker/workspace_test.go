package checker

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestWorkspace(t *testing.T) {
	c := fixture(t)
	root := t.TempDir()
	c.Project.Root = root
	write(t, root, "go.work", "go 1.24\nuse (\n ./app\n ./lib\n)\n")
	write(t, root, "app/go.mod", "module example.com/app\ngo 1.24\n")
	write(t, root, "lib/go.mod", "module example.com/lib\ngo 1.24\n")
	write(t, root, "app/handler/a.go", "package handler\nimport \"example.com/lib/repo\"\nvar _ = repo.Value\n")
	write(t, root, "lib/repo/a.go", "package repo\nconst Value = 1\n")
	write(t, root, "ignored/go.mod", "module example.com/ignored\ngo 1.24\n")
	write(t, root, "ignored/bad.go", "invalid syntax")
	t.Setenv("GOWORK", "off")
	r, err := Run(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if r.Packages != 2 || r.Files != 2 || len(r.Diagnostics) != 1 {
		t.Fatalf("unexpected report: %+v", r)
	}
	d := r.Diagnostics[0]
	if d.Code != "forbidden_layer_dependency" || d.File != "app/handler/a.go" || d.TargetLayer != "repo" {
		t.Fatalf("unexpected diagnostic: %+v", d)
	}
	write(t, root, "lib/repo/a_test.go", "package repo_test\nimport _ \"missing.invalid/test\"\nfunc A() {}\nfunc B() {}\n")
	c = spacingConfig(t, root, true)
	c.Rules[0].Format.Packages = []string{"repo"}
	r, err = Run(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if r.Files != 3 || len(r.Diagnostics) != 1 || r.Diagnostics[0].File != "lib/repo/a_test.go" || r.Diagnostics[0].Code != "missing_declaration_blank_line" {
		t.Fatalf("test sources: %+v", r)
	}
}

func TestWorkspaceMemberLocations(t *testing.T) {
	for _, location := range []string{".", "../sibling", "absolute"} {
		t.Run(location, func(t *testing.T) {
			base := t.TempDir()
			root := filepath.Join(base, "workspace")
			dir := root
			use := location
			if location == "../sibling" {
				dir = filepath.Join(base, "sibling")
			}
			if location == "absolute" {
				dir = filepath.Join(base, "absolute")
				use = filepath.ToSlash(dir)
			}
			write(t, root, "go.work", "go 1.24\nuse "+strconv.Quote(use)+"\n")
			write(t, dir, "go.mod", "module example.com/member\ngo 1.24\n")
			write(t, dir, "a.go", "package member\nfunc A() {}\nfunc B() {}\n")
			c := spacingConfig(t, root, false)
			r, err := Run(context.Background(), c)
			if err != nil {
				t.Fatal(err)
			}
			relative, err := filepath.Rel(root, filepath.Join(dir, "a.go"))
			if err != nil {
				t.Fatal(err)
			}
			if r.Packages != 1 || len(r.Diagnostics) != 1 || r.Diagnostics[0].File != filepath.ToSlash(relative) {
				t.Fatalf("unexpected report: %+v", r)
			}
		})
	}
}

func TestInvalidWorkspace(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"empty", "go 1.24\n", "no workspace modules"},
		{"syntax", "not a workspace", "unknown directive"},
		{"missing member", "go 1.24\nuse ./missing\n", "workspace module"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := fixture(t)
			write(t, c.Project.Root, "go.work", tc.body)
			_, err := Run(context.Background(), c)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}
