package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommentAndFileConfigStrict(t *testing.T) {
	original, err := os.ReadFile("../../rules.toml")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, old, replacement, want string }{
		{"missing limit", "max_comment_percent = 20", "", "max_comment_percent is required"},
		{"unknown field", "max_comment_percent = 20", "max_comments_percent = 20", "unknown"},
		{"negative percent", "max_comment_percent = 20", "max_comment_percent = -1", "percent must"},
		{"high percent", "max_comment_percent = 20", "max_comment_percent = 101", "percent must"},
		{"negative minimum", "min_allowed_comment_lines = 1", "min_allowed_comment_lines = -1", "nonnegative"},
		{"wrong denominator", `denominator = "comment_and_code_lines"`, `denominator = "effective_code_lines"`, "budget requires"},
		{"wrong rounding", `rounding = "ceil"`, `rounding = "floor"`, "budget requires"},
		{"cross kind", "max_comment_percent = 20", "max_comment_percent = 20\nmax_column = 80", "not allowed"},
		{"missing nested", "require_name_prefix = true", "", "require_name_prefix is required"},
		{"bad regex", `line_pattern = '\S.*'`, `line_pattern = '['`, "line_pattern"},
		{"wrong style", `style = "line"`, `style = "block"`, "style must"},
		{"empty positions", `positions = ["doc", "body", "trailing", "file"]`, `positions = []`, "must not be empty"},
		{"zero column", "max_column = 80", "max_column = 0", "positive"},
		{"missing empty allowlist", "allowed_directories = []", "", "allowed_directories is required"},
		{"parent path", `allowed_paths = ["README.md"]`, `allowed_paths = ["../README.md"]`, "project-relative"},
		{"absolute path", `allowed_paths = ["README.md"]`, `allowed_paths = ["/README.md"]`, "project-relative"},
		{"wildcard", `allowed_paths = ["README.md"]`, `allowed_paths = ["*.md"]`, "project-relative"},
		{"duplicate extension", `extensions = [".md"]`, `extensions = [".md", ".MD"]`, "duplicate extension"},
		{"directory links", "follow_directory_symlinks = false", "follow_directory_symlinks = true", "not supported"},
		{"allow default", "\ndefault = \"deny\"", "\ndefault = \"allow\"", "default=deny"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(string(original), tc.old) {
				t.Fatal("fixture replacement missing")
			}
			data := strings.Replace(string(original), tc.old, tc.replacement, 1)
			file := filepath.Join(t.TempDir(), "rules.toml")
			if err := os.WriteFile(file, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(file)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}
