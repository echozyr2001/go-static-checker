package config

import (
	"fmt"
	"io/fs"
	"strings"
)

// FilePolicy restricts file locations by extension and project-relative paths.
type FilePolicy struct {
	Extensions              []string `toml:"extensions"`
	ExtensionCaseSensitive  *bool    `toml:"extension_case_sensitive"`
	Default                 *string  `toml:"default"`
	AllowedPaths            []string `toml:"allowed_paths"`
	AllowedDirectories      []string `toml:"allowed_directories"`
	ExcludedDirectories     []string `toml:"excluded_directories"`
	FollowDirectorySymlinks *bool    `toml:"follow_directory_symlinks"`
}

func (f FilePolicy) validate() error {
	if err := requiredFields(f, []string{"extensions", "extension_case_sensitive", "default", "allowed_paths",
		"allowed_directories", "excluded_directories", "follow_directory_symlinks"}); err != nil {
		return err
	}
	if *f.Default != "deny" {
		return fmt.Errorf("file_policy supports default=deny only")
	}
	if *f.FollowDirectorySymlinks {
		return fmt.Errorf("follow_directory_symlinks=true is not supported")
	}
	if len(f.Extensions) == 0 {
		return fmt.Errorf("extensions must not be empty")
	}
	seen := map[string]bool{}
	for _, ext := range f.Extensions {
		key := ext
		if !*f.ExtensionCaseSensitive {
			key = strings.ToLower(key)
		}
		if len(ext) < 2 || ext[0] != '.' || strings.ContainsAny(ext[1:], "./\\:*?[]\x00") || seen[key] {
			return fmt.Errorf("invalid or duplicate extension %q", ext)
		}
		seen[key] = true
	}
	for _, paths := range [][]string{f.AllowedPaths, f.AllowedDirectories, f.ExcludedDirectories} {
		seen := map[string]bool{}
		for _, p := range paths {
			if p == "." || !fs.ValidPath(p) || strings.ContainsAny(p, "\\:*?[]\x00") || seen[p] {
				return fmt.Errorf("invalid or duplicate project-relative path %q", p)
			}
			seen[p] = true
		}
	}
	return nil
}
