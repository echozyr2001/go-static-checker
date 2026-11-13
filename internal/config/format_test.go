package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const formatConfig = `schema_version = 1
[project]
root = "."
include_tests = false
build_tags = []
[[rules]]
id = "FMT001"
kind = "source_format"
description = "custom format"
severity = "error"
[rules.format]
packages = ["..."]
max_line_length = 120
tab_width = 4
file_name_pattern = '[a-z][a-z0-9_]*\.go'
package_name_pattern = '[a-z][a-z0-9]*'
`
const layoutConfig = `schema_version = 1
[project]
root = "."
include_tests = false
build_tags = []
[[rules]]
id = "LAYOUT001"
kind = "file_layout"
description = "required files"
severity = "error"
[rules.layout]
required_files = ["README.md"]
required_directories = ["cmd"]
`

func TestCustomRuleConfig(t *testing.T) {
	for _, tc := range []struct{ name, base, old, new, want string }{
		{"valid format", formatConfig, "", "", ""},
		{"valid layout", layoutConfig, "", "", ""},
		{"missing line width", formatConfig, "max_line_length = 120", "", "requires"},
		{"negative line width", formatConfig, "max_line_length = 120", "max_line_length = -1", "nonnegative"},
		{"bad tab", formatConfig, "tab_width = 4", "tab_width = 0", "positive"},
		{"missing pattern", formatConfig, "package_name_pattern = '[a-z][a-z0-9]*'", "", "requires"},
		{"regex", formatConfig, "'[a-z][a-z0-9]*'", "'['", "invalid package_name_pattern"},
		{"scope", formatConfig, `packages = ["..."]`, `packages = ["internal/*"]`, "invalid package pattern"},
		{"empty scope", formatConfig, `packages = ["..."]`, `packages = []`, "requires"},
		{"unknown option", formatConfig, "tab_width", "tab_size", "unknown"},
		{"cross kind even empty", formatConfig, `severity = "error"`, `severity = "error"
unmatched = ""`, "not allowed"},
		{"cross kind array", formatConfig, `severity = "error"`, `severity = "error"
layers = []`, "not allowed"},
		{"wrong table", formatConfig, `kind = "source_format"`, `kind = "file_layout"`, "requires only"},
		{"unknown kind", formatConfig, `kind = "source_format"`, `kind = "format"`, "unknown kind"},
		{"missing directories", layoutConfig, `required_directories = ["cmd"]`, "", "requires"},
		{"absolute path", layoutConfig, `["README.md"]`, `["/README.md"]`, "invalid project-relative"},
		{"escape", layoutConfig, `["README.md"]`, `["../README.md"]`, "invalid project-relative"},
		{"unclean", layoutConfig, `["README.md"]`, `["docs/../README.md"]`, "invalid project-relative"},
		{"wildcard", layoutConfig, `["README.md"]`, `["*.md"]`, "invalid project-relative"},
		{"duplicate", layoutConfig, `["README.md"]`, `["README.md", "README.md"]`, "duplicate"},
		{"conflict", layoutConfig, `["cmd"]`, `["README.md"]`, "conflicting"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := tc.base
			if tc.old != "" {
				data = strings.Replace(data, tc.old, tc.new, 1)
			}
			filename := filepath.Join(t.TempDir(), "rules.toml")
			if err := os.WriteFile(filename, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(filename)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}
func TestRejectEmptyCustomRules(t *testing.T) {
	c, err := Load("../../rules.toml")
	if err != nil {
		t.Fatal(err)
	}
	zero, four := 0, 4
	empty := ""
	c.Rules = []Rule{{ID: "FMT", Kind: "source_format", Description: &empty, Severity: "error", Format: &SourceFormat{
		Packages: []string{"..."}, MaxLineLength: &zero, TabWidth: &four, FileNamePattern: &empty, PackageNamePattern: &empty}}}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "at least one") {
		t.Fatalf("got %v", err)
	}
	c.Rules = []Rule{{ID: "LAYOUT", Kind: "file_layout", Description: &empty, Severity: "error", Layout: &FileLayout{RequiredFiles: []string{}, RequiredDirectories: []string{}}}}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "at least one") {
		t.Fatalf("got %v", err)
	}
}
