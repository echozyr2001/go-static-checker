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
