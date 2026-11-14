package checker

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/echozyr2001/go-static-checker/internal/analysis"
	"github.com/echozyr2001/go-static-checker/internal/config"
)

// ExportFormal writes fresh artifacts; generation does not run either prover.
func (r Report) ExportFormal(c config.Config, directory string) error {
	var models []analysis.FormalModel
	for _, rule := range c.Rules {
		if rule.Contract == nil {
			continue
		}
		contract := rule.Contract
		generated, err := analysis.FormalModels(r.callModel, analysis.Policy{RuleID: rule.ID, Entries: contract.Entries,
			Forbidden: contract.Forbidden, Trusted: contract.Trusted, OnUnknown: *contract.OnUnknown})
		if err != nil {
			return err
		}
		models = append(models, generated...)
	}
	if len(models) == 0 {
		return fmt.Errorf("formal export requires at least one matched call_contract entry")
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		return fmt.Errorf("create fresh formal output: %w", err)
	}
	for i, model := range models {
		manifest, err := json.MarshalIndent(model, "", "  ")
		if err != nil {
			return err
		}
		folder := filepath.Join(directory, fmt.Sprintf("contract_%03d", i))
		if err := os.Mkdir(folder, 0700); err != nil {
			return err
		}
		files := map[string]string{"Model.tla": model.TLA(), "Model.lean": model.Lean(),
			"Model.cfg":     "SPECIFICATION Spec\nINVARIANT Safe\nCHECK_DEADLOCK FALSE\n",
			"manifest.json": string(manifest) + "\n"}
		for name, content := range files {
			if err := os.WriteFile(filepath.Join(folder, name), []byte(content), 0600); err != nil {
				return err
			}
		}
	}
	return nil
}
