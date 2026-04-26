package cli

import (
	"fmt"

	"github.com/bytter/autoresearch/internal/output"
	"github.com/bytter/autoresearch/internal/readmodel"
	"github.com/spf13/cobra"
)

func conclusionLintCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "lint <C-id>",
		Short: "Lint conclusion citations, refs, constraints, and mechanism evidence",
		Long: `Lint a persisted conclusion without mutating state. The report checks
cited observations, candidate and baseline refs, required goal constraints,
same-scope observations that were not cited, and mechanism/counter claims
without supporting cited evidence artifacts.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			s, err := openStore()
			if err != nil {
				return err
			}
			report, err := readmodel.LintConclusion(s, args[0])
			if err != nil {
				return err
			}
			if w.IsJSON() {
				return w.JSON(report)
			}
			renderConclusionLintText(w, report)
			return nil
		},
	}
}

func renderConclusionLintText(w *output.Writer, report *readmodel.ConclusionLintReport) {
	status := "ok"
	if !report.OK {
		status = "issues"
	}
	w.Textf("lint %s: %s (%d errors, %d warnings)\n", report.Conclusion, status, report.Errors, report.Warnings)
	for _, issue := range report.Issues {
		subject := issue.Subject
		if subject != "" {
			subject = " " + subject
		}
		w.Textf("  - [%s] %s%s: %s\n", issue.Severity, issue.Code, subject, issue.Message)
	}
}

func renderConclusionLintWarningText(w *output.Writer, report *readmodel.ConclusionLintReport) {
	if report == nil || report.OK {
		return
	}
	w.Textln("")
	w.Textf("LINT WARNINGS for %s (%d errors, %d warnings):\n", report.Conclusion, report.Errors, report.Warnings)
	for _, issue := range report.Issues {
		subject := issue.Subject
		if subject != "" {
			subject = " " + subject
		}
		w.Textf("  - [%s] %s%s: %s\n", issue.Severity, issue.Code, subject, issue.Message)
	}
	w.Textln(fmt.Sprintf("Run `autoresearch conclusion lint %s --json` for the structured report.", report.Conclusion))
}
