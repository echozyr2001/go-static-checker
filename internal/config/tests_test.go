package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTestRulesStrict(t *testing.T) {
	valid := `schema_version = 1
[project]
root = "."
include_tests = false
build_tags = []
[[rules]]
id = "TEST"
kind = "test_duplicates"
description = "quality"
severity = "warning"
[rules.tests]
packages = ["..."]
include_generated = false
duplicate_bodies = true
duplicate_cases = true
`
	for _, tc := range []struct{ name, old, replacement, want string }{
		{"valid", "", "", ""},
		{"missing generated", "include_generated = false", "", "include_generated is required"},
		{"missing toggle", "duplicate_cases = true", "", "duplicate_cases is required"},
		{"disabled", "duplicate_bodies = true\nduplicate_cases = true", "duplicate_bodies = false\nduplicate_cases = false", "enable"},
		{"empty scope", `packages = ["..."]`, `packages = []`, "packages must not be empty"},
		{"bad scope", `packages = ["..."]`, `packages = ["*"]`, "invalid package pattern"},
		{"typo", "duplicate_cases", "duplicate_case", "unknown"},
		{"wrong kind", `kind = "test_duplicates"`, `kind = "test_waiting"`, "not allowed"},
		{"other table", "[rules.tests]", "[rules.format]", "unknown"},
		{"layer option", `severity = "warning"`, "severity = \"warning\"\nallow_stdlib = true", "requires only"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text := valid
			if tc.old != "" {
				text = strings.Replace(text, tc.old, tc.replacement, 1)
			}
			name := filepath.Join(t.TempDir(), "rules.toml")
			if err := os.WriteFile(name, []byte(text), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(name)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v", err)
			}
		})
	}
}
