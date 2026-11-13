package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMatch(t *testing.T) {
	for _, tc := range []struct {
		pattern, value string
		want           bool
	}{
		{"internal/service/...", "internal/service", true}, {"internal/service/...", "internal/service/order", true}, {"internal/service/...", "internal/servicekit", false}, {".", ".", true}, {".", "cmd", false}, {"...", "a/b", true}, {"a", "a/b", false},
	} {
		if got := Match(tc.pattern, tc.value); got != tc.want {
			t.Errorf("Match(%q,%q)=%v", tc.pattern, tc.value, got)
		}
	}
}

func TestLoadStrict(t *testing.T) {
	original, err := os.ReadFile("../../rules.toml")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, old, new, want string }{
		{"valid", "", "", ""},
		{"unknown", "allow_stdlib", "allow_stdlbi", "unknown"},
		{"missing bool", "allow_stdlib = true", "", "behavior"},
		{"missing array", "allow_external = []", "", "required"},
		{"unknown layer", "allow = [\"config\"]", "allow = [\"missing\"]", "unknown layer"},
		{"bad pattern", "cmd/...", "cmd/*", "invalid pattern"},
		{"tests", "include_tests = false", "include_tests = true", "not supported"},
		{"version", "schema_version = 1", "schema_version = 2", "schema_version"},
		{"duplicate", "name = \"checker\"", "name = \"cli\"", "duplicate layer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := string(original)
			if tc.old != "" {
				data = strings.Replace(data, tc.old, tc.new, 1)
			}
			file := filepath.Join(t.TempDir(), "rules.toml")
			if err := os.WriteFile(file, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(file)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}

func TestAbsoluteProjectRoot(t *testing.T) {
	original, err := os.ReadFile("../../rules.toml")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	data := strings.Replace(string(original), `root = "."`, `root = "`+filepath.ToSlash(root)+`"`, 1)
	filename := filepath.Join(t.TempDir(), "rules.toml")
	if err := os.WriteFile(filename, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(filename)
	if err != nil {
		t.Fatal(err)
	}
	if c.Project.Root != root {
		t.Fatalf("root = %q, want %q", c.Project.Root, root)
	}
}
