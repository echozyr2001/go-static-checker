package analysis

import (
	"reflect"
	"testing"
)

func TestEvaluateEvidenceAndAssumptions(t *testing.T) {
	p := Program{Functions: map[string]Function{
		"entry":  {ID: "entry", Symbol: "app::Entry", Entry: true, Calls: []Call{{Target: "helper", Symbol: "app::helper"}, {Symbol: "ext::Safe"}}},
		"helper": {ID: "helper", Symbol: "app::helper", Calls: []Call{{Target: "entry", Symbol: "app::Entry"}, {Symbol: "os::Exit", Location: Location{File: "helper.go", Line: 8}}, {Unknown: "dynamic dispatch"}}},
	}}
	policy := Policy{RuleID: "C", Entries: []string{"app::*", "app::Entry"}, Forbidden: []string{"os::Exit"}, Trusted: []string{"ext::*", "os::*"}, OnUnknown: "error"}
	results := Evaluate(p, policy)
	if len(results) != 1 {
		t.Fatal(results)
	}
	r := results[0]
	if r.Status != "violated" || r.Complete || len(r.Assumptions) != 1 || r.Assumptions[0].Symbol != "ext::Safe" || len(r.Evidence) != 2 {
		t.Fatalf("%+v", r)
	}
	if r.Evidence[0].Code != "forbidden_call" || len(r.Evidence[0].Chain) != 2 || r.Evidence[0].Chain[1].Location.Line != 8 || r.Evidence[1].Code != "unresolved_call" {
		t.Fatal(r.Evidence)
	}
	if !reflect.DeepEqual(results, Evaluate(p, policy)) {
		t.Fatal("nondeterministic results")
	}
}

func TestUnmatchedSelectorRemainsUnknown(t *testing.T) {
	p := Program{Functions: map[string]Function{"entry": {Symbol: "app::Entry", Entry: true}}}
	results := Evaluate(p, Policy{Entries: []string{"app::Entry", "missing::*"}})
	if len(results) != 2 || results[0].Status != "satisfied" || results[1].Status != "unknown" || results[1].Complete || results[1].Evidence[0].Code != "entry_not_found" {
		t.Fatal(results)
	}
}
