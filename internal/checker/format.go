package checker

import (
	"bytes"
	"fmt"
	"path"
	"strings"

	"github.com/echozyr2001/go-static-checker/internal/config"
)

func checkSourceFormat(r config.Rule, sources []source) ([]Diagnostic, error) {
	f := r.Format
	filenamePattern, err := config.CompileNamePattern(*f.FileNamePattern)
	if err != nil {
		return nil, err
	}
	packagePattern, err := config.CompileNamePattern(*f.PackageNamePattern)
	if err != nil {
		return nil, err
	}
	var diagnostics []Diagnostic
	for _, s := range sources {
		if s.isTest && !f.IncludeTests {
			continue
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(s.pkg.PkgPath, s.pkg.Module.Path), "/")
		if rel == "" {
			rel = "."
		}
		matched := false
		for _, pattern := range f.Packages {
			matched = matched || config.Match(pattern, rel)
		}
		if !matched {
			continue
		}
		add := func(code, message string, line, column int) {
			diagnostics = append(diagnostics, Diagnostic{
				RuleID: r.ID, Code: code, Severity: r.Severity, Message: message,
				File: s.file, Line: line, Column: column, SourcePackage: s.pkg.PkgPath})
		}
		if *f.FileNamePattern != "" && !filenamePattern.MatchString(path.Base(s.file)) {
			add("invalid_file_name",
				fmt.Sprintf("file name %q does not match %q", path.Base(s.file), *f.FileNamePattern), 1, 1)
		}
		if *f.PackageNamePattern != "" && !packagePattern.MatchString(s.packageName) {
			add("invalid_package_name",
				fmt.Sprintf("package name %q does not match %q", s.packageName, *f.PackageNamePattern),
				s.packagePosition.Line, s.packagePosition.Column)
		}
		diagnostics = append(diagnostics, declarationSpacing(r, s)...)
		if *f.MaxLineLength == 0 {
			continue
		}
		for i, line := range bytes.SplitAfter(s.content, []byte("\n")) {
			if bytes.HasSuffix(line, []byte("\n")) {
				line = bytes.TrimSuffix(line, []byte("\n"))
				line = bytes.TrimSuffix(line, []byte("\r"))
			}
			column := firstOverlongColumn(string(line), *f.MaxLineLength, *f.TabWidth)
			if column != 0 {
				add("line_too_long",
					fmt.Sprintf("line exceeds %d characters (tab stops every %d columns)",
						*f.MaxLineLength, *f.TabWidth),
					i+1, column)
			}
		}
	}
	return diagnostics, nil
}

// firstOverlongColumn returns the byte offset of the first excess code point.
// Stopping at the limit avoids overflow with large tab widths.
func firstOverlongColumn(line string, limit, tabWidth int) int {
	width := 0
	for offset, r := range line {
		advance := 1
		if r == '\t' {
			advance = tabWidth - width%tabWidth
		}
		if advance > limit-width {
			return offset + 1
		}
		width += advance
	}
	return 0
}
