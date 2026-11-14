package analysis

import (
	"sort"
	"strings"
)

func matches(pattern, symbol string) bool {
	if strings.HasSuffix(pattern, "::*") {
		return strings.HasPrefix(symbol, strings.TrimSuffix(pattern, "*"))
	}
	return pattern == symbol
}

func anyMatch(patterns []string, symbol string) bool {
	for _, pattern := range patterns {
		if matches(pattern, symbol) {
			return true
		}
	}
	return false
}

// Evaluate checks static reachability without assuming unknown edges are safe.
func Evaluate(program Program, policy Policy) []Result {
	var ids []string
	for id, function := range program.Functions {
		if function.Entry {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	selected := map[string]bool{}
	var results []Result
	for _, entry := range policy.Entries {
		found := false
		for _, id := range ids {
			if !matches(entry, program.Functions[id].Symbol) {
				continue
			}
			found = true
			if selected[id] {
				continue
			}
			selected[id] = true
			results = append(results, evaluateEntry(program, policy, id))
		}
		if !found {
			results = append(results, Result{RuleID: policy.RuleID, Entry: entry,
				Status: "unknown", OnUnknown: policy.OnUnknown, Assumptions: []Assumption{},
				Evidence: []Evidence{{Code: "entry_not_found", Reason: "entry matched no loaded function",
					Chain: []Call{}}}})
		}
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Entry < results[j].Entry })
	return results
}

func evaluateEntry(program Program, policy Policy, id string) Result {
	entry := program.Functions[id]
	result := Result{RuleID: policy.RuleID, Entry: entry.Symbol, Status: "satisfied", Complete: true,
		OnUnknown: policy.OnUnknown, Location: entry.Location, Assumptions: []Assumption{}, Evidence: []Evidence{}}
	type item struct {
		id    string
		chain []Call
	}
	queue := []item{{id: id}}
	visited := map[string]bool{id: true}
	assumptions := map[string]bool{}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, call := range program.Functions[current.id].Calls {
			chain := append(append([]Call{}, current.chain...), call)
			if call.Symbol != "" && anyMatch(policy.Forbidden, call.Symbol) {
				result.Status = "violated"
				result.Evidence = append(result.Evidence, Evidence{Code: "forbidden_call",
					Reason: "forbidden API is statically reachable", Chain: chain})
				continue
			}
			if call.Unknown != "" {
				result.Complete = false
				result.Evidence = append(result.Evidence, Evidence{Code: "unresolved_call",
					Reason: call.Unknown, Chain: chain})
				continue
			}
			if _, ok := program.Functions[call.Target]; ok {
				if !visited[call.Target] {
					visited[call.Target] = true
					queue = append(queue, item{call.Target, chain})
				}
				continue
			}
			if call.Primitive {
				continue
			}
			if anyMatch(policy.Trusted, call.Symbol) {
				if !assumptions[call.Symbol] {
					assumptions[call.Symbol] = true
					result.Assumptions = append(result.Assumptions,
						Assumption{Symbol: call.Symbol, Location: call.Location})
				}
				continue
			}
			result.Complete = false
			result.Evidence = append(result.Evidence, Evidence{Code: "unmodeled_call",
				Reason: "callee body is unavailable and not trusted", Chain: chain})
		}
	}
	if result.Status != "violated" && !result.Complete {
		result.Status = "unknown"
	}
	sort.Slice(result.Assumptions, func(i, j int) bool {
		return result.Assumptions[i].Symbol < result.Assumptions[j].Symbol
	})
	return result
}
