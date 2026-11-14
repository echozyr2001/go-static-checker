package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func repositoryRuleText(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../../rules.toml")
	if err != nil {
		t.Fatal(err)
	}
	var entry ruleFile
	if _, err := toml.Decode(string(data), &entry); err != nil {
		t.Fatal(err)
	}
	_, project, ok := strings.Cut(string(data), "[project]")
	if !ok {
		t.Fatal("repository configuration has no project table")
	}
	text := "schema_version = 1\n[project]" + project
	for _, name := range entry.Includes {
		part, err := os.ReadFile(filepath.Join("../..", name))
		if err != nil {
			t.Fatal(err)
		}
		text += "\n" + strings.Replace(string(part), "schema_version = 1", "", 1)
	}
	return text
}
