package checker

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/mod/modfile"
)

// projectInputs selects explicit workspace members or an isolated module.
func projectInputs(root string) (patterns []string, modules map[string]bool, work string, err error) {
	work = filepath.Join(root, "go.work")
	data, err := os.ReadFile(work)
	if err == nil {
		file, parseErr := modfile.ParseWork(work, data, nil)
		if parseErr != nil {
			return nil, nil, "", parseErr
		}
		if len(file.Use) == 0 {
			return nil, nil, "", fmt.Errorf("%s: no workspace modules", work)
		}
		modules = map[string]bool{}
		for _, use := range file.Use {
			dir := use.Path
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(root, dir)
			}
			name := filepath.Join(dir, "go.mod")
			if _, err := os.Stat(name); err != nil {
				return nil, nil, "", fmt.Errorf("workspace module: %w", err)
			}
			modules[name] = true
			patterns = append(patterns, filepath.ToSlash(dir)+"/...")
		}
		return patterns, modules, work, nil
	}
	if !os.IsNotExist(err) {
		return nil, nil, "", err
	}
	name := filepath.Join(root, "go.mod")
	if _, err := os.Stat(name); err != nil {
		return nil, nil, "", err
	}
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "vendor") {
			return filepath.SkipDir
		}
		if !d.IsDir() && d.Name() == "go.mod" && p != name {
			return fmt.Errorf("nested modules are unsupported: %s", p)
		}
		return nil
	})
	if err != nil {
		return nil, nil, "", err
	}
	return []string{"./..."}, map[string]bool{name: true}, "off", nil
}
