package config

import "fmt"

// TestChecks selects test sources and duplicate checks.
type TestChecks struct {
	Packages         []string `toml:"packages"`
	IncludeGenerated *bool    `toml:"include_generated"`
	DuplicateBodies  *bool    `toml:"duplicate_bodies"`
	DuplicateCases   *bool    `toml:"duplicate_cases"`
}

func (r Rule) validateTests() error {
	if r.Tests == nil || r.Format != nil || r.Layout != nil || r.Comments != nil || r.Files != nil ||
		r.Layers != nil || r.Unmatched != nil || r.AllowSameLayer != nil ||
		r.AllowStdlib != nil || r.ExternalDefault != nil {
		return fmt.Errorf("test rule requires only a tests table")
	}
	fields := []string{"packages", "include_generated"}
	if r.Kind == "test_duplicates" {
		fields = append(fields, "duplicate_bodies", "duplicate_cases")
	}
	if err := requiredFields(*r.Tests, fields); err != nil {
		return err
	}
	if len(r.Tests.Packages) == 0 {
		return fmt.Errorf("packages must not be empty")
	}
	for _, pattern := range r.Tests.Packages {
		if !validPattern(pattern) {
			return fmt.Errorf("invalid package pattern %q", pattern)
		}
	}
	if r.Kind == "test_duplicates" && !*r.Tests.DuplicateBodies && !*r.Tests.DuplicateCases {
		return fmt.Errorf("enable duplicate_bodies or duplicate_cases")
	}
	return nil
}
