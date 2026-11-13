package checker

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/scanner"
	"go/token"
	"strings"
)

type commentPart struct {
	start, end    int
	first, last   int
	raw, position string
	directive     bool
}

type commentBlock struct {
	name, kind string
	start, end int
	doc        *ast.CommentGroup
	pos        token.Pos
}

type codePart struct {
	start, end int
	delimiter  string
}

type commentSource struct {
	source   source
	lines    [][]byte
	starts   []int
	comments []commentPart
	blocks   []commentBlock
	code     []codePart
}

func analyzeComments(s source) (commentSource, error) {
	a := commentSource{source: s, lines: bytes.Split(s.content, []byte("\n"))}
	offset := 0
	for _, line := range a.lines {
		a.starts = append(a.starts, offset)
		offset += len(line) + 1
	}
	file := s.fset.File(s.node.Pos())
	var scan scanner.Scanner
	var scanErr error
	scan.Init(file, s.content, func(pos token.Position, msg string) {
		if scanErr == nil {
			scanErr = fmt.Errorf("%s: %s", pos, msg)
		}
	},
		scanner.ScanComments)
	for {
		pos, tok, lit := scan.Scan()
		if tok == token.EOF {
			break
		}
		start := file.Offset(pos)
		if tok == token.COMMENT {
			end := start
			if bytes.HasPrefix(s.content[start:], []byte("//")) {
				end = len(s.content)
				if n := bytes.IndexByte(s.content[start:], '\n'); n >= 0 {
					end = start + n
				}
				if end > start && s.content[end-1] == '\r' {
					end--
				}
			} else {
				end = start + bytes.Index(s.content[start:], []byte("*/")) + 2
			}
			raw := string(s.content[start:end])
			a.comments = append(a.comments, commentPart{start: start, end: end,
				first: file.PositionFor(pos, false).Line, last: file.PositionFor(file.Pos(end-1), false).Line,
				raw: raw, directive: isGoDirective(raw)})
			continue
		}
		if tok == token.SEMICOLON && lit == "\n" {
			continue
		}
		text := lit
		if text == "" {
			text = tok.String()
		}
		end := start + len(text)
		if tok == token.STRING && s.content[start] == '`' {
			end = start + 1 + bytes.IndexByte(s.content[start+1:], '`') + 1
		}
		delimiter := ""
		if !tok.IsLiteral() && !tok.IsKeyword() && tok != token.IDENT {
			delimiter = tok.String()
		}
		a.code = append(a.code, codePart{start: start, end: end, delimiter: delimiter})
	}
	if scanErr != nil {
		return a, scanErr
	}
	for _, decl := range s.node.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			kind := "function"
			if d.Recv != nil {
				kind = "method"
			}
			a.blocks = append(a.blocks, commentBlock{name: d.Name.Name, kind: kind, start: file.Offset(d.Pos()),
				end: file.Offset(d.End()), doc: d.Doc, pos: d.Pos()})
		case *ast.GenDecl:
			if d.Tok != token.TYPE {
				continue
			}
			for _, spec := range d.Specs {
				t := spec.(*ast.TypeSpec)
				typ := t.Type
				for {
					p, ok := typ.(*ast.ParenExpr)
					if !ok {
						break
					}
					typ = p.X
				}
				kind := ""
				switch typ.(type) {
				case *ast.StructType:
					kind = "struct"
				case *ast.InterfaceType:
					kind = "interface"
				}
				if kind == "" {
					continue
				}
				doc := t.Doc
				start, pos := file.Offset(t.Pos()), t.Pos()
				if !d.Lparen.IsValid() {
					doc = d.Doc
					start = file.Offset(d.Pos())
					pos = d.Pos()
				}
				a.blocks = append(a.blocks, commentBlock{name: t.Name.Name, kind: kind, start: start,
					end: file.Offset(t.End()), doc: doc, pos: pos})
			}
		}
	}
	masked := bytes.Clone(s.content)
	for _, c := range a.comments {
		for i := c.start; i < c.end; i++ {
			if masked[i] != '\n' && masked[i] != '\r' {
				masked[i] = ' '
			}
		}
	}
	for i := range a.comments {
		c := &a.comments[i]
		c.position = "file"
		if len(bytes.TrimSpace(masked[a.starts[c.first-1]:c.start])) > 0 {
			c.position = "trailing"
			continue
		}
		for _, b := range a.blocks {
			if inDoc(s, b.doc, *c) {
				c.position = "doc"
				break
			}
			if c.start >= b.start && c.end <= b.end {
				c.position = "body"
				break
			}
		}
		if c.position == "file" && inDoc(s, s.node.Doc, *c) {
			c.position = "doc"
		}
	}
	for i := range a.blocks {
		b := &a.blocks[i]
		endLine := file.PositionFor(file.Pos(b.end-1), false).Line
		for _, c := range a.comments {
			if c.first != endLine || c.start < b.end {
				continue
			}
			separated := false
			for _, code := range a.code {
				if code.start >= b.end && code.start < c.start && code.delimiter != ";" {
					separated = true
					break
				}
			}
			if !separated {
				b.end = c.end
			}
		}
	}
	return a, nil
}

func inDoc(s source, doc *ast.CommentGroup, c commentPart) bool {
	if doc == nil {
		return false
	}
	for _, part := range doc.List {
		if s.fset.PositionFor(part.Pos(), false).Offset == c.start {
			return true
		}
	}
	return false
}

func isGoDirective(raw string) bool {
	return strings.HasPrefix(raw, "//go:") || strings.HasPrefix(raw, "//line ") ||
		strings.HasPrefix(raw, "/*line ") || strings.HasPrefix(raw, "// +build ")
}
