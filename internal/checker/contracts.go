package checker

import (
	"strings"

	"github.com/echozyr2001/go-static-checker/internal/analysis"
	"github.com/echozyr2001/go-static-checker/internal/config"
)

func checkCallContract(r config.Rule, program analysis.Program) ([]analysis.Result, []Diagnostic) {
	c := r.Contract
	results := analysis.Evaluate(program, analysis.Policy{RuleID: r.ID, Entries: c.Entries,
		Forbidden: c.Forbidden, Trusted: c.Trusted, OnUnknown: *c.OnUnknown})
	var diagnostics []Diagnostic
	for _, result := range results {
		for _, evidence := range result.Evidence {
			severity := r.Severity
			if evidence.Code != "forbidden_call" {
				severity = *c.OnUnknown
			}
			location := result.Location
			chain := []string{result.Entry}
			target := ""
			for _, call := range evidence.Chain {
				name := call.Symbol
				if name == "" {
					name = "<dynamic call>"
				}
				chain = append(chain, name)
				location = call.Location
				target = call.Symbol
			}
			sourcePackage, _, _ := strings.Cut(result.Entry, "::")
			targetPackage, _, _ := strings.Cut(target, "::")
			diagnostics = append(diagnostics, Diagnostic{RuleID: r.ID, Code: evidence.Code, Severity: severity,
				Message: evidence.Reason + ": " + strings.Join(chain, " -> "), File: location.File, Line: location.Line,
				Column: location.Column, SourcePackage: sourcePackage, TargetPackage: targetPackage})
		}
	}
	return results, diagnostics
}
