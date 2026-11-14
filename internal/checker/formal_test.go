package checker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/echozyr2001/go-static-checker/internal/analysis"
)

func TestFormalExportFromSource(t *testing.T) {
	c := contractFixture(t, "package app\nfunc Entry(){helper()}\nfunc helper(){panic(1)}\n", `"builtin::panic"`, "", "error")
	r, err := Run(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(t.TempDir(), "artifacts")
	if err := r.ExportFormal(c, directory); err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(directory, "contract_000", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var model analysis.FormalModel
	if err := json.Unmarshal(manifest, &model); err != nil {
		t.Fatal(err)
	}
	if model.Result.Status != "violated" || len(model.Nodes) != 3 || model.Nodes[2].Symbol != "builtin::panic" || model.Nodes[2].Location.Line != 3 {
		t.Fatalf("%+v", model)
	}
	if err := r.ExportFormal(c, directory); err == nil {
		t.Fatal("must not overwrite existing artifacts")
	}
}

func TestFormalUnknownCreatesNoArtifacts(t *testing.T) {
	c := contractFixture(t, "package app\nfunc Entry(f func()){f()}\n", `"os::Exit"`, "", "warning")
	r, err := Run(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(t.TempDir(), "artifacts")
	if err := r.ExportFormal(c, directory); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatal(err)
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatalf("artifacts should not exist: %v", err)
	}
}
