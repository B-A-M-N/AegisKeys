package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"aegiskeys/internal/security"
)

var doctorJSON bool

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Run security diagnostics on the local AegisKeys setup",
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := resolvedConfigDir()
		results := security.RunDoctor(dir)
		out := buildDoctorOutput(dir, results)

		if doctorJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(out); err != nil {
				return err
			}
		} else {
			for _, r := range out.Checks {
				mark := r.Severity
				fmt.Printf("[%s] %s\n", mark, r.Message)
				if r.Fix != "" {
					fmt.Printf("       fix: %s\n", r.Fix)
				}
			}
			fmt.Println("\nOverall: " + out.Overall)
		}

		switch out.Overall {
		case "FAIL":
			os.Exit(2)
		case "WARN":
			os.Exit(1)
		default:
			return nil
		}
		return nil
	},
}

type doctorOutput struct {
	ConfigDir string              `json:"config_dir"`
	Overall   string              `json:"overall"`
	Checks    []doctorCheckOutput `json:"checks"`
}

type doctorCheckOutput struct {
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Fix      string `json:"fix,omitempty"`
}

func buildDoctorOutput(dir string, results []security.CheckResult) doctorOutput {
	hasWarn, hasFail := false, false
	out := doctorOutput{ConfigDir: dir, Overall: "OK"}
	for _, r := range results {
		mark := doctorSeverityString(r.Severity)
		switch r.Severity {
		case security.SeverityOK:
		case security.SeverityWarn:
			hasWarn = true
		case security.SeverityFail:
			hasFail = true
		}
		out.Checks = append(out.Checks, doctorCheckOutput{
			Severity: mark,
			Message:  r.Message,
			Fix:      r.Fix,
		})
	}
	switch {
	case hasFail:
		out.Overall = "FAIL"
	case hasWarn:
		out.Overall = "WARN"
	}
	return out
}

func doctorSeverityString(s security.Severity) string {
	switch s {
	case security.SeverityOK:
		return "OK"
	case security.SeverityWarn:
		return "WARN"
	case security.SeverityFail:
		return "FAIL"
	default:
		return "UNKNOWN"
	}
}

func init() {
	doctorCmd.Flags().BoolVar(&doctorJSON, "json", false, "print diagnostics as JSON")
	rootCmd.AddCommand(doctorCmd)
}
