package cli

import (
	"github.com/bytter/autoresearch/internal/output"
	"github.com/bytter/autoresearch/internal/readmodel"
	"github.com/spf13/cobra"
)

func experimentPreflightCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "preflight <E-id>",
		Short: "Check experiment design quality before implementation",
		Long: `Preflight a designed experiment without mutating state. The report checks
that the experiment measures the predicted instrument and goal constraints,
that cited lessons are usable, that mechanism claims have an evidence path,
and that automatic baseline resolution is not silently missing.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			s, err := openStore()
			if err != nil {
				return err
			}
			report, err := readmodel.PreflightExperiment(s, args[0])
			if err != nil {
				return err
			}
			if w.IsJSON() {
				return w.JSON(report)
			}
			renderExperimentPreflightText(w, report)
			return nil
		},
	}
}

func renderExperimentPreflightText(w *output.Writer, report *readmodel.ExperimentPreflightReport) {
	status := "ok"
	if !report.OK {
		status = "issues"
	}
	w.Textf("preflight %s: %s (%d errors, %d warnings)\n", report.Experiment, status, report.Errors, report.Warnings)
	if report.Hypothesis != "" {
		w.Textf("hypothesis: %s\n", report.Hypothesis)
	}
	if report.Baseline != nil {
		w.Textf("baseline:   %s", report.Baseline.ExperimentID)
		if report.Baseline.Source != "" {
			w.Textf(" source=%s", report.Baseline.Source)
		}
		if report.Baseline.Note != "" {
			w.Textf(" note=%q", report.Baseline.Note)
		}
		w.Textln("")
	}
	for _, issue := range report.Issues {
		subject := issue.Subject
		if subject != "" {
			subject = " " + subject
		}
		w.Textf("  - [%s] %s%s: %s\n", issue.Severity, issue.Code, subject, issue.Message)
		if issue.Recommendation != "" {
			w.Textf("      recommendation: %s\n", issue.Recommendation)
		}
	}
	if report.OK && len(report.Issues) == 0 {
		w.Textln("ready for implementation")
	}
}
