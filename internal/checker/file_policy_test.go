package checker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilePolicySymlinks(t *testing.T) {
	c := commentFixture(t, "DOC001", "package sample\n")
	write(t, c.Project.Root, "README.md", "text")
	external := t.TempDir()
	write(t, external, "guide.md", "text")
	for name, target := range map[string]string{
		"alias":     filepath.Join(external, "guide.md"),
		"linked.md": filepath.Join(c.Project.Root, "README.md"),
		"directory": external,
	} {
		if err := os.Symlink(target, filepath.Join(c.Project.Root, name)); err != nil {
			t.Fatal(err)
		}
	}
	ds, err := checkFilePolicy(c.Project.Root, c.Rules[0])
	if err != nil || len(ds) != 2 || ds[0].File != "alias" || ds[1].File != "linked.md" {
		t.Fatalf("%+v %v", ds, err)
	}
	if err := os.Symlink(filepath.Join(external, "missing.md"), filepath.Join(c.Project.Root, "broken.md")); err != nil {
		t.Fatal(err)
	}
	_, err = checkFilePolicy(c.Project.Root, c.Rules[0])
	if err == nil || !strings.Contains(err.Error(), "broken.md") {
		t.Fatalf("got %v", err)
	}
}

func TestFilePolicyPathBoundaries(t *testing.T) {
	c := commentFixture(t, "DOC001", "package sample\n")
	c.Rules[0].Files.AllowedDirectories = []string{"docs"}
	for _, name := range []string{"readme.MD", "docs/guide.md", "docs-old/guide.md", ".hidden/guide.md"} {
		write(t, c.Project.Root, name, "text")
	}
	ds, err := checkFilePolicy(c.Project.Root, c.Rules[0])
	if err != nil || len(ds) != 3 {
		t.Fatalf("%+v %v", ds, err)
	}
	if ds[0].File != ".hidden/guide.md" || ds[1].File != "docs-old/guide.md" || ds[2].File != "readme.MD" {
		t.Fatal(ds)
	}
}
