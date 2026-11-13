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
