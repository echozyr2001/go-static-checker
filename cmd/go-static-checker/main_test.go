package main

import (
	"bytes"
	"encoding/json"
	"github.com/echozyr2001/go-static-checker/internal/checker"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLI(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{"go.mod": "module example.com/app\n\ngo 1.24\n", "app.go": "package app\nimport _ \"fmt\"\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	template := `schema_version = 1
[project]
root = "."
include_tests = false
build_tags = []
[[rules]]
id = "A"
kind = "layer_dependencies"
description = "test"
severity = "error"
unmatched = "error"
allow_same_layer = true
allow_stdlib = false
external_default = "deny"
[[rules.layers]]
name = "app"
packages = ["."]
allow = []
allow_external = []
`
	file := filepath.Join(root, "rules.toml")
	for _, tc := range []struct {
		name, config string
		args         []string
		code         int
	}{
		{"violation", template, []string{"-format", "json"}, 1},
		{"pass", strings.Replace(template, "allow_stdlib = false", "allow_stdlib = true", 1), nil, 0},
		{"warning", strings.Replace(template, "severity = \"error\"", "severity = \"warning\"", 1), nil, 0},
		{"config error", strings.Replace(template, "schema_version = 1", "schema_version = 2", 1), nil, 2},
		{"invalid format", template, []string{"-format", "xml"}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(file, []byte(tc.config), 0600); err != nil {
				t.Fatal(err)
			}
			var out, stderr bytes.Buffer
			code := run(append([]string{"-config", file}, tc.args...), &out, &stderr)
			if code != tc.code {
				t.Fatalf("exit=%d stdout=%s stderr=%s", code, &out, &stderr)
			}
			if tc.name == "violation" {
				var r checker.Report
				if err := json.Unmarshal(out.Bytes(), &r); err != nil {
					t.Fatal(err)
				}
				if len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "forbidden_stdlib_dependency" || r.Diagnostics[0].Line != 2 {
					t.Fatalf("%+v", r)
				}
			}
			if tc.code == 2 && (stderr.Len() == 0 || out.Len() != 0) {
				t.Fatalf("failure output: %s %s", &out, &stderr)
			}
		})
	}
}

func TestCustomFormatCLI(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"go.mod":     "module example.com/app\n\ngo 1.24\n",
		"BadFile.go": "package app\n// a line exceeding twenty columns\n",
		"rules.toml": `schema_version = 1
[project]
root = "."
include_tests = false
build_tags = []
[[rules]]
id = "FMT001"
kind = "source_format"
description = "format"
severity = "error"
[rules.format]
packages = ["."]
max_line_length = 20
tab_width = 4
file_name_pattern = '[a-z_]+\.go'
package_name_pattern = '[a-z]+'
[[rules]]
id = "LAYOUT001"
kind = "file_layout"
description = "layout"
severity = "warning"
[rules.layout]
required_files = ["README.md"]
required_directories = []
`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, format := range []string{"json", "text"} {
		t.Run(format, func(t *testing.T) {
			var out, stderr bytes.Buffer
			code := run([]string{"-config", filepath.Join(root, "rules.toml"), "-format", format}, &out, &stderr)
			if code != 1 || stderr.Len() != 0 {
				t.Fatalf("exit=%d out=%s err=%s", code, &out, &stderr)
			}
			if format == "json" {
				var report checker.Report
				if err := json.Unmarshal(out.Bytes(), &report); err != nil {
					t.Fatal(err)
				}
				if len(report.Diagnostics) != 3 || report.Diagnostics[0].Code != "invalid_file_name" || report.Diagnostics[1].Code != "line_too_long" || report.Diagnostics[1].Line != 2 || report.Diagnostics[1].Column != 21 || report.Diagnostics[2].Code != "missing_required_file" {
					t.Fatalf("%+v", report)
				}
			} else {
				for _, want := range []string{"BadFile.go:1:1: error FMT001 invalid_file_name", "BadFile.go:2:21: error FMT001 line_too_long", "README.md:0:0: warning LAYOUT001 missing_required_file"} {
					if !strings.Contains(out.String(), want) {
						t.Fatalf("missing %q in %s", want, &out)
					}
				}
			}
		})
	}
	// Invalid regular expressions must produce an incomplete check, never a success report.
	invalid := strings.Replace(files["rules.toml"], "package_name_pattern = '[a-z]+'", "package_name_pattern = '['", 1)
	if err := os.WriteFile(filepath.Join(root, "rules.toml"), []byte(invalid), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code := run([]string{"-config", filepath.Join(root, "rules.toml")}, &out, &stderr); code != 2 || out.Len() != 0 || !strings.Contains(stderr.String(), "invalid package_name_pattern") {
		t.Fatalf("exit=%d out=%s err=%s", code, &out, &stderr)
	}
}

func TestCommentRulesCLI(t *testing.T) {
	root := t.TempDir()
	template := `schema_version = 1
[project]
root = "."
include_tests = false
build_tags = []
[[rules]]
id = "COMMENTS"
kind = "required_comments"
description = "exported documentation"
severity = "error"
[rules.comments]
packages = ["..."]
declarations = ["function"]
visibility = "exported"
include_tests = false
include_generated = false
exclude_go_directives = true
position = "doc"
require_nonempty = true
`
	for name, body := range map[string]string{"go.mod": "module example.com/app\n\ngo 1.24\n", "app.go": "package app\nfunc Exported() {}\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, severity := range []string{"error", "warning"} {
		if err := os.WriteFile(filepath.Join(root, "rules.toml"), []byte(strings.Replace(template, `severity = "error"`, `severity = "`+severity+`"`, 1)), 0600); err != nil {
			t.Fatal(err)
		}
		var out, stderr bytes.Buffer
		code := run([]string{"-config", filepath.Join(root, "rules.toml"), "-format", "json"}, &out, &stderr)
		want := 1
		if severity == "warning" {
			want = 0
		}
		if code != want || stderr.Len() != 0 {
			t.Fatalf("exit=%d out=%s err=%s", code, &out, &stderr)
		}
		var report checker.Report
		if err := json.Unmarshal(out.Bytes(), &report); err != nil {
			t.Fatal(err)
		}
		if len(report.Diagnostics) != 1 {
			t.Fatal(report)
		}
		d := report.Diagnostics[0]
		if d.Code != "missing_required_comment" || d.Severity != severity || d.File != "app.go" || d.Line != 2 || d.Column != 1 {
			t.Fatal(d)
		}
	}
}

func TestIncludedRulesCLI(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"go.mod":    "module example.com/app\n\ngo 1.24\n",
		"app.go":    "package app\n",
		"README.md": "text",
		"rules.toml": `schema_version = 1
includes = ["rules/files.toml"]
[project]
root = "."
include_tests = false
build_tags = []
[[rules]]
id = "ROOT"
kind = "file_layout"
description = "root requirement"
severity = "error"
[rules.layout]
required_files = ["missing.txt"]
required_directories = []
`,
		"rules/files.toml": `schema_version = 1
[[rules]]
id = "INCLUDED"
kind = "file_layout"
description = "included requirement"
severity = "error"
[rules.layout]
required_files = ["README.md", "other.txt"]
required_directories = []
`,
	}
	for name, body := range files {
		filename := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var out, stderr bytes.Buffer
	args := []string{"-config", filepath.Join(root, "rules.toml"), "-format", "json"}
	if code := run(args, &out, &stderr); code != 1 || stderr.Len() != 0 {
		t.Fatalf("exit=%d out=%s err=%s", code, &out, &stderr)
	}
	var report checker.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Packages != 1 || report.Files != 1 || len(report.Diagnostics) != 2 {
		t.Fatal(report)
	}
	for i, want := range []struct{ id, file string }{{"ROOT", "missing.txt"}, {"INCLUDED", "other.txt"}} {
		d := report.Diagnostics[i]
		if d.RuleID != want.id || d.File != want.file || d.Code != "missing_required_file" {
			t.Fatal(d)
		}
	}
	for _, name := range []string{"missing.txt", "other.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("text"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	out.Reset()
	stderr.Reset()
	if code := run(args, &out, &stderr); code != 0 || stderr.Len() != 0 {
		t.Fatalf("exit=%d out=%s err=%s", code, &out, &stderr)
	}
	if err := os.Remove(filepath.Join(root, "rules/files.toml")); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	stderr.Reset()
	if code := run(args, &out, &stderr); code != 2 || out.Len() != 0 || !strings.Contains(stderr.String(), "files.toml") {
		t.Fatalf("exit=%d out=%s err=%s", code, &out, &stderr)
	}
}

func TestTestQualityCLI(t *testing.T) {
	root := t.TempDir()
	template := `schema_version = 1
[project]
root = "."
include_tests = false
build_tags = []
[[rules]]
id = "DUP"
kind = "test_duplicates"
description = "duplicates"
severity = "warning"
[rules.tests]
packages = ["..."]
include_generated = false
duplicate_bodies = true
duplicate_cases = true
[[rules]]
id = "WAIT"
kind = "test_waiting"
description = "waiting"
severity = "warning"
[rules.tests]
packages = ["..."]
include_generated = false
`
	files := map[string]string{
		"go.mod": "module example.com/app\n\ngo 1.27.3\n",
		"app.go": "package app\n",
		"app_test.go": `package app
import("testing";"time")
func TestOne(t *testing.T){t.Log(1)}
func TestTwo(t *testing.T){t.Log(1)}
func TestRows(t *testing.T){
 for _,tc:=range []struct{name string;input int}{{"a",1},{"b",1}} {
 t.Run(tc.name,func(t *testing.T){t.Log(tc.input)})
 }
}
func TestWait(t *testing.T){time.Sleep(time.Second)}
`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	filename := filepath.Join(root, "rules.toml")
	for _, severity := range []string{"warning", "error"} {
		text := strings.ReplaceAll(template, `severity = "warning"`, `severity = "`+severity+`"`)
		if err := os.WriteFile(filename, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		var out, stderr bytes.Buffer
		code := run([]string{"-config", filename, "-format", "json"}, &out, &stderr)
		want := 0
		if severity == "error" {
			want = 1
		}
		if code != want || stderr.Len() != 0 {
			t.Fatalf("exit=%d out=%s err=%s", code, &out, &stderr)
		}
		var report checker.Report
		if err := json.Unmarshal(out.Bytes(), &report); err != nil {
			t.Fatal(err)
		}
		if len(report.Diagnostics) != 3 {
			t.Fatal(report)
		}
		for i, want := range []string{"duplicate_test_body", "duplicate_test_case", "real_time_wait"} {
			if report.Diagnostics[i].Code != want || report.Diagnostics[i].Severity != severity {
				t.Fatal(report.Diagnostics)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(root, "app_test.go"), []byte("package app\nvar _ = unknown\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code := run([]string{"-config", filename}, &out, &stderr); code != 2 || out.Len() != 0 || !strings.Contains(stderr.String(), "unknown") {
		t.Fatalf("exit=%d out=%s err=%s", code, &out, &stderr)
	}
}
