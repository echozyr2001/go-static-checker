package checker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/echozyr2001/go-static-checker/internal/config"
)

func qualityFixture(t *testing.T, kind, options, body string) config.Config {
	t.Helper()
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/quality\n\ngo 1.27.3\n")
	write(t, root, "app.go", "package app\nfunc value(n int) int { return n*2 }\n")
	write(t, root, "app_test.go", body)
	text := `schema_version = 1
[project]
root = "."
include_tests = false
build_tags = []
[[rules]]
id = "TEST"
kind = "` + kind + `"
description = "test quality"
severity = "warning"
[rules.tests]
packages = ["..."]
include_generated = false
` + options
	write(t, root, "rules.toml", text)
	c, err := config.Load(filepath.Join(root, "rules.toml"))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestDuplicateTestBodies(t *testing.T) {
	c := qualityFixture(t, "test_duplicates", "duplicate_bodies = true\nduplicate_cases = false\n", `package app
import "testing"
func TestOne(t *testing.T) { if value(2)!=4 { t.Fatal("bad") } }
func TestTwo(t *testing.T) { // same body
 if value(2) != 4 { t.Fatal("bad") }
}
func TestDifferent(t *testing.T) { if value(3)!=6 { t.Fatal("bad") } }
`)
	r, err := Run(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if r.ExitCode() != 0 || len(r.Diagnostics) != 1 {
		t.Fatalf("%+v", r)
	}
	d := r.Diagnostics[0]
	if d.Code != "duplicate_test_body" || d.Line != 4 || !strings.Contains(d.Message, "app_test.go:3:") {
		t.Fatal(d)
	}
}

func TestDuplicateTableCases(t *testing.T) {
	c := qualityFixture(t, "test_duplicates", "duplicate_bodies = false\nduplicate_cases = true\n", `package app
import "testing"
func TestTable(t *testing.T) {
 cases := []struct{name string; input, want int}{
  {"first",2,4},
  {"second",1+1,4},
  {"other",3,6},
 }
 for _,tc := range cases { t.Run(tc.name,func(t *testing.T){if value(tc.input)!=tc.want {t.Fatal("bad")}}) }
}
`)
	r, err := Run(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "duplicate_test_case" || r.Diagnostics[0].Line != 6 ||
		!strings.Contains(r.Diagnostics[0].Message, "app_test.go:5:") {
		t.Fatalf("%+v", r)
	}
}

func TestRealTimeWaiting(t *testing.T) {
	c := qualityFixture(t, "test_waiting", "", `package app
import("testing"; clock "time"; "testing/synctest")
func pause(){clock.Sleep(clock.Millisecond)}
func TestWaiting(t *testing.T) {
 pause()
 clock.Sleep(0)
 synctest.Test(t,func(t *testing.T){pause();clock.Sleep(clock.Second)})
 t.Run("real",func(t *testing.T){clock.Sleep(clock.Second)})
}
`)
	r, err := Run(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Diagnostics) != 2 || r.ExitCode() != 0 {
		t.Fatalf("%+v", r)
	}
	for _, d := range r.Diagnostics {
		if d.Code != "real_time_wait" || d.Severity != "warning" {
			t.Fatal(d)
		}
	}
	if r.Diagnostics[0].Line != 3 || r.Diagnostics[1].Line != 8 {
		t.Fatal(r.Diagnostics)
	}
}

func TestQualityLoadFailure(t *testing.T) {
	c := qualityFixture(t, "test_waiting", "", "package app\nimport \"testing\"\nfunc TestBad(t *testing.T){unknown()}\n")
	_, err := Run(context.Background(), c)
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("got %v", err)
	}
}

func TestQualityBuildSelection(t *testing.T) {
	c := qualityFixture(t, "test_waiting", "", "package app\nimport \"testing\"\nfunc TestOK(t *testing.T){}\n")
	write(t, c.Project.Root, "ignored_test.go", "//go:build not_selected\n\npackage app\nfunc broken(){unknown()}\n")
	write(t, c.Project.Root, "external_test.go", "package app_test\nimport(\"testing\";\"time\")\nfunc TestExternal(t *testing.T){time.Sleep(time.Second)}\n")
	r, err := Run(context.Background(), c)
	if err != nil || len(r.Diagnostics) != 1 || r.Diagnostics[0].File != "external_test.go" || r.Files != 3 {
		t.Fatalf("%+v %v", r, err)
	}
	if err := os.Remove(filepath.Join(c.Project.Root, "external_test.go")); err != nil {
		t.Fatal(err)
	}
	r, err = Run(context.Background(), c)
	if err != nil || len(r.Diagnostics) != 0 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestTableCasesConservative(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       int
	}{
		{"name used by behavior", `cases:=[]struct{name string; input int}{{"a",1},{"b",1}}
for _,tc:=range cases {t.Run(tc.name,func(t *testing.T){t.Log(tc.name,tc.input)})}`, 0},
		{"dynamic values", `cases:=[]struct{input int}{{value(1)},{value(1)}}
for _,tc:=range cases {t.Run("case",func(t *testing.T){t.Log(tc.input)})}`, 0},
		{"index matters", `cases:=[]struct{input int}{{1},{1}}
for i,tc:=range cases {t.Run("case",func(t *testing.T){t.Log(i,tc.input)})}`, 0},
		{"table modified", `cases:=[]struct{input int}{{1},{1}}
cases[1].input=2
for _,tc:=range cases {t.Run("case",func(t *testing.T){t.Log(tc.input)})}`, 0},
		{"no subtest", `cases:=[]struct{input int}{{1},{1}}
for _,tc:=range cases {t.Log(tc.input)}`, 0},
		{"keyed zero values", `for _,tc:=range []struct{name string;input int;want bool}{
{name:"a"},{name:"b",input:0,want:false},} {t.Run(tc.name,func(t *testing.T){t.Log(tc.input,tc.want)})}`, 1},
		{"interface types differ", `for _,tc:=range []struct{input any}{{int(1)},{int64(1)}} {
t.Run("case",func(t *testing.T){t.Log(tc.input)})}`, 0},
		{"escaped row retains name", `for _,tc:=range []struct{name string;input int}{{"a",1},{"b",1}} {
t.Run(tc.name,func(t *testing.T){t.Log(tc)})}`, 0},
		{"mutated row", `for _,tc:=range []struct{input int}{{1},{1}} {
tc.input++;t.Run("case",func(t *testing.T){t.Log(tc.input)})}`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := qualityFixture(t, "test_duplicates", "duplicate_bodies = false\nduplicate_cases = true\n",
				"package app\nimport \"testing\"\nfunc TestCases(t *testing.T){\n"+tc.body+"\n}\n")
			r, err := Run(context.Background(), c)
			if err != nil || len(r.Diagnostics) != tc.want {
				t.Fatalf("%+v %v", r, err)
			}
		})
	}
}

func TestDuplicateSubtestCaptures(t *testing.T) {
	c := qualityFixture(t, "test_duplicates", "duplicate_bodies = true\nduplicate_cases = false\n", `package app
import "testing"
func TestParent(t *testing.T){
 t.Run("a",func(t *testing.T){if value(2)!=4 {t.Fatal("bad")}})
 t.Run("b",func(t *testing.T){if value(2)!=4 {t.Fatal("bad")}})
 {n:=1;t.Run("capture1",func(t *testing.T){t.Log(n)})}
 {n:=2;t.Run("capture2",func(t *testing.T){t.Log(n)})}
}
func TestEmptyOne(t *testing.T){}
func TestEmptyTwo(t *testing.T){}
func helper(t *testing.T){if value(2)!=4 {t.Fatal("bad")}}
`)
	r, err := Run(context.Background(), c)
	if err != nil || len(r.Diagnostics) != 1 || r.Diagnostics[0].Line != 5 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestWaitingVirtualAndUncalled(t *testing.T) {
	c := qualityFixture(t, "test_waiting", "", `package app
import("testing";"time";"testing/synctest")
func helper(){time.Sleep(time.Second)}
func unused(){time.Sleep(time.Hour)}
func TestVirtual(t *testing.T){
 synctest.Test(t,func(t *testing.T){helper()})
 ignored:=func(){time.Sleep(time.Hour)};_ = ignored
 time.Sleep(-time.Second)
}
func BenchmarkIgnored(b *testing.B){time.Sleep(time.Hour)}
func ExampleIgnored(){time.Sleep(time.Hour)}
`)
	r, err := Run(context.Background(), c)
	if err != nil || len(r.Diagnostics) != 0 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestWaitingShadowAndRecursion(t *testing.T) {
	c := qualityFixture(t, "test_waiting", "", `package app
import("testing";"time")
func recursive(n int){if n>0 {recursive(n-1)};time.Sleep(time.Millisecond)}
func TestCalls(t *testing.T){
 time:=struct{Sleep func(int)}{Sleep:func(int){}}
 time.Sleep(2)
 recursive(1)
 func(){recursive(1)}()
}
`)
	r, err := Run(context.Background(), c)
	if err != nil || len(r.Diagnostics) != 1 || r.Diagnostics[0].Line != 3 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestQualityScopeAndGenerated(t *testing.T) {
	c := qualityFixture(t, "test_waiting", "", "// Code generated by fixture. DO NOT EDIT.\npackage app\nimport(\"testing\";\"time\")\nfunc TestGenerated(t *testing.T){time.Sleep(time.Second)}\n")
	r, err := Run(context.Background(), c)
	if err != nil || len(r.Diagnostics) != 0 {
		t.Fatalf("%+v %v", r, err)
	}
	*c.Rules[0].Tests.IncludeGenerated = true
	r, err = Run(context.Background(), c)
	if err != nil || len(r.Diagnostics) != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	c.Rules[0].Tests.Packages = []string{"other/..."}
	r, err = Run(context.Background(), c)
	if err != nil || len(r.Diagnostics) != 0 || r.Files != 1 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestDynamicCaseNamesAreNotStaticRows(t *testing.T) {
	c := qualityFixture(t, "test_duplicates", "duplicate_bodies = false\nduplicate_cases = true\n", `package app
import "testing"
func label()string{return "case"}
func TestRows(t *testing.T){
 for _,tc:=range []struct{name string;input int}{{label(),1},{label(),1}} {
 t.Run(tc.name,func(t *testing.T){t.Log(tc.input)})
 }
}
`)
	r, err := Run(context.Background(), c)
	if err != nil || len(r.Diagnostics) != 0 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestUnnamedTestingParameter(t *testing.T) {
	c := qualityFixture(t, "test_waiting", "", `package app
import("testing";"time")
func TestUnnamed(*testing.T){time.Sleep(time.Second)}
`)
	r, err := Run(context.Background(), c)
	if err != nil || len(r.Diagnostics) != 1 || r.Diagnostics[0].Line != 3 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestDuplicateBodiesKeepImportBindings(t *testing.T) {
	c := qualityFixture(t, "test_duplicates", "duplicate_bodies = true\nduplicate_cases = false\n", `package app
import("testing";dep "fmt")
func TestFirst(t *testing.T){dep.Print("value")}
`)
	write(t, c.Project.Root, "other_test.go", `package app
import("testing";dep "log")
func TestSecond(t *testing.T){dep.Print("value")}
`)
	r, err := Run(context.Background(), c)
	if err != nil || len(r.Diagnostics) != 0 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestWaitingTestOnlyPackageAndDotImport(t *testing.T) {
	c := qualityFixture(t, "test_waiting", "", "package app\nimport \"testing\"\nfunc TestOK(t *testing.T){}\n")
	write(t, c.Project.Root, "isolated/only_test.go", `package isolated
import("testing";. "time")
//line remapped.go:500
func TestOnly(t *testing.T){Sleep(Second)}
`)
	r, err := Run(context.Background(), c)
	if err != nil || len(r.Diagnostics) != 1 || r.Packages != 2 || r.Files != 3 {
		t.Fatalf("%+v %v", r, err)
	}
	if r.Diagnostics[0].File != "isolated/only_test.go" || r.Diagnostics[0].Line != 4 {
		t.Fatal(r.Diagnostics)
	}
}
