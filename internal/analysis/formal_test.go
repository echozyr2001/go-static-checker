package analysis

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func formalFixture(violation bool) (Program, Policy) {
	calls := []Call{{Target: "entry", Symbol: "app::Entry"}, {Symbol: "strings::TrimSpace"}}
	if violation {
		calls = append(calls, Call{Symbol: "os::Exit", Location: Location{File: "helper.go", Line: 7}})
	}
	p := Program{Functions: map[string]Function{
		"entry":       {Symbol: "app::Entry", Entry: true, Calls: []Call{{Target: "helper", Symbol: "app::helper"}}},
		"helper":      {Symbol: "app::helper", Calls: calls},
		"unreachable": {Symbol: "app::Unused", Calls: []Call{{Symbol: "os::Exit"}}},
	}}
	return p, Policy{RuleID: "C", Entries: []string{"app::Entry"}, Forbidden: []string{"os::Exit"}, Trusted: []string{"strings::*"}, OnUnknown: "error"}
}

func TestFormalGraph(t *testing.T) {
	p, policy := formalFixture(true)
	models, err := FormalModels(p, policy)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 {
		t.Fatal(models)
	}
	m := models[0]
	if m.Result.Status != "violated" || len(m.Nodes) != 3 || !m.Nodes[2].Forbidden || m.Nodes[2].Location.Line != 7 {
		t.Fatalf("%+v", m)
	}
	if len(m.Edges) != 3 || m.Edges[0].From != 0 || m.Edges[0].To != 1 || m.Edges[1].To != 0 || m.Edges[2].To != 2 {
		t.Fatal(m.Edges)
	}
	if len(m.Result.Assumptions) != 1 || m.Result.Assumptions[0].Symbol != "strings::TrimSpace" {
		t.Fatal(m.Result)
	}
	again, err := FormalModels(p, policy)
	if err != nil || !reflect.DeepEqual(models, again) {
		t.Fatal("unstable model", err)
	}
}

func TestFormalRefusesIncomplete(t *testing.T) {
	p, policy := formalFixture(false)
	for _, kind := range []string{"dynamic", "external", "missing entry"} {
		t.Run(kind, func(t *testing.T) {
			fn := p.Functions["helper"]
			switch kind {
			case "dynamic":
				fn.Calls = []Call{{Unknown: "dynamic dispatch"}}
			case "external":
				fn.Calls = []Call{{Symbol: "external::Do"}}
			case "missing entry":
				policy.Entries = []string{"app::Missing"}
			}
			p.Functions["helper"] = fn
			policy.OnUnknown = "warning"
			if _, err := FormalModels(p, policy); err == nil || !strings.Contains(err.Error(), "incomplete") {
				t.Fatal(err)
			}
		})
	}
}

func TestFormalBackends(t *testing.T) {
	lean, jar := os.Getenv("GSC_LEAN"), os.Getenv("GSC_TLC_JAR")
	if lean == "" && jar == "" {
		t.Skip("set GSC_LEAN and GSC_TLC_JAR to run real formal backends")
	}
	if lean == "" || jar == "" {
		t.Fatal("both formal backends must be configured")
	}
	for _, violation := range []bool{false, true} {
		name := "safe"
		if violation {
			name = "forbidden"
		}
		t.Run(name, func(t *testing.T) {
			p, policy := formalFixture(violation)
			models, err := FormalModels(p, policy)
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			for file, content := range map[string]string{
				"Model.tla": models[0].TLA(), "Model.lean": models[0].Lean(),
				"Model.cfg": "SPECIFICATION Spec\nINVARIANT Safe\nCHECK_DEADLOCK FALSE\n",
			} {
				if err := os.WriteFile(filepath.Join(root, file), []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			for _, backend := range []struct {
				name string
				args []string
			}{
				{"java", []string{"-XX:+UseParallelGC", "-cp", jar, "tlc2.TLC", "-workers", "1", "-config", "Model.cfg", "Model.tla"}},
				{lean, []string{"Model.lean"}},
			} {
				ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				cmd := exec.CommandContext(ctx, backend.name, backend.args...)
				cmd.Dir = root
				out, err := cmd.CombinedOutput()
				timedOut := ctx.Err() != nil
				cancel()
				if timedOut {
					t.Fatalf("backend timeout: %s\n%s", backend.name, out)
				}
				if violation {
					marker := "Invariant Safe is violated"
					if backend.name == lean {
						marker = "decide"
					}
					if err == nil || !strings.Contains(string(out), marker) {
						t.Fatalf("expected safety failure: %s %v\n%s", backend.name, err, out)
					}
				} else {
					if err != nil {
						t.Fatalf("backend %s: %v\n%s", backend.name, err, out)
					}
					marker := "No error has been found"
					if backend.name == lean {
						marker = "Contract.reachable_safe"
					}
					if !strings.Contains(string(out), marker) {
						t.Fatalf("missing verification evidence: %s", out)
					}
					if strings.Contains(string(out), "sorryAx") {
						t.Fatalf("unproved theorem: %s", out)
					}
				}
				t.Logf("%s: %s", backend.name, out)
			}
		})
	}
}
