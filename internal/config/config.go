// Package config decodes and validates version 1 rule files.
package config

import (
	"fmt"
	"path"
	"strings"
)

// Config holds project settings and validated rule definitions.
type Config struct {
	SchemaVersion int     `toml:"schema_version"`
	Project       Project `toml:"project"`
	Rules         []Rule  `toml:"rules"`
}

// Project selects the module or workspace root and build conditions.
type Project struct {
	Root         string   `toml:"root"`
	IncludeTests *bool    `toml:"include_tests"`
	BuildTags    []string `toml:"build_tags"`
}

// Rule defines one check and its severity.
type Rule struct {
	ID              string        `toml:"id"`
	Kind            string        `toml:"kind"`
	Description     *string       `toml:"description"`
	Severity        string        `toml:"severity"`
	Unmatched       *string       `toml:"unmatched"`
	AllowSameLayer  *bool         `toml:"allow_same_layer"`
	AllowStdlib     *bool         `toml:"allow_stdlib"`
	ExternalDefault *string       `toml:"external_default"`
	Layers          []Layer       `toml:"layers"`
	Format          *SourceFormat `toml:"format"`
	Layout          *FileLayout   `toml:"layout"`
	Comments        *Comments     `toml:"comments"`
	Files           *FilePolicy   `toml:"files"`
}

// Layer assigns packages and declares their allowed dependencies.
type Layer struct {
	Name          string   `toml:"name"`
	Packages      []string `toml:"packages"`
	Allow         []string `toml:"allow"`
	AllowExternal []string `toml:"allow_external"`
}

// Validate rejects incomplete or unsupported rule definitions.
func (c Config) Validate() error {
	if c.SchemaVersion != 1 {
		return fmt.Errorf("schema_version must be 1")
	}
	if c.Project.Root == "" || c.Project.IncludeTests == nil || c.Project.BuildTags == nil {
		return fmt.Errorf("project requires root, include_tests and build_tags")
	}
	if *c.Project.IncludeTests {
		return fmt.Errorf("include_tests=true is not supported in schema version 1")
	}
	if len(c.Rules) == 0 {
		return fmt.Errorf("at least one rule is required")
	}
	ids := map[string]bool{}
	for _, r := range c.Rules {
		if strings.TrimSpace(r.ID) == "" || ids[r.ID] {
			return fmt.Errorf("empty or duplicate rule id %q", r.ID)
		}
		ids[r.ID] = true
		if r.Description == nil || (r.Severity != "error" && r.Severity != "warning") {
			return fmt.Errorf("rule %s: missing or invalid behavior fields", r.ID)
		}
		switch r.Kind {
		case "comment_budget", "required_comments", "comment_format", "comment_line_length", "file_policy":
			if r.Format != nil || r.Layout != nil || r.Layers != nil || r.Unmatched != nil ||
				r.AllowSameLayer != nil || r.AllowStdlib != nil || r.ExternalDefault != nil {
				return fmt.Errorf("rule %s: unrelated fields are not allowed", r.ID)
			}
			if r.Kind == "file_policy" {
				if r.Files == nil || r.Comments != nil {
					return fmt.Errorf("rule %s requires only files", r.ID)
				}
				if err := r.Files.validate(); err != nil {
					return fmt.Errorf("rule %s: %w", r.ID, err)
				}
			} else {
				if r.Comments == nil || r.Files != nil {
					return fmt.Errorf("rule %s requires only comments", r.ID)
				}
				if err := r.Comments.validate(r.Kind); err != nil {
					return fmt.Errorf("rule %s: %w", r.ID, err)
				}
			}
			continue
		}
		if r.Comments != nil || r.Files != nil {
			return fmt.Errorf("rule %s: comments/files not allowed", r.ID)
		}
		switch r.Kind {
		case "source_format", "file_layout":
			if r.Unmatched != nil || r.AllowSameLayer != nil || r.AllowStdlib != nil ||
				r.ExternalDefault != nil || r.Layers != nil {
				return fmt.Errorf("rule %s: layer dependency fields are not allowed for %s", r.ID, r.Kind)
			}
			if r.Kind == "source_format" {
				if r.Format == nil || r.Layout != nil {
					return fmt.Errorf("rule %s: source_format requires only a format table", r.ID)
				}
				if err := r.Format.validate(); err != nil {
					return fmt.Errorf("rule %s: %w", r.ID, err)
				}
			} else {
				if r.Layout == nil || r.Format != nil {
					return fmt.Errorf("rule %s: file_layout requires only a layout table", r.ID)
				}
				if err := r.Layout.validate(); err != nil {
					return fmt.Errorf("rule %s: %w", r.ID, err)
				}
			}
			continue
		case "layer_dependencies":
			if r.Format != nil || r.Layout != nil {
				return fmt.Errorf("rule %s: format and layout tables are not allowed for layer_dependencies", r.ID)
			}
		default:
			return fmt.Errorf("rule %s: unknown kind %q", r.ID, r.Kind)
		}
		if r.Unmatched == nil || *r.Unmatched != "error" || r.AllowSameLayer == nil || r.AllowStdlib == nil ||
			r.ExternalDefault == nil || (*r.ExternalDefault != "allow" && *r.ExternalDefault != "deny") {
			return fmt.Errorf("rule %s: missing or invalid behavior fields", r.ID)
		}
		if len(r.Layers) == 0 {
			return fmt.Errorf("rule %s: layers required", r.ID)
		}
		names := map[string]bool{}
		for _, l := range r.Layers {
			if strings.TrimSpace(l.Name) == "" || names[l.Name] {
				return fmt.Errorf("rule %s: empty or duplicate layer %q", r.ID, l.Name)
			}
			names[l.Name] = true
			if len(l.Packages) == 0 || l.Allow == nil || l.AllowExternal == nil {
				return fmt.Errorf("layer %s: packages, allow and allow_external required", l.Name)
			}
			for _, p := range append(append([]string{}, l.Packages...), l.AllowExternal...) {
				if !validPattern(p) {
					return fmt.Errorf("layer %s: invalid pattern %q", l.Name, p)
				}
			}
		}
		for _, l := range r.Layers {
			for _, name := range l.Allow {
				if !names[name] {
					return fmt.Errorf("layer %s references unknown layer %s", l.Name, name)
				}
			}
		}
	}
	return nil
}

func validPattern(p string) bool {
	if p == "." || p == "..." {
		return true
	}
	p = strings.TrimSuffix(p, "/...")
	return p != "" && !strings.ContainsAny(p, "*?\\: \t\n[]") && !strings.Contains(p, "...") &&
		!strings.HasPrefix(p, "/") && path.Clean(p) == p && p != ".." && !strings.HasPrefix(p, "../")
}

// Match uses path segments; a recursive pattern also matches its base package.
func Match(pattern, value string) bool {
	if pattern == "..." {
		return true
	}
	if strings.HasSuffix(pattern, "/...") {
		base := strings.TrimSuffix(pattern, "/...")
		return value == base || strings.HasPrefix(value, base+"/")
	}
	return pattern == value
}
