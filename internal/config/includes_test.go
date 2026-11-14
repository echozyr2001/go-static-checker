package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const includeProject = `schema_version = 1
[project]
root = "../project"
include_tests = false
build_tags = []
`

func layoutRule(id string) string {
	return `
[[rules]]
id = "` + id + `"
kind = "file_layout"
description = "required file"
severity = "error"
[rules.layout]
required_files = ["README.md"]
required_directories = []
`
}

func writeRuleFile(t *testing.T, root, name, body string) string {
	t.Helper()
	filename := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return filename
}

func TestLoadIncludes(t *testing.T) {
	root := t.TempDir()
	main := writeRuleFile(t, root, "config/rules.toml", "includes = [\"parts/one.toml\", \"two.toml\"]\n"+includeProject+layoutRule("MAIN"))
	writeRuleFile(t, root, "config/parts/one.toml", "schema_version = 1\nincludes = [\"../nested/three.toml\"]\n"+layoutRule("ONE"))
	writeRuleFile(t, root, "config/nested/three.toml", "schema_version = 1\n"+layoutRule("THREE"))
	writeRuleFile(t, root, "config/two.toml", "schema_version = 1\n"+layoutRule("TWO"))
	c, err := Load(main)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, r := range c.Rules {
		ids = append(ids, r.ID)
	}
	if !reflect.DeepEqual(ids, []string{"MAIN", "ONE", "THREE", "TWO"}) {
		t.Fatal(ids)
	}
	if c.Project.Root != filepath.Join(root, "project") {
		t.Fatal(c.Project.Root)
	}
	if c.Rules[2].Layout.RequiredFiles[0] != "README.md" {
		t.Fatal(c.Rules[2])
	}
}

func TestLoadIncludesOnly(t *testing.T) {
	root := t.TempDir()
	main := writeRuleFile(t, root, "rules.toml", "includes = [\"part.toml\"]\n"+includeProject)
	writeRuleFile(t, root, "part.toml", "schema_version = 1\n"+layoutRule("PART"))
	c, err := Load(main)
	if err != nil || len(c.Rules) != 1 || c.Rules[0].ID != "PART" {
		t.Fatalf("%+v %v", c, err)
	}
}

func TestLoadIncludesErrors(t *testing.T) {
	for _, tc := range []struct{ name, includes, part, want string }{
		{"missing", `["missing.toml"]`, "", "missing.toml"},
		{"cycle", `["part.toml"]`, "schema_version = 1\nincludes = [\"rules.toml\"]", "cycle"},
		{"repeat", `["part.toml", "./part.toml"]`, "schema_version = 1\n" + layoutRule("PART"), "already included"},
		{"duplicate id", `["part.toml"]`, "schema_version = 1\n" + layoutRule("MAIN"), "duplicate rule id"},
		{"project override", `["part.toml"]`, includeProject + layoutRule("PART"), "project"},
		{"empty project override", `["part.toml"]`, "schema_version = 1\n[project]\n" + layoutRule("PART"), "project"},
		{"unknown", `["part.toml"]`, "schema_version = 1\nunknown = true\n" + layoutRule("PART"), "unknown"},
		{"invalid rule", `["part.toml"]`, "schema_version = 1\n" + strings.Replace(layoutRule("PART"), "file_layout", "typo", 1), "unknown kind"},
		{"missing version", `["part.toml"]`, layoutRule("PART"), "schema_version"},
		{"version", `["part.toml"]`, "schema_version = 2\n" + layoutRule("PART"), "schema_version"},
		{"empty reference", `[""]`, "", "empty include"},
		{"malformed", `["part.toml"]`, "broken = [", "part.toml"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			main := writeRuleFile(t, root, "rules.toml", "includes = "+tc.includes+"\n"+includeProject+layoutRule("MAIN"))
			if tc.part != "" {
				writeRuleFile(t, root, "part.toml", tc.part)
			}
			_, err := Load(main)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
			if !strings.Contains(err.Error(), "rules.toml") {
				t.Fatalf("missing entry context: %v", err)
			}
		})
	}
}

func TestLoadIncludeSymlinkCycle(t *testing.T) {
	root := t.TempDir()
	main := writeRuleFile(t, root, "rules.toml", "includes = [\"alias.toml\"]\n"+includeProject+layoutRule("MAIN"))
	if err := os.Symlink(main, filepath.Join(root, "alias.toml")); err != nil {
		t.Fatal(err)
	}
	_, err := Load(main)
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("got %v", err)
	}
}

func TestLoadIncludeDiamond(t *testing.T) {
	root := t.TempDir()
	main := writeRuleFile(t, root, "rules.toml", "includes = [\"a.toml\", \"b.toml\"]\n"+includeProject)
	for _, name := range []string{"a.toml", "b.toml"} {
		writeRuleFile(t, root, name, "schema_version = 1\nincludes = [\"shared.toml\"]\n")
	}
	writeRuleFile(t, root, "shared.toml", "schema_version = 1\n"+layoutRule("SHARED"))
	_, err := Load(main)
	if err == nil || !strings.Contains(err.Error(), "already included") || !strings.Contains(err.Error(), "b.toml") {
		t.Fatalf("got %v", err)
	}
}

func TestLoadEmptyIncludes(t *testing.T) {
	root := t.TempDir()
	main := writeRuleFile(t, root, "rules.toml", "includes = []\n"+includeProject)
	_, err := Load(main)
	if err == nil || !strings.Contains(err.Error(), "at least one rule") {
		t.Fatalf("got %v", err)
	}
	writeRuleFile(t, root, "rules.toml", "includes = []\n"+includeProject+layoutRule("ONE"))
	c, err := Load(main)
	if err != nil || len(c.Rules) != 1 {
		t.Fatalf("%+v %v", c, err)
	}
}

func TestLoadIncludeAbsoluteAndSymlinkRelativePaths(t *testing.T) {
	root := t.TempDir()
	target := writeRuleFile(t, root, "target/part.toml", "schema_version = 1\nincludes = [\"child.toml\"]\n")
	writeRuleFile(t, root, "config/child.toml", "schema_version = 1\n"+layoutRule("CHILD"))
	link := filepath.Join(root, "config/link.toml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	main := writeRuleFile(t, root, "config/rules.toml", "includes = ['"+filepath.ToSlash(link)+"']\n"+includeProject)
	c, err := Load(main)
	if err != nil || len(c.Rules) != 1 || c.Rules[0].ID != "CHILD" {
		t.Fatalf("%+v %v", c, err)
	}
}

func TestLoadCleansAbsoluteRoot(t *testing.T) {
	root := t.TempDir()
	projectRoot := filepath.ToSlash(root) + "/unused/../project"
	body := strings.Replace(includeProject, `root = "../project"`, `root = "`+projectRoot+`"`, 1)
	main := writeRuleFile(t, root, "rules.toml", body+layoutRule("ROOT"))
	c, err := Load(main)
	if err != nil || c.Project.Root != filepath.Join(root, "project") {
		t.Fatalf("%+v %v", c, err)
	}
}
