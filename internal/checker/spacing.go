package checker

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/token"

	"github.com/echozyr2001/go-static-checker/internal/config"
)

type declarationSpan struct {
	start, doc, end token.Pos
	relevant        bool
}

// Check adjacent top-level declarations and adjacent specs within a type group.
// Non-function/non-struct declarations break adjacency; local types are excluded.
func declarationSpacing(r config.Rule, s source) []Diagnostic {
	minimum := r.Format.MinBlankLinesBetweenDeclarations
	if minimum == 0 {
		return nil
	}
	var blockComments [][2]int
	for _, group := range s.node.Comments {
		for _, comment := range group.List {
			start := s.fset.PositionFor(comment.Pos(), false).Offset
			if bytes.HasPrefix(s.content[start:], []byte("/*")) {
				// Parsing has already validated the closing delimiter. AST comment
				// text omits CR bytes, so Comment.End is not a physical CRLF offset.
				end := start + bytes.Index(s.content[start:], []byte("*/")) + 2
				blockComments = append(blockComments, [2]int{start, end})
			}
		}
	}
	var diagnostics []Diagnostic
	check := func(spans []declarationSpan) {
		for i := 1; i < len(spans); i++ {
			previous, current := spans[i-1], spans[i]
			if !previous.relevant || !current.relevant {
				continue
			}
			end := s.fset.PositionFor(previous.end, false).Offset
			start := s.fset.PositionFor(current.doc, false).Offset
			gap := s.content[end:start]
			lines := bytes.Split(gap, []byte("\n"))
			blanks := 0
			offset := end + len(lines[0]) + 1
			// Only complete physical lines between declarations count. A whitespace-only
			// line inside a block comment is not a blank separator.
			for j := 1; j < len(lines)-1; j++ {
				lineOffset := offset
				offset += len(lines[j]) + 1
				if len(bytes.TrimSpace(lines[j])) != 0 {
					continue
				}
				inComment := false
				for _, bounds := range blockComments {
					if lineOffset >= bounds[0] && lineOffset < bounds[1] {
						inComment = true
						break
					}
				}
				if !inComment {
					blanks++
				}
			}
			if blanks >= minimum {
				continue
			}
			pos := s.fset.PositionFor(current.start, false)
			diagnostics = append(diagnostics, Diagnostic{RuleID: r.ID, Code: "missing_declaration_blank_line",
				Severity: r.Severity,
				Message:  fmt.Sprintf("declarations require at least %d blank line(s); found %d", minimum, blanks),
				File:     s.file, Line: pos.Line, Column: pos.Column, SourcePackage: s.pkg.PkgPath})
		}
	}
	var top []declarationSpan
	for _, decl := range s.node.Decls {
		span := declarationSpan{start: decl.Pos(), doc: decl.Pos(), end: decl.End()}
		switch d := decl.(type) {
		case *ast.FuncDecl:
			span.relevant = true
			if d.Doc != nil {
				span.doc = d.Doc.Pos()
			}
		case *ast.GenDecl:
			if d.Doc != nil {
				span.doc = d.Doc.Pos()
			}
			if d.Tok == token.TYPE {
				var specs []declarationSpan
				for _, spec := range d.Specs {
					t := spec.(*ast.TypeSpec)
					_, isStruct := t.Type.(*ast.StructType)
					item := declarationSpan{start: t.Pos(), doc: t.Pos(), end: t.End(), relevant: isStruct}
					if t.Doc != nil {
						item.doc = t.Doc.Pos()
					}
					specs = append(specs, item)
					span.relevant = span.relevant || isStruct
				}
				check(specs)
			}
		}
		top = append(top, span)
	}
	check(top)
	return diagnostics
}
