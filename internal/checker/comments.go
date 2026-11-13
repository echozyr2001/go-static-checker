package checker

import (
	"bytes"
	"fmt"
	"go/ast"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/echozyr2001/go-static-checker/internal/config"
)

func commentSelected(c *config.Comments, s source) bool {
	if s.isTest && !*c.IncludeTests || ast.IsGenerated(s.node) && !*c.IncludeGenerated {
		return false
	}
	rel := strings.TrimPrefix(strings.TrimPrefix(s.pkg.PkgPath, s.pkg.Module.Path), "/")
	if rel == "" {
		rel = "."
	}
	for _, pattern := range c.Packages {
		if config.Match(pattern, rel) {
			return true
		}
	}
	return false
}

func blockSelected(c *config.Comments, b commentBlock) bool {
	if !slices.Contains(c.Declarations, b.kind) {
		return false
	}
	return *c.Visibility == "all" || (*c.Visibility == "exported") == ast.IsExported(b.name)
}

func commentDiagnostic(r config.Rule, s source, code, message string, line, column int) Diagnostic {
	return Diagnostic{RuleID: r.ID, Code: code, Severity: r.Severity, Message: message,
		File: s.file, Line: line, Column: column, SourcePackage: s.pkg.PkgPath}
}

func checkComments(r config.Rule, sources []source) ([]Diagnostic, error) {
	var diagnostics []Diagnostic
	type totals struct {
		comments, code int
		sample         source
	}
	packages := map[string]*totals{}
	for _, s := range sources {
		if !commentSelected(r.Comments, s) {
			continue
		}
		a, err := analyzeComments(s)
		if err != nil {
			return nil, err
		}
		if r.Kind == "comment_line_length" {
			diagnostics = append(diagnostics, commentLineLength(r, a)...)
			continue
		}
		if r.Kind == "comment_budget" && slices.Contains(r.Comments.Declarations, "package") {
			key := s.pkg.PkgPath
			if s.isTest && s.packageName != s.pkg.Name {
				key += "_test"
			}
			total := packages[key]
			if total == nil {
				total = &totals{sample: s}
				packages[key] = total
			}
			if s.file < total.sample.file {
				total.sample = s
			}
			comments, code := countCommentLines(r.Comments, a, 0, len(s.content), nil)
			total.comments += comments
			total.code += code
		}
		for _, b := range a.blocks {
			if !blockSelected(r.Comments, b) {
				continue
			}
			pos := s.fset.PositionFor(b.pos, false)
			switch r.Kind {
			case "comment_budget":
				comments, code := countCommentLines(r.Comments, a, b.start, b.end, b.doc)
				if message := budgetViolation(r.Comments, b.kind+" "+b.name, comments, code); message != "" {
					diagnostics = append(diagnostics, commentDiagnostic(r, s,
						"comment_budget_exceeded", message, pos.Line, pos.Column))
				}
			case "required_comments":
				docs := blockDocParts(r.Comments, a, b)
				text := docText(docs)
				if len(docs) == 0 || *r.Comments.RequireNonempty && strings.TrimSpace(text) == "" {
					diagnostics = append(diagnostics, commentDiagnostic(r, s, "missing_required_comment",
						fmt.Sprintf("%s %s requires a doc comment", b.kind, b.name), pos.Line, pos.Column))
				}
			case "comment_format":
				findings, err := commentFormat(r, a, b)
				if err != nil {
					return nil, err
				}
				diagnostics = append(diagnostics, findings...)
			}
		}
	}
	keys := make([]string, 0, len(packages))
	for key := range packages {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		total := packages[key]
		if message := budgetViolation(r.Comments, "package "+key, total.comments, total.code); message != "" {
			d := commentDiagnostic(r, total.sample, "comment_budget_exceeded", message, 0, 0)
			d.File = ""
			d.SourcePackage = key
			diagnostics = append(diagnostics, d)
		}
	}
	return diagnostics, nil
}

func countCommentLines(c *config.Comments, a commentSource, start, end int, doc *ast.CommentGroup) (int, int) {
	commentLines, codeLines := map[int]bool{}, map[int]bool{}
	for _, part := range a.comments {
		if part.directive && *c.ExcludeGoDirectives {
			continue
		}
		if part.start >= start && part.end <= end || inDoc(a.source, doc, part) {
			for line := part.first; line <= part.last; line++ {
				commentLines[line] = true
			}
		}
	}
	for _, part := range a.code {
		if part.end <= start || part.start >= end || slices.Contains(c.ExcludeDelimiterOnlyLines, part.delimiter) {
			continue
		}
		lo, hi := max(start, part.start), min(end, part.end)
		file := a.source.fset.File(a.source.node.Pos())
		first := file.PositionFor(file.Pos(lo), false).Line
		last := file.PositionFor(file.Pos(hi-1), false).Line
		for line := first; line <= last; line++ {
			from, to := max(lo, a.starts[line-1]), min(hi, a.starts[line-1]+len(a.lines[line-1]))
			if len(bytes.TrimSpace(a.source.content[from:to])) > 0 {
				codeLines[line] = true
			}
		}
	}
	return len(commentLines), len(codeLines)
}

func budgetViolation(c *config.Comments, name string, comments, code int) string {
	total := comments + code
	percent := *c.MaxCommentPercent
	allowed := total/100*percent + (total%100*percent+99)/100
	allowed = max(allowed, *c.MinAllowedCommentLines)
	if comments <= allowed {
		return ""
	}
	return fmt.Sprintf("%s has %d comment lines and %d code lines; allowed %d comment lines",
		name, comments, code, allowed)
}

func blockDocParts(c *config.Comments, a commentSource, b commentBlock) []commentPart {
	var parts []commentPart
	for _, part := range a.comments {
		if part.directive && *c.ExcludeGoDirectives {
			continue
		}
		if inDoc(a.source, b.doc, part) {
			parts = append(parts, part)
		}
	}
	return parts
}

func docText(parts []commentPart) string {
	var text []string
	for _, p := range parts {
		value := p.raw[2:]
		if strings.HasPrefix(p.raw, "/*") {
			value = strings.TrimSuffix(value, "*/")
		}
		text = append(text, strings.TrimPrefix(value, " "))
	}
	return strings.Join(text, "\n")
}

func commentFormat(r config.Rule, a commentSource, b commentBlock) ([]Diagnostic, error) {
	c := r.Comments
	pattern, err := config.CompileNamePattern(*c.Body.LinePattern)
	if err != nil {
		return nil, err
	}
	var diagnostics []Diagnostic
	add := func(p commentPart, message string) {
		pos := a.source.fset.File(a.source.node.Pos()).PositionFor(
			a.source.fset.File(a.source.node.Pos()).Pos(p.start), false)
		diagnostics = append(diagnostics, commentDiagnostic(r, a.source,
			"invalid_comment_format", message, pos.Line, pos.Column))
	}
	docs := blockDocParts(c, a, b)
	for _, p := range docs {
		if !strings.HasPrefix(p.raw, "//") {
			add(p, "doc comments must use //")
		}
	}
	if len(docs) > 0 && *c.Doc.RequireNamePrefix {
		first := strings.SplitN(docText(docs), "\n", 2)[0]
		rest, ok := strings.CutPrefix(first, b.name)
		if ok && rest != "" {
			rune, _ := utf8.DecodeRuneInString(rest)
			ok = unicode.IsSpace(rune)
		}
		if !ok || *c.Doc.RequireTextAfterName && strings.TrimSpace(rest) == "" {
			add(docs[0], fmt.Sprintf("doc comment must start with %s followed by whitespace and explanation", b.name))
		}
	}
	for _, p := range a.comments {
		if p.directive && *c.ExcludeGoDirectives || p.start < b.start || p.end > b.end {
			continue
		}
		if p.position == "trailing" {
			if !*c.Trailing.Allowed {
				add(p, "trailing comments are not allowed")
			}
		} else if !strings.HasPrefix(p.raw, "//") || !pattern.MatchString(strings.TrimPrefix(p.raw[2:], " ")) {
			add(p, "body comment must use // and match line_pattern")
		}
	}
	return diagnostics, nil
}

func commentLineLength(r config.Rule, a commentSource) []Diagnostic {
	lines := map[int]bool{}
	c := r.Comments
	for _, p := range a.comments {
		if p.directive && *c.ExcludeGoDirectives || !slices.Contains(c.Positions, p.position) {
			continue
		}
		for line := p.first; line <= p.last; line++ {
			lines[line] = true
		}
	}
	var diagnostics []Diagnostic
	for line := range lines {
		text := a.lines[line-1]
		if line < len(a.lines) {
			text = bytes.TrimSuffix(text, []byte("\r"))
		}
		col := firstOverlongColumn(string(text), *c.MaxColumn, *c.TabWidth)
		if col > 0 {
			diagnostics = append(diagnostics, commentDiagnostic(r, a.source, "comment_line_too_long",
				fmt.Sprintf("comment line exceeds column %d", *c.MaxColumn), line, col))
		}
	}
	return diagnostics
}
