package checker

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/echozyr2001/go-static-checker/internal/config"
)

func withinDirectories(path string, dirs []string) bool {
	for _, dir := range dirs {
		if path == dir || strings.HasPrefix(path, dir+"/") {
			return true
		}
	}
	return false
}

func checkFilePolicy(root string, r config.Rule) ([]Diagnostic, error) {
	f := r.Files
	matches := func(name string) bool {
		ext := filepath.Ext(name)
		for _, expected := range f.Extensions {
			if *f.ExtensionCaseSensitive && ext == expected ||
				!*f.ExtensionCaseSensitive && strings.EqualFold(ext, expected) {
				return true
			}
		}
		return false
	}
	var diagnostics []Diagnostic
	err := filepath.WalkDir(root, func(filename string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, filename)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == "." {
			return nil
		}
		if withinDirectories(relative, f.ExcludedDirectories) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		selected := matches(relative)
		if d.Type()&os.ModeSymlink != 0 {
			info, err := os.Stat(filename)
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			target, err := filepath.EvalSymlinks(filename)
			if err != nil {
				return err
			}
			selected = selected || matches(target)
		}
		if selected && !slices.Contains(f.AllowedPaths, relative) &&
			!withinDirectories(relative, f.AllowedDirectories) {
			diagnostics = append(diagnostics, Diagnostic{RuleID: r.ID,
				Code: "forbidden_file_location", Severity: r.Severity,
				Message: fmt.Sprintf("file %q is not in an allowed location", relative), File: relative})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("rule %s: scan files: %w", r.ID, err)
	}
	return diagnostics, nil
}
