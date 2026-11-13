package checker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/echozyr2001/go-static-checker/internal/config"
)

func spacingConfig(t *testing.T, root string, includeTests bool) config.Config {
	t.Helper()
	tests := "false"
	if includeTests {
		tests = "true"
	}
	text := `schema_version = 1
[project]
root = "."
include_tests = false
build_tags = []
[[rules]]
id = "SPACE"
kind = "source_format"
description = "spacing"
severity = "error"
[rules.format]
packages = ["..."]
max_line_length = 0
tab_width = 4
file_name_pattern = ""
package_name_pattern = ""
min_blank_lines_between_declarations = 1
include_tests = ` + tests + "\n"
	name := filepath.Join(root, "spacing.toml")
	if err := os.WriteFile(name, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(name)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestDeclarationSpacing(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		wantLine   int
	}{
		{"functions", "func A() {}\nfunc B() {}\n", 3},
		{"structs", "type A struct{}\ntype B struct{}\n", 3},
		{"mixed", "type A struct{}\nfunc (A) B() {}\n", 3},
		{"blank", "func A() {}\n\nfunc B() {}\n", 0},
		{"whitespace blank", "func A() {}\n \t\nfunc B() {}\n", 0},
		{"doc not blank", "func A() {}\n// B documents B.\nfunc B() {}\n", 4},
		{"before doc", "func A() {}\n\n// B documents B.\nfunc B() {}\n", 0},
		{"comment internal blank", "func A() {}\n/* B\n\n docs */\nfunc B() {}\n", 6},
		{"trailing comment", "func A() {} // end\nfunc B() {}\n", 3},
		{"same line", "func A() {}; func B() {}\n", 2},
		{"grouped structs", "type (\n A struct{}\n B struct{}\n)\n", 4},
		{"fields", "type A struct {\n X int\n Y struct{}\n}\n", 0},
		{"local declarations", "func A() {\n type X struct{}\n type Y struct{}\n}\n", 0},
		{"unrelated", "func A() {}\nvar X int\nfunc B() {}\n", 0},
		{"line directives", "func A() {}\n//line fake.go:900\nfunc B() {}\n", 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			write(t, root, "go.mod", "module example.com/spacing\n\ngo 1.24\n")
			write(t, root, "spacing.go", "package spacing\n"+tc.body)
			c := spacingConfig(t, root, false)
			r, err := Run(context.Background(), c)
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantLine == 0 {
				if len(r.Diagnostics) != 0 {
					t.Fatalf("unexpected: %+v", r.Diagnostics)
				}
				return
			}
			if len(r.Diagnostics) != 1 {
				t.Fatalf("want one diagnostic: %+v", r.Diagnostics)
			}
			d := r.Diagnostics[0]
			if d.Code != "missing_declaration_blank_line" || d.Line != tc.wantLine || d.Column < 1 || d.RuleID != "SPACE" || d.File != "spacing.go" || r.ExitCode() != 1 {
				t.Fatalf("%+v", d)
			}
		})
	}
}

func TestSpacingTestFiles(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/spacing\n\ngo 1.24\n")
	write(t, root, "spacing.go", "package spacing\n")
	write(t, root, "spacing_test.go", "package spacing\nfunc A() {}\nfunc B() {}\n")
	write(t, root, "external_test.go", "package spacing_test\nfunc A() {}\nfunc B() {}\n")
	write(t, root, "tag_test.go", "//go:build custom\n\npackage spacing\nfunc C() {}\nfunc D() {}\n")
	c := spacingConfig(t, root, false)
	r, err := Run(context.Background(), c)
	if err != nil || len(r.Diagnostics) != 0 || r.Files != 1 {
		t.Fatalf("disabled: %+v %v", r, err)
	}
	c = spacingConfig(t, root, true)
	r, err = Run(context.Background(), c)
	if err != nil || len(r.Diagnostics) != 2 || r.Files != 3 {
		t.Fatalf("enabled: %+v %v", r, err)
	}
	for _, d := range r.Diagnostics {
		if !strings.HasSuffix(d.File, "_test.go") || d.Line != 3 {
			t.Fatalf("%+v", d)
		}
	}
	c.Project.BuildTags = []string{"custom"}
	r, err = Run(context.Background(), c)
	if err != nil || len(r.Diagnostics) != 3 || r.Files != 4 {
		t.Fatalf("tags: %+v %v", r, err)
	}
	write(t, root, "spacing_test.go", "package spacing\nfunc Broken( {\n")
	if _, err := Run(context.Background(), c); err == nil {
		t.Fatal("invalid test syntax must fail")
	}
}

func TestSpacingMinimumAndOptIn(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/spacing\n\ngo 1.24\n")
	write(t, root, "spacing.go", "package spacing\r\nfunc A() {}\r\n\r\nfunc B() {}\r\n")
	c := spacingConfig(t, root, false)
	c.Rules[0].Format.MinBlankLinesBetweenDeclarations = 2
	c.Rules[0].Severity = "warning"
	r, err := Run(context.Background(), c)
	if err != nil || len(r.Diagnostics) != 1 || r.Diagnostics[0].Line != 4 || r.ExitCode() != 0 {
		t.Fatalf("minimum: %+v %v", r, err)
	}
	c.Rules[0].Format.MinBlankLinesBetweenDeclarations = 0
	*c.Rules[0].Format.FileNamePattern = `.*\.go`
	r, err = Run(context.Background(), c)
	if err != nil || len(r.Diagnostics) != 0 {
		t.Fatalf("disabled: %+v %v", r, err)
	}
	c.Rules[0].Format.MinBlankLinesBetweenDeclarations = -1
	if _, err := Run(context.Background(), c); err == nil || !strings.Contains(err.Error(), "nonnegative") {
		t.Fatalf("negative: %v", err)
	}
}

func TestSpacingRuleIsolation(t *testing.T) {
	c := fixture(t)
	spacing := spacingConfig(t, c.Project.Root, true).Rules[0]
	spacing.Format.Packages = []string{"repo/..."}
	c.Rules = append(c.Rules, formatRule(), spacing)
	write(t, c.Project.Root, "repo/spacing_test.go", "package repo\nfunc A() {}\nfunc B() {}\n")
	// Out-of-scope test bodies must not be parsed by the formatting rule.
	write(t, c.Project.Root, "handler/bad_test.go", "package handler\nfunc Broken( {\n")
	r, err := Run(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Diagnostics) != 4 || r.Files != 5 {
		t.Fatalf("isolation: %+v", r)
	}
	found := 0
	for _, d := range r.Diagnostics {
		if d.RuleID == "SPACE" {
			found++
			if d.File != "repo/spacing_test.go" {
				t.Fatalf("%+v", d)
			}
		}
	}
	if found != 1 {
		t.Fatalf("diagnostics: %+v", r.Diagnostics)
	}
}

func TestSpacingTestOnlyPackage(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/spacing\n\ngo 1.24\n")
	write(t, root, "only_test.go", "package spacing_test\nfunc A() {}\nfunc B() {}\n")
	c := spacingConfig(t, root, true)
	r, err := Run(context.Background(), c)
	if err != nil || len(r.Diagnostics) != 1 || r.Files != 1 {
		t.Fatalf("test only: %+v %v", r, err)
	}
}

func TestSpacingCRLFBlockCommentDoesNotCountAsBlankLines(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/spacing\n\ngo 1.24\n")
	body := "package spacing\r\nfunc A() {}\r\n/*" + strings.Repeat("\r\n", 30) + "*/\r\n\r\nfunc B() {}\r\n"
	write(t, root, "spacing.go", body)
	c := spacingConfig(t, root, false)
	c.Rules[0].Format.MinBlankLinesBetweenDeclarations = 2
	r, err := Run(context.Background(), c)
	if err != nil || len(r.Diagnostics) != 1 {
		t.Fatalf("comment blank lines must not count: %+v %v", r, err)
	}
}
