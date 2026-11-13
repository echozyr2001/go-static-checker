package checker

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/echozyr2001/go-static-checker/internal/config"
)

func checkFileLayout(projectRoot string, r config.Rule) ([]Diagnostic, error) {
	root, err := os.OpenRoot(projectRoot)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	var diagnostics []Diagnostic
	for _, group := range []struct {
		paths     []string
		directory bool
		kind      string
	}{
		{r.Layout.RequiredFiles, false, "file"},
		{r.Layout.RequiredDirectories, true, "directory"},
	} {
		for _, name := range group.paths {
			info, err := root.Stat(name)
			code, message := "", ""
			switch {
			case errors.Is(err, fs.ErrNotExist):
				code = "missing_required_" + group.kind
				message = fmt.Sprintf("required %s %q is missing", group.kind, name)
			case err != nil:
				return nil, fmt.Errorf("rule %s: inspect %s: %w", r.ID, name, err)
			case group.directory && !info.IsDir(), !group.directory && !info.Mode().IsRegular():
				code = "invalid_required_" + group.kind
				message = fmt.Sprintf("%q must be a %s", name, group.kind)
			}
			if code != "" {
				diagnostics = append(diagnostics, Diagnostic{
					RuleID: r.ID, Code: code, Severity: r.Severity, Message: message, File: name,
				})
			}
		}
	}
	return diagnostics, nil
}
