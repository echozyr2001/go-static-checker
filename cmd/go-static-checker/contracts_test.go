package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/echozyr2001/go-static-checker/internal/checker"
)

func TestContractCLI(t *testing.T) {
	for _, tc := range []struct {
		name, body, unknown, status string
		exit                        int
	}{
		{"safe", "func Entry(){}", "error", "satisfied", 0},
		{"violation", "func Entry(){panic(1)}", "error", "violated", 1},
		{"strict unknown", "func Entry(f func()){f()}", "error", "unknown", 2},
		{"warning unknown", "func Entry(f func()){f()}", "warning", "unknown", 0},
		{"violation plus unknown", "func Entry(f func()){f();panic(1)}", "error", "violated", 2},
		{"trusted", `import "strings"; func Entry(s string) string{return strings.TrimSpace(s)}`, "error", "satisfied", 0},
		{"type error", "func Entry(){missing()}", "error", "", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for name, body := range map[string]string{
				"go.mod": "module example.com/app\n\ngo 1.27.3\n",
				"app.go": "package app\n" + tc.body + "\n",
				"rules.toml": `schema_version=1
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
forbidden=["builtin::panic"]
trusted=["strings::*"]
on_unknown="` + tc.unknown + `"
`,
			} {
				if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			for _, format := range []string{"text", "json"} {
				var out, stderr bytes.Buffer
				code := run([]string{"-config", filepath.Join(root, "rules.toml"), "-format", format}, &out, &stderr)
				if code != tc.exit {
					t.Fatalf("exit=%d stdout=%s stderr=%s", code, &out, &stderr)
				}
				if tc.status == "" {
					if out.Len() != 0 || !strings.Contains(stderr.String(), "missing") {
						t.Fatalf("%s %s", &out, &stderr)
					}
					continue
				}
				if stderr.Len() != 0 {
					t.Fatal(&stderr)
				}
				if format == "text" {
					if !strings.Contains(out.String(), "C example.com/app::Entry: "+tc.status) {
						t.Fatal(&out)
					}
					if tc.name == "trusted" && !strings.Contains(out.String(), "strings::TrimSpace") {
						t.Fatal(&out)
					}
				} else {
					var r checker.Report
					if err := json.Unmarshal(out.Bytes(), &r); err != nil {
						t.Fatal(err)
					}
					if len(r.Verifications) != 1 || r.Verifications[0].Status != tc.status {
						t.Fatal(r)
					}
					if tc.name == "trusted" && (len(r.Verifications[0].Assumptions) != 1 || r.Verifications[0].Assumptions[0].Symbol != "strings::TrimSpace") {
						t.Fatal(r)
					}
					if tc.name == "strict unknown" && (r.Verifications[0].Complete || len(r.Diagnostics) != 1) {
						t.Fatal(r)
					}
				}
			}
		})
	}
}

func TestFormalCLIExport(t *testing.T) {
	root := t.TempDir()
	for file, body := range map[string]string{
		"go.mod": "module example.com/app\n\ngo 1.27.3\n",
		"app.go": "package app\nfunc Entry(){}\n",
		"rules.toml": `schema_version=1
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
forbidden=["builtin::panic"]
trusted=[]
on_unknown="warning"
`,
	} {
		if err := os.WriteFile(filepath.Join(root, file), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	directory := filepath.Join(t.TempDir(), "formal")
	args := []string{"-config", filepath.Join(root, "rules.toml"), "-export-formal", directory}
	var out, stderr bytes.Buffer
	if code := run(args, &out, &stderr); code != 0 {
		t.Fatalf("exit %d: %s %s", code, &out, &stderr)
	}
	for _, file := range []string{"Model.tla", "Model.cfg", "Model.lean", "manifest.json"} {
		if _, err := os.Stat(filepath.Join(directory, "contract_000", file)); err != nil {
			t.Fatal(err)
		}
	}
	out.Reset()
	stderr.Reset()
	if code := run(args, &out, &stderr); code != 2 || out.Len() != 0 || !strings.Contains(stderr.String(), "fresh formal output") {
		t.Fatalf("exit %d: %s %s", code, &out, &stderr)
	}
	if err := os.WriteFile(filepath.Join(root, "app.go"), []byte("package app\nfunc Entry(f func()){f()}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	args[len(args)-1] = filepath.Join(t.TempDir(), "unknown")
	out.Reset()
	stderr.Reset()
	if code := run(args, &out, &stderr); code != 2 || !strings.Contains(stderr.String(), "incomplete") {
		t.Fatalf("exit %d: %s %s", code, &out, &stderr)
	}
}
