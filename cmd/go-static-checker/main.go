package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/echozyr2001/go-static-checker/internal/checker"
	"github.com/echozyr2001/go-static-checker/internal/config"
	"io"
	"os"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("go-static-checker", flag.ContinueOnError)
	flags.SetOutput(stderr)
	filename := flags.String("config", "rules.toml", "TOML rule file")
	format := flags.String("format", "text", "report format: text or json")
	formal := flags.String("export-formal", "", "export TLA+/Lean artifacts to a new directory; does not run provers")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 || (*format != "text" && *format != "json") {
		fmt.Fprintln(stderr, "expected -config FILE -format text|json, without positional arguments")
		return 2
	}
	c, err := config.Load(*filename)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	report, err := checker.Run(context.Background(), c)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if *formal != "" {
		if err := report.ExportFormal(c, *formal); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
	}
	if *format == "json" {
		err = json.NewEncoder(stdout).Encode(report)
	} else {
		for _, d := range report.Diagnostics {
			_, err = fmt.Fprintf(stdout, "%s:%d:%d: %s %s %s: %s\n",
				d.File, d.Line, d.Column, d.Severity, d.RuleID, d.Code, d.Message)
			if err != nil {
				break
			}
		}
		if err == nil {
			for _, result := range report.Verifications {
				var assumptions []string
				for _, assumption := range result.Assumptions {
					assumptions = append(assumptions, assumption.Symbol)
				}
				_, err = fmt.Fprintf(stdout, "%s %s: %s (complete=%t, assumptions=%v)\n",
					result.RuleID, result.Entry, result.Status, result.Complete, assumptions)
				if err != nil {
					break
				}
			}
		}
		if err == nil {
			_, err = fmt.Fprintf(stdout, "Checked %d packages, %d files (%s/%s, CGO_ENABLED=%s, %s, tags=%v)\n",
				report.Packages, report.Files, report.Environment["GOOS"], report.Environment["GOARCH"],
				report.Environment["CGO_ENABLED"], report.Environment["GOVERSION"], report.BuildTags)
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	return report.ExitCode()
}
