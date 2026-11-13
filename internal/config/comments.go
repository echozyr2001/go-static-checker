package config

import (
	"fmt"
	"reflect"
	"slices"
)

// Comments configures comment selection, budgets and formatting.
type Comments struct {
	Packages                  []string         `toml:"packages"`
	Declarations              []string         `toml:"declarations"`
	Visibility                *string          `toml:"visibility"`
	IncludeTests              *bool            `toml:"include_tests"`
	IncludeGenerated          *bool            `toml:"include_generated"`
	ExcludeGoDirectives       *bool            `toml:"exclude_go_directives"`
	CountUnit                 *string          `toml:"count_unit"`
	Denominator               *string          `toml:"denominator"`
	ExcludeDelimiterOnlyLines []string         `toml:"exclude_delimiter_only_lines"`
	MaxCommentPercent         *int             `toml:"max_comment_percent"`
	Rounding                  *string          `toml:"rounding"`
	MinAllowedCommentLines    *int             `toml:"min_allowed_comment_lines"`
	Position                  *string          `toml:"position"`
	RequireNonempty           *bool            `toml:"require_nonempty"`
	Doc                       *DocComment      `toml:"doc"`
	Body                      *BodyComment     `toml:"body"`
	Trailing                  *TrailingComment `toml:"trailing"`
	Positions                 []string         `toml:"positions"`
	MaxColumn                 *int             `toml:"max_column"`
	ColumnMetric              *string          `toml:"column_metric"`
	TabWidth                  *int             `toml:"tab_width"`
}

// DocComment specifies declaration documentation syntax.
type DocComment struct {
	Style                *string `toml:"style"`
	RequireNamePrefix    *bool   `toml:"require_name_prefix"`
	RequireTextAfterName *bool   `toml:"require_text_after_name"`
}

// BodyComment specifies standalone comment syntax inside declarations.
type BodyComment struct {
	Style       *string `toml:"style"`
	LinePattern *string `toml:"line_pattern"`
}

// TrailingComment controls comments on the same line as preceding code.
type TrailingComment struct {
	Allowed *bool `toml:"allowed"`
}

func requiredFields(value any, allowed []string) error {
	v := reflect.ValueOf(value)
	typ := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		key := typ.Field(i).Tag.Get("toml")
		if slices.Contains(allowed, key) {
			if field.IsNil() {
				return fmt.Errorf("%s is required", key)
			}
		} else if !field.IsNil() {
			return fmt.Errorf("%s is not allowed for this rule kind", key)
		}
	}
	return nil
}

func (c Comments) validate(kind string) error {
	fields := []string{"packages", "include_tests", "include_generated", "exclude_go_directives"}
	if kind != "comment_line_length" {
		fields = append(fields, "declarations", "visibility")
	}
	switch kind {
	case "comment_budget":
		fields = append(fields, "count_unit", "denominator", "exclude_delimiter_only_lines",
			"max_comment_percent", "rounding", "min_allowed_comment_lines")
	case "required_comments":
		fields = append(fields, "position", "require_nonempty")
	case "comment_format":
		fields = append(fields, "doc", "body", "trailing")
	case "comment_line_length":
		fields = append(fields, "positions", "max_column", "column_metric", "tab_width")
	}
	if err := requiredFields(c, fields); err != nil {
		return err
	}
	if len(c.Packages) == 0 {
		return fmt.Errorf("packages must not be empty")
	}
	for _, p := range c.Packages {
		if !validPattern(p) {
			return fmt.Errorf("invalid package pattern %q", p)
		}
	}
	if kind != "comment_line_length" {
		kinds := []string{"function", "method", "struct", "interface", "package"}
		if err := oneOfList(c.Declarations, kinds); err != nil {
			return fmt.Errorf("declarations: %w", err)
		}
		if !slices.Contains([]string{"all", "exported", "unexported"}, *c.Visibility) {
			return fmt.Errorf("invalid visibility %q", *c.Visibility)
		}
		if slices.Contains(c.Declarations, "package") && (kind != "comment_budget" || *c.Visibility != "all") {
			return fmt.Errorf("package selection requires comment_budget with visibility=all")
		}
	}
	switch kind {
	case "comment_budget":
		if *c.CountUnit != "physical_lines" || *c.Denominator != "comment_and_code_lines" || *c.Rounding != "ceil" {
			return fmt.Errorf("budget requires physical_lines, comment_and_code_lines and ceil")
		}
		if *c.MaxCommentPercent < 0 || *c.MaxCommentPercent > 100 || *c.MinAllowedCommentLines < 0 {
			return fmt.Errorf("percent must be 0..100 and min_allowed_comment_lines must be nonnegative")
		}
		for _, p := range c.ExcludeDelimiterOnlyLines {
			if !slices.Contains([]string{"{", "}", "(", ")", ";", "[", "]", ","}, p) {
				return fmt.Errorf("unsupported delimiter %q", p)
			}
		}
	case "required_comments":
		if *c.Position != "doc" {
			return fmt.Errorf("required_comments supports position=doc only")
		}
	case "comment_format":
		docFields := []string{"style", "require_name_prefix", "require_text_after_name"}
		if err := requiredFields(*c.Doc, docFields); err != nil {
			return fmt.Errorf("doc: %w", err)
		}
		if err := requiredFields(*c.Body, []string{"style", "line_pattern"}); err != nil {
			return fmt.Errorf("body: %w", err)
		}
		if c.Trailing.Allowed == nil {
			return fmt.Errorf("trailing.allowed is required")
		}
		if *c.Doc.Style != "line" || *c.Body.Style != "line" {
			return fmt.Errorf("comment style must be line")
		}
		if *c.Doc.RequireTextAfterName && !*c.Doc.RequireNamePrefix {
			return fmt.Errorf("require_text_after_name requires require_name_prefix")
		}
		if _, err := CompileNamePattern(*c.Body.LinePattern); err != nil {
			return fmt.Errorf("line_pattern: %w", err)
		}
	case "comment_line_length":
		if err := oneOfList(c.Positions, []string{"doc", "body", "trailing", "file"}); err != nil {
			return fmt.Errorf("positions: %w", err)
		}
		if *c.MaxColumn < 1 || *c.TabWidth < 1 || *c.ColumnMetric != "unicode_codepoints" {
			return fmt.Errorf("line length requires positive max_column/tab_width and unicode_codepoints")
		}
	}
	return nil
}

func oneOfList(values, allowed []string) error {
	if len(values) == 0 {
		return fmt.Errorf("list must not be empty")
	}
	seen := map[string]bool{}
	for _, v := range values {
		if !slices.Contains(allowed, v) || seen[v] {
			return fmt.Errorf("invalid or duplicate value %q", v)
		}
		seen[v] = true
	}
	return nil
}
