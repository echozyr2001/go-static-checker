package analysis

import (
	"fmt"
	"sort"
	"strings"
)

// FormalNode binds a model state to a function or prohibited call.
type FormalNode struct {
	Symbol    string   `json:"symbol"`
	Location  Location `json:"location"`
	Forbidden bool     `json:"forbidden"`
}

// FormalEdge records one possible call-chain transition.
type FormalEdge struct {
	From     int      `json:"from"`
	To       int      `json:"to"`
	Location Location `json:"location"`
}

// FormalModel exports a call-reachability abstraction, not execution order.
type FormalModel struct {
	Policy Policy       `json:"policy"`
	Result Result       `json:"result"`
	Nodes  []FormalNode `json:"nodes"`
	Edges  []FormalEdge `json:"edges"`
}

// FormalModels builds complete finite models and refuses unresolved contracts.
func FormalModels(program Program, policy Policy) ([]FormalModel, error) {
	results := Evaluate(program, policy)
	for _, result := range results {
		if !result.Complete {
			return nil, fmt.Errorf("cannot export incomplete contract %s: %s", policy.RuleID, result.Entry)
		}
	}
	var ids []string
	for id, fn := range program.Functions {
		if fn.Entry && anyMatch(policy.Entries, fn.Symbol) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	var models []FormalModel
	for _, id := range ids {
		result := evaluateEntry(program, policy, id)
		model := FormalModel{Policy: policy, Result: result, Edges: []FormalEdge{}}
		queue := []string{id}
		indices := map[string]int{id: 0}
		entry := program.Functions[id]
		model.Nodes = append(model.Nodes, FormalNode{Symbol: entry.Symbol, Location: entry.Location})
		for cursor := 0; cursor < len(queue); cursor++ {
			current := queue[cursor]
			from := indices[current]
			for _, call := range program.Functions[current].Calls {
				to := -1
				if anyMatch(policy.Forbidden, call.Symbol) {
					to = len(model.Nodes)
					model.Nodes = append(model.Nodes,
						FormalNode{Symbol: call.Symbol, Location: call.Location, Forbidden: true})
				} else if target, ok := program.Functions[call.Target]; ok {
					var exists bool
					to, exists = indices[call.Target]
					if !exists {
						to = len(model.Nodes)
						indices[call.Target] = to
						model.Nodes = append(model.Nodes, FormalNode{Symbol: target.Symbol, Location: target.Location})
						queue = append(queue, call.Target)
					}
				}
				if to >= 0 {
					model.Edges = append(model.Edges, FormalEdge{From: from, To: to, Location: call.Location})
				}
			}
		}
		if len(model.Nodes) > 128 {
			return nil, fmt.Errorf("formal prototype supports at most 128 states per entry: %s", entry.Symbol)
		}
		models = append(models, model)
	}
	return models, nil
}

// TLA emits a finite graph with an invariant excluding forbidden states.
func (m FormalModel) TLA() string {
	var edges, bad []string
	for _, e := range m.Edges {
		edges = append(edges, fmt.Sprintf("<<%d, %d>>", e.From, e.To))
	}
	for i, n := range m.Nodes {
		if n.Forbidden {
			bad = append(bad, fmt.Sprint(i))
		}
	}
	return fmt.Sprintf(`---- MODULE Model ----
EXTENDS Naturals
VARIABLE pc
Nodes == 0..%d
Edges == {%s}
Bad == {%s}
Init == pc = 0
Next == \E next \in Nodes : <<pc, next>> \in Edges /\ pc' = next
Spec == Init /\ [][Next]_pc
Safe == pc \notin Bad
====
`, len(m.Nodes)-1, strings.Join(edges, ", "), strings.Join(bad, ", "))
}

// Lean emits a kernel-checkable invariant proof without sorry or native_decide.
func (m FormalModel) Lean() string {
	edges := []string{"a == b"}
	bad := []string{"true"}
	for _, e := range m.Edges {
		edges = append(edges, fmt.Sprintf("(a.val == %d && b.val == %d)", e.From, e.To))
	}
	for i, n := range m.Nodes {
		if n.Forbidden {
			bad = append(bad, fmt.Sprintf("s.val != %d", i))
		}
	}
	return fmt.Sprintf(`import Std
set_option maxRecDepth 4096
set_option maxHeartbeats 2000000
namespace Contract
abbrev State := Fin %d
def initial : State := ⟨0, by decide⟩
def safe (s : State) : Bool := %s
def step (a b : State) : Bool := %s
inductive Reachable : State → Prop where
  | initial : Reachable initial
  | next {a b : State} : Reachable a → step a b = true → Reachable b
 theorem initial_safe : safe initial = true := by decide
 theorem step_safe : ∀ a b : State, safe a = true → step a b = true → safe b = true := by decide
 theorem reachable_safe {s : State} (h : Reachable s) : safe s = true := by
  induction h with
  | initial => exact initial_safe
  | next _ edge ih => exact step_safe _ _ ih edge
#print axioms reachable_safe
end Contract
`, len(m.Nodes), strings.Join(bad, " && "), strings.Join(edges, " || "))
}
