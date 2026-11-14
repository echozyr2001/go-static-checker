package checker

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/echozyr2001/go-static-checker/internal/config"
)

func contractFixture(t *testing.T, body, forbidden, trusted, unknown string) config.Config {
	t.Helper()
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/contract\n\ngo 1.27.3\n")
	write(t, root, "app.go", body)
	write(t, root, "rules.toml", `schema_version = 1
[project]
root = "."
include_tests = false
build_tags = []
[[rules]]
id = "CALL001"
kind = "call_contract"
description = "forbidden call reachability"
severity = "error"
[rules.contract]
entries = ["example.com/contract::Entry"]
forbidden = [`+forbidden+`]
trusted = [`+trusted+`]
on_unknown = "`+unknown+`"
`)
	c, err := config.Load(filepath.Join(root, "rules.toml"))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestContractTransitiveViolation(t *testing.T) {
	c := contractFixture(t, `package app
import "os"
func Entry(){helper()}
func helper(){os.Exit(1)}
`, `"os::Exit"`, "", "error")
	r, err := Run(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if r.ExitCode() != 1 || len(r.Diagnostics) != 1 {
		t.Fatalf("%+v", r)
	}
	d := r.Diagnostics[0]
	if d.Code != "forbidden_call" || d.Line != 4 || !strings.Contains(d.Message, "helper") || !strings.Contains(d.Message, "Entry") {
		t.Fatal(d)
	}
}

func TestContractUnknownDoesNotPass(t *testing.T) {
	c := contractFixture(t, `package app
func Entry(f func()){f()}
`, `"os::Exit"`, "", "error")
	r, err := Run(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if r.ExitCode() != 2 || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "unresolved_call" {
		t.Fatalf("%+v", r)
	}
}

func TestContractUnavailableEntryBody(t *testing.T) {
	c := contractFixture(t, "package app\nfunc Entry()\n", `"os::Exit"`, "", "error")
	write(t, c.Project.Root, "stub.s", "")
	r, err := Run(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if r.ExitCode() != 2 || len(r.Verifications) != 1 || r.Verifications[0].Complete {
		t.Fatalf("%+v", r)
	}
}

func TestContractPanic(t *testing.T) {
	c := contractFixture(t, "package app\nfunc Entry(){panic(\"bad\")}\n", `"builtin::panic"`, "", "error")
	r, err := Run(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if r.ExitCode() != 1 || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "forbidden_call" {
		t.Fatalf("%+v", r)
	}
}

func TestContractResolutionBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, body, trusted, unknown, status, code string
		exit, assumptions                          int
	}{
		{"safe and unreachable", `func Entry(){}; func unused(){panic("bad")}`, "", "error", "satisfied", "", 0, 0},
		{"static alias", `func Entry(){f:=helper; f()}; func helper(){panic("bad")}`, "", "error", "violated", "forbidden_call", 1, 0},
		{"closure", `func Entry(){f:=func(){panic("bad")}; f()}`, "", "error", "violated", "forbidden_call", 1, 0},
		{"defer", `func Entry(){defer helper()}; func helper(){panic("bad")}`, "", "error", "violated", "forbidden_call", 1, 0},
		{"goroutine", `func Entry(){go helper()}; func helper(){panic("bad")}`, "", "error", "violated", "forbidden_call", 1, 0},
		{"recursive", `func Entry(){helper()}; func helper(){helper(); panic("bad")}`, "", "error", "violated", "forbidden_call", 1, 0},
		{"generic", `func Entry(){helper(1)}; func helper[T any](v T){panic(v)}`, "", "error", "violated", "forbidden_call", 1, 0},
		{"method", `type service struct{}; func Entry(){new(service).Run()}; func (*service) Run(){panic("bad")}`, "", "error", "violated", "forbidden_call", 1, 0},
		{"interface trust cannot resolve", `type service interface{Run()}; func Entry(s service){s.Run()}`, `"example.com/contract::*"`, "error", "unknown", "unresolved_call", 2, 0},
		{"warning unknown", `func Entry(f func()){f()}`, "", "warning", "unknown", "unresolved_call", 0, 0},
		{"missing entry", `func Other(){}`, "", "error", "unknown", "entry_not_found", 2, 0},
		{"external unknown", `import "strings"; func Entry(s string)string{return strings.TrimSpace(s)}`, "", "error", "unknown", "unmodeled_call", 2, 0},
		{"external trusted", `import "strings"; func Entry(s string)string{return strings.TrimSpace(s)}`, `"strings::*"`, "error", "satisfied", "", 0, 1},
		{"trust cannot override violation", `func Entry(){panic("bad")}`, `"builtin::*"`, "error", "violated", "forbidden_call", 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := contractFixture(t, "package app\n"+tc.body+"\n", `"builtin::panic"`, tc.trusted, tc.unknown)
			r, err := Run(context.Background(), c)
			if err != nil {
				t.Fatal(err)
			}
			if r.ExitCode() != tc.exit || len(r.Verifications) != 1 {
				t.Fatalf("%+v", r)
			}
			v := r.Verifications[0]
			if v.Status != tc.status || len(v.Assumptions) != tc.assumptions {
				t.Fatalf("%+v", v)
			}
			if tc.code == "" {
				if len(r.Diagnostics) != 0 {
					t.Fatal(r.Diagnostics)
				}
			} else if len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != tc.code {
				t.Fatal(r.Diagnostics)
			}
		})
	}
}

func TestContractCrossPackageAndMethodEntry(t *testing.T) {
	c := contractFixture(t, "package app\nimport \"example.com/contract/service\"\nfunc Entry(){service.Run()}\n", `"builtin::panic"`, "", "error")
	write(t, c.Project.Root, "service/run.go", "package service\nfunc Run(){panic(1)}\ntype Worker struct{}\nfunc (*Worker) Safe(){}\n")
	c.Rules[0].Contract.Entries = append(c.Rules[0].Contract.Entries, "example.com/contract/service::Worker.Safe")
	r, err := Run(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if r.ExitCode() != 1 || len(r.Verifications) != 2 || len(r.Diagnostics) != 1 || r.Diagnostics[0].File != "service/run.go" {
		t.Fatalf("%+v", r)
	}
	if r.Verifications[0].Status != "satisfied" || r.Verifications[1].Status != "violated" || len(r.Verifications[1].Evidence[0].Chain) != 2 {
		t.Fatal(r.Verifications)
	}
}

func TestContractBuildSelection(t *testing.T) {
	c := contractFixture(t, "package app\nfunc Entry(){helper()}\n", `"builtin::panic"`, "", "error")
	write(t, c.Project.Root, "safe.go", "//go:build !danger\n\npackage app\nfunc helper(){}\n")
	write(t, c.Project.Root, "danger.go", "//go:build danger\n\npackage app\nfunc helper(){panic(1)}\n")
	write(t, c.Project.Root, "app_test.go", "package app\nfunc broken(){invalid()}\n")
	for _, tagged := range []bool{false, true} {
		want := 0
		if tagged {
			c.Project.BuildTags = []string{"danger"}
			want = 1
		}
		r, err := Run(context.Background(), c)
		if err != nil || r.ExitCode() != want {
			t.Fatalf("%+v %v", r, err)
		}
	}
	write(t, c.Project.Root, "app.go", "package app\nfunc Entry(){invalid()}\n")
	if _, err := Run(context.Background(), c); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("%v", err)
	}
}
