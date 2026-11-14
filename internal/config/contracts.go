package config

import (
	"fmt"
	"go/token"
	"strings"
)

// CallContract describes a production call-chain restriction entirely in TOML.
type CallContract struct {
	Entries   []string `toml:"entries"`
	Forbidden []string `toml:"forbidden"`
	Trusted   []string `toml:"trusted"`
	OnUnknown *string  `toml:"on_unknown"`
}

func (r Rule) validateContract() error {
	if r.Contract == nil || r.Format != nil || r.Layout != nil || r.Comments != nil || r.Files != nil ||
		r.Layers != nil || r.Unmatched != nil || r.AllowSameLayer != nil ||
		r.AllowStdlib != nil || r.ExternalDefault != nil {
		return fmt.Errorf("call_contract requires only a contract table")
	}
	c := r.Contract
	if c.Entries == nil || c.Forbidden == nil || c.Trusted == nil || c.OnUnknown == nil {
		return fmt.Errorf("contract requires entries, forbidden, trusted and on_unknown")
	}
	if len(c.Entries) == 0 || len(c.Forbidden) == 0 {
		return fmt.Errorf("entries and forbidden must not be empty")
	}
	if *c.OnUnknown != "error" && *c.OnUnknown != "warning" {
		return fmt.Errorf("on_unknown must be error or warning")
	}
	for _, symbols := range [][]string{c.Entries, c.Forbidden, c.Trusted} {
		seen := map[string]bool{}
		for _, symbol := range symbols {
			pkg, name, ok := strings.Cut(symbol, "::")
			valid := ok && pkg != "." && pkg != "..." && !strings.HasSuffix(pkg, "/...") && validPattern(pkg)
			if name != "*" {
				parts := strings.Split(name, ".")
				valid = valid && len(parts) <= 2
				for _, part := range parts {
					valid = valid && token.IsIdentifier(part)
				}
			}
			if !valid || seen[symbol] {
				return fmt.Errorf("invalid or duplicate contract symbol %q", symbol)
			}
			seen[symbol] = true
		}
	}
	return nil
}
