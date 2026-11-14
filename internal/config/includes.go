package config

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type ruleFile struct {
	SchemaVersion int      `toml:"schema_version"`
	Project       *Project `toml:"project"`
	Includes      []string `toml:"includes"`
	Rules         []Rule   `toml:"rules"`
}

type ruleLoader struct {
	config Config
	active map[string]bool
	files  map[string]string
	ids    map[string]string
}

// Load combines a rule file and its includes under one project root.
func Load(filename string) (Config, error) {
	loader := ruleLoader{active: map[string]bool{}, files: map[string]string{}, ids: map[string]string{}}
	if err := loader.read(filename, true); err != nil {
		return Config{}, err
	}
	if err := loader.config.Validate(); err != nil {
		return Config{}, fmt.Errorf("%s: %w", filename, err)
	}
	return loader.config, nil
}

func (l *ruleLoader) read(filename string, entry bool) error {
	absolute, err := filepath.Abs(filename)
	if err != nil {
		return fmt.Errorf("%s: %w", filename, err)
	}
	if err := l.readFile(absolute, entry); err != nil {
		return fmt.Errorf("%s: %w", absolute, err)
	}
	return nil
}

func (l *ruleLoader) readFile(filename string, entry bool) error {
	canonical, err := filepath.EvalSymlinks(filename)
	if err != nil {
		return err
	}
	if l.active[canonical] {
		return fmt.Errorf("include cycle through %s", l.files[canonical])
	}
	if previous, ok := l.files[canonical]; ok {
		return fmt.Errorf("file already included as %s", previous)
	}
	l.files[canonical] = filename
	l.active[canonical] = true
	defer delete(l.active, canonical)
	var file ruleFile
	metadata, err := toml.DecodeFile(filename, &file)
	if err != nil {
		return err
	}
	if keys := metadata.Undecoded(); len(keys) > 0 {
		return fmt.Errorf("unknown configuration fields: %v", keys)
	}
	if file.SchemaVersion != 1 {
		return fmt.Errorf("schema_version must be 1")
	}
	if entry {
		if file.Project == nil {
			return fmt.Errorf("project is required in entry file")
		}
		l.config = Config{SchemaVersion: file.SchemaVersion, Project: *file.Project}
		if l.config.Project.Root != "" && !filepath.IsAbs(l.config.Project.Root) {
			l.config.Project.Root = filepath.Join(filepath.Dir(filename), l.config.Project.Root)
		}
		if l.config.Project.Root != "" {
			l.config.Project.Root = filepath.Clean(l.config.Project.Root)
		}
	} else if file.Project != nil {
		return fmt.Errorf("project is only allowed in entry file")
	}
	if len(file.Rules) > 0 {
		local := Config{SchemaVersion: file.SchemaVersion, Project: l.config.Project, Rules: file.Rules}
		if err := local.Validate(); err != nil {
			return err
		}
	}
	for _, rule := range file.Rules {
		if previous, ok := l.ids[rule.ID]; ok {
			return fmt.Errorf("duplicate rule id %q; first defined in %s", rule.ID, previous)
		}
		l.ids[rule.ID] = filename
		l.config.Rules = append(l.config.Rules, rule)
	}
	for _, include := range file.Includes {
		if strings.TrimSpace(include) == "" {
			return fmt.Errorf("empty include path")
		}
		if !filepath.IsAbs(include) {
			include = filepath.Join(filepath.Dir(filename), include)
		}
		if err := l.read(include, false); err != nil {
			return err
		}
	}
	return nil
}
