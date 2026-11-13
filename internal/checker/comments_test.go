package checker

import (
	"context"
	"strings"
	"testing"

	"github.com/echozyr2001/go-static-checker/internal/config"
)

func commentFixture(t *testing.T, id, body string) config.Config {
	t.Helper()
	c, err := config.Load("../../rules.toml")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range c.Rules {
		if r.ID == id {
			c.Rules = []config.Rule{r}
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("rule %s not found", id)
	}
	c.Project.Root = t.TempDir()
	write(t, c.Project.Root, "go.mod", "module example.com/comments\n\ngo 1.24\n")
	write(t, c.Project.Root, "sample.go", body)
	return c
}

func TestCommentBudgetRounding(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       int
	}{
		{"one allowed", "package sample\n// F works.\nfunc F() {}\n", 0},
		{"two exceed", "package sample\n// F works.\n// Extra explanation.\nfunc F() {}\n", 2},
		{"string markers", "package sample\nfunc F() {\n _ = \"// not a comment\"\n _ = `/* also data */`\n}\n", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := commentFixture(t, "COMMENT001", tc.body)
			r, err := Run(context.Background(), c)
			if err != nil {
				t.Fatal(err)
			}
			if len(r.Diagnostics) != tc.want {
				t.Fatalf("got %+v", r.Diagnostics)
			}
			for _, d := range r.Diagnostics {
				if d.Code != "comment_budget_exceeded" {
					t.Fatalf("%+v", d)
				}
			}
		})
	}
}

func TestRequiredComments(t *testing.T) {
	c := commentFixture(t, "COMMENT002", "package sample\ntype Public struct{}\nfunc Exported() {}\nfunc private() {}\n")
	r, err := Run(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Diagnostics) != 2 || r.Diagnostics[0].Line != 2 || r.Diagnostics[1].Line != 3 {
		t.Fatalf("%+v", r)
	}
	for _, d := range r.Diagnostics {
		if d.Code != "missing_required_comment" {
			t.Fatal(d)
		}
	}
}

func TestCommentFormats(t *testing.T) {
	c := commentFixture(t, "COMMENT003", "package sample\n// Wrong explanation.\nfunc F() {\n //\n x := 1 // trailing\n _ = x\n}\n")
	r, err := Run(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Diagnostics) != 3 {
		t.Fatalf("%+v", r)
	}
	for _, d := range r.Diagnostics {
		if d.Code != "invalid_comment_format" {
			t.Fatal(d)
		}
	}
}

func TestCommentColumn(t *testing.T) {
	c := commentFixture(t, "COMMENT004", "package sample\n//"+strings.Repeat("中", 79)+"\r\nvar S = \"//"+strings.Repeat("x", 90)+"\"\n")
	r, err := Run(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "comment_line_too_long" || r.Diagnostics[0].Line != 2 || r.Diagnostics[0].Column != 237 {
		t.Fatalf("%+v", r)
	}
}

func TestMarkdownLocations(t *testing.T) {
	c := commentFixture(t, "DOC001", "package sample\n")
	for _, name := range []string{"README.md", "CONTRIBUTING.md", "docs/README.md", "vendor/lib/guide.MD", ".git/ignored.md", "guide.MD"} {
		write(t, c.Project.Root, name, "text")
	}
	r, err := Run(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Diagnostics) != 4 {
		t.Fatalf("%+v", r)
	}
	for _, d := range r.Diagnostics {
		if d.Code != "forbidden_file_location" || d.File == "README.md" {
			t.Fatal(d)
		}
	}
}

func TestTrailingCommentsAfterDeclarations(t *testing.T) {
	c := commentFixture(t, "COMMENT003", "package sample\nfunc f() {} // explain\ntype s struct{} // explain\n")
	r, err := Run(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Diagnostics) != 2 || r.Diagnostics[0].Line != 2 || r.Diagnostics[1].Line != 3 {
		t.Fatalf("%+v", r)
	}
}

func TestCommentBudgetBodyAndNesting(t *testing.T) {
	body := "package sample\nfunc f() {\n // one\n // two\n // three\n // four\n g := func() {\n  // five\n }\n _ = g\n}\n"
	c := commentFixture(t, "COMMENT001", body)
	r, err := Run(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Diagnostics) != 2 {
		t.Fatalf("%+v", r)
	}
	for _, d := range r.Diagnostics {
		if !strings.Contains(d.Message, "5 comment lines") {
			t.Fatal(d)
		}
	}
}

func TestRequiredCommentDirectiveAndGroupedTypes(t *testing.T) {
	c := commentFixture(t, "COMMENT002", "package sample\n//go:noinline\nfunc F() {}\ntype (\n // A documents A.\n A struct{}\n B interface{}\n)\n")
	r, err := Run(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Diagnostics) != 2 || r.Diagnostics[0].Line != 3 || r.Diagnostics[1].Line != 7 {
		t.Fatalf("%+v", r)
	}
}

func TestCommentCRLFAndRawString(t *testing.T) {
	body := "package sample\r\n/* one\r\n\r\nthree */\r\nfunc f() {\r\n _ = `// raw\r\n/* literal */`\r\n}\r\n"
	c := commentFixture(t, "COMMENT001", body)
	r, err := Run(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Diagnostics) != 2 {
		t.Fatalf("%+v", r)
	}
	for _, d := range r.Diagnostics {
		if !strings.Contains(d.Message, "3 comment lines") {
			t.Fatal(d)
		}
	}
}

func TestCommentPackageAggregation(t *testing.T) {
	c := commentFixture(t, "COMMENT001", "// one\n// two\npackage sample\n")
	write(t, c.Project.Root, "other.go", "package sample\nvar a=1\nvar b=2\nvar c=3\nvar d=4\nvar e=5\n")
	r, err := Run(context.Background(), c)
	if err != nil || len(r.Diagnostics) != 0 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestCommentSelection(t *testing.T) {
	c := commentFixture(t, "COMMENT002", "package sample\nfunc Public() {}\n")
	write(t, c.Project.Root, "generated.go", "// Code generated by example. DO NOT EDIT.\npackage sample\nfunc Generated() {}\n")
	write(t, c.Project.Root, "sample_test.go", "package sample\nfunc Helper() {}\n")
	write(t, c.Project.Root, "external_test.go", "package sample_test\nfunc External() {}\n")
	write(t, c.Project.Root, "ignored.go", "//go:build neverselected\n\npackage sample\nfunc Ignored() {}\n")
	for _, tc := range []struct {
		tests, generated bool
		want             int
	}{
		{false, false, 1}, {true, false, 3}, {false, true, 2}, {true, true, 4},
	} {
		*c.Rules[0].Comments.IncludeTests = tc.tests
		*c.Rules[0].Comments.IncludeGenerated = tc.generated
		r, err := Run(context.Background(), c)
		if err != nil || len(r.Diagnostics) != tc.want {
			t.Fatalf("%+v: %+v %v", tc, r, err)
		}
	}
	c.Rules[0].Comments.Packages = []string{"other/..."}
	r, err := Run(context.Background(), c)
	if err != nil || len(r.Diagnostics) != 0 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestCommentFormattingFieldsAndNames(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       int
	}{
		{"field body", "// S describes data.\ntype S struct {\n // Value stores a number.\n Value int\n}\n", 0},
		{"bare name", "// F\nfunc F() {}\n", 1},
		{"longer name", "// Foo explains a different name.\nfunc F() {}\n", 1},
		{"block body", "func f() {\n /* explanation */\n}\n", 1},
		{"empty doc", "//\nfunc F() {}\n", 1},
		{"method", "type s struct{}\n// wrong name\nfunc (s) Method() {}\n", 1},
		{"two declarations same line", "func f() {}; func g() {} // explanation\n", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := commentFixture(t, "COMMENT003", "package sample\n"+tc.body)
			r, err := Run(context.Background(), c)
			if err != nil || len(r.Diagnostics) != tc.want {
				t.Fatalf("%+v %v", r, err)
			}
		})
	}
}

func TestCommentBudgetExactCounts(t *testing.T) {
	body := "package sample\n// F explains work.\nfunc F() {\n /* one */ /* two */ _ = 1\n // more\n _ = 2\n}\n"
	c := commentFixture(t, "COMMENT001", body)
	r, err := Run(context.Background(), c)
	if err != nil || len(r.Diagnostics) != 2 {
		t.Fatalf("%+v %v", r, err)
	}
	if !strings.Contains(r.Diagnostics[1].Message, "3 comment lines and 3 code lines; allowed 2") {
		t.Fatal(r.Diagnostics)
	}
}

func TestCommentColumnTabsBlocksAndDirectives(t *testing.T) {
	body := "package sample\n//" + strings.Repeat("x", 78) + "\n\t//" + strings.Repeat("x", 75) + "\n/*\n" + strings.Repeat("x", 81) + "\n*/\n//go:generate " + strings.Repeat("x", 80) + "\n"
	c := commentFixture(t, "COMMENT004", body)
	r, err := Run(context.Background(), c)
	if err != nil || len(r.Diagnostics) != 3 {
		t.Fatalf("%+v %v", r, err)
	}
	for i, line := range []int{3, 5, 7} {
		if r.Diagnostics[i].Line != line {
			t.Fatal(r.Diagnostics)
		}
	}
	*c.Rules[0].Comments.ExcludeGoDirectives = true
	r, err = Run(context.Background(), c)
	if err != nil || len(r.Diagnostics) != 2 {
		t.Fatalf("%+v %v", r, err)
	}
}
