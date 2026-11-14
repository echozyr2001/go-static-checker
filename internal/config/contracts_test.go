package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContractConfiguration(t *testing.T) {
	const template = `schema_version=1
[project]
root="."
include_tests=false
build_tags=[]
[[rules]]
id="C"
kind="call_contract"
description="contract"
severity="error"
[rules.contract]
entries=["example.com/app::Entry"]
forbidden=["os::Exit"]
trusted=[]
on_unknown="error"
`
	for _, tc := range []struct {
		name, old, new string
		valid          bool
	}{
		{"valid", "", "", true},
		{"method", "app::Entry", "app::Worker.Run", true},
		{"wildcard", "app::Entry", "app::*", true},
		{"missing trust", "trusted=[]", "", false},
		{"missing policy", `on_unknown="error"`, "", false},
		{"ignored unknown", `on_unknown="error"`, `on_unknown="ignore"`, false},
		{"empty entry", `entries=["example.com/app::Entry"]`, `entries=[]`, false},
		{"empty forbidden", `forbidden=["os::Exit"]`, `forbidden=[]`, false},
		{"bad symbol", "app::Entry", "app/Entry", false},
		{"recursive wildcard", "app::Entry", "app/...::*", false},
		{"duplicate", `forbidden=["os::Exit"]`, `forbidden=["os::Exit","os::Exit"]`, false},
		{"wrong table", "[rules.contract]", "[rules.contracts]", false},
		{"wrong kind", `kind="call_contract"`, `kind="source_format"`, false},
		{"mixed fields", "[rules.contract]", "allow_stdlib=true\n[rules.contract]", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := template
			if tc.old != "" {
				data = strings.Replace(data, tc.old, tc.new, 1)
			}
			file := filepath.Join(t.TempDir(), "rules.toml")
			if err := os.WriteFile(file, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(file)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
		})
	}
}
