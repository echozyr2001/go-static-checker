package config

import (
	"fmt"
	"io/fs"
	"regexp"
	"strings"
)

// SourceFormat requires the original format options; the newer spacing and test
// options are opt-in. Zero/empty values disable the corresponding checks.
type SourceFormat struct {
	Packages           []string `toml:"packages"`
	MaxLineLength      *int     `toml:"max_line_length"`
	TabWidth           *int     `toml:"tab_width"`
	FileNamePattern    *string  `toml:"file_name_pattern"`
	PackageNamePattern *string  `toml:"package_name_pattern"`
	// Omitted values preserve production-only checks without declaration spacing.
	MinBlankLinesBetweenDeclarations int  `toml:"min_blank_lines_between_declarations"`
	IncludeTests                     bool `toml:"include_tests"`
}

type FileLayout struct {
	RequiredFiles       []string `toml:"required_files"`
	RequiredDirectories []string `toml:"required_directories"`
}

// CompileNamePattern matches the whole name, even when the expression has alternatives.
func CompileNamePattern(pattern string) (*regexp.Regexp, error) {
	return regexp.Compile(`\A(?:` + pattern + `)\z`)
}

func (f SourceFormat) validate() error {
	if len(f.Packages) == 0 || f.MaxLineLength == nil || f.TabWidth == nil ||
		f.FileNamePattern == nil || f.PackageNamePattern == nil {
		return fmt.Errorf("format requires packages, max_line_length, tab_width, " +
			"file_name_pattern and package_name_pattern")
	}
	if *f.MaxLineLength < 0 || *f.TabWidth < 1 {
		return fmt.Errorf("max_line_length must be nonnegative and tab_width must be positive")
	}
	if f.MinBlankLinesBetweenDeclarations < 0 {
		return fmt.Errorf("min_blank_lines_between_declarations must be nonnegative")
	}
	if *f.MaxLineLength == 0 && *f.FileNamePattern == "" && *f.PackageNamePattern == "" &&
		f.MinBlankLinesBetweenDeclarations == 0 {
		return fmt.Errorf("format must enable at least one check")
	}
	for _, p := range f.Packages {
		if !validPattern(p) {
			return fmt.Errorf("invalid package pattern %q", p)
		}
	}
	for _, field := range []struct{ name, pattern string }{
		{"file_name_pattern", *f.FileNamePattern},
		{"package_name_pattern", *f.PackageNamePattern},
	} {
		if _, err := CompileNamePattern(field.pattern); err != nil {
			return fmt.Errorf("invalid %s: %w", field.name, err)
		}
	}
	return nil
}

func (l FileLayout) validate() error {
	if l.RequiredFiles == nil || l.RequiredDirectories == nil {
		return fmt.Errorf("layout requires required_files and required_directories")
	}
	if len(l.RequiredFiles)+len(l.RequiredDirectories) == 0 {
		return fmt.Errorf("layout must require at least one file or directory")
	}
	seen := map[string]bool{}
	for _, p := range append(append([]string{}, l.RequiredFiles...), l.RequiredDirectories...) {
		if p == "." || !fs.ValidPath(p) || strings.ContainsAny(p, "\\:*?[]\x00") {
			return fmt.Errorf("invalid project-relative layout path %q", p)
		}
		if seen[p] {
			return fmt.Errorf("duplicate or conflicting layout path %q", p)
		}
		seen[p] = true
	}
	return nil
}
