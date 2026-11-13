package checker

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/echozyr2001/go-static-checker/internal/config"
)

func layoutRule() config.Rule {
	description := "required project structure"
	return config.Rule{ID: "LAYOUT001", Kind: "file_layout", Description: &description, Severity: "error",
		Layout: &config.FileLayout{RequiredFiles: []string{"README.md", "docs/guide.md"}, RequiredDirectories: []string{"configs"}}}
}
func TestFileLayout(t *testing.T) {
	c := fixture(t)
	c.Rules = []config.Rule{layoutRule()}
	write(t, c.Project.Root, "README.md", "hello")
	r, err := Run(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Diagnostics) != 2 || r.Diagnostics[0].File != "configs" || r.Diagnostics[0].Code != "missing_required_directory" || r.Diagnostics[1].File != "docs/guide.md" || r.Diagnostics[1].Code != "missing_required_file" || r.ExitCode() != 1 {
		t.Fatalf("missing: %+v", r)
	}
	for _, d := range r.Diagnostics {
		if d.Line != 0 || d.Column != 0 || d.RuleID != "LAYOUT001" {
			t.Fatalf("%+v", d)
		}
	}
	write(t, c.Project.Root, "docs/guide.md", "guide")
	if err := os.Mkdir(filepath.Join(c.Project.Root, "configs"), 0755); err != nil {
		t.Fatal(err)
	}
	r, err = Run(context.Background(), c)
	if err != nil || len(r.Diagnostics) != 0 || r.ExitCode() != 0 {
		t.Fatalf("valid: %+v %v", r, err)
	}
	c.Rules[0].Layout.RequiredFiles = []string{"configs"}
	c.Rules[0].Layout.RequiredDirectories = []string{"README.md"}
	c.Rules[0].Severity = "warning"
	r, err = Run(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	var codes []string
	for _, d := range r.Diagnostics {
		codes = append(codes, d.Code)
	}
	if !reflect.DeepEqual(codes, []string{"invalid_required_directory", "invalid_required_file"}) || r.ExitCode() != 0 {
		t.Fatalf("wrong types: %+v", r)
	}
}
func TestLayoutSymlinkEscapeIsIncomplete(t *testing.T) {
	c := fixture(t)
	c.Rules = []config.Rule{layoutRule()}
	external := t.TempDir()
	write(t, external, "README.md", "external")
	if err := os.Symlink(filepath.Join(external, "README.md"), filepath.Join(c.Project.Root, "README.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), c); err == nil {
		t.Fatal("symlink outside project should fail the check")
	}
}
