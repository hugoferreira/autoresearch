package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/output"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/spf13/cobra"
)

type observationShowJSON struct {
	*entity.Observation
	Raw           *observationRaw `json:"raw,omitempty"`
	RawReadIssues []string        `json:"raw_read_issues"`
}

type observationRaw struct {
	Artifact entity.Artifact  `json:"artifact"`
	Samples  []map[string]any `json:"samples,omitempty"`
}

func observationCommands() []*cobra.Command {
	o := &cobra.Command{
		Use:   "observation",
		Short: "Inspect recorded observations",
	}
	o.AddCommand(observationShowCmd())
	return []*cobra.Command{o}
}

func observationShowCmd() *cobra.Command {
	var includeRaw bool
	c := &cobra.Command{
		Use:   "show <O-id>",
		Short: "Show a single observation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			s, err := openStore()
			if err != nil {
				return err
			}
			o, err := s.ReadObservation(args[0])
			if err != nil {
				return err
			}
			if w.IsJSON() {
				if !includeRaw {
					return w.JSON(o)
				}
				raw, issues := loadObservationRaw(s, o)
				return w.JSON(observationShowJSON{
					Observation:   o,
					Raw:           raw,
					RawReadIssues: issues,
				})
			}
			renderObservationShowText(w, o, nil, nil)
			if includeRaw {
				raw, issues := loadObservationRaw(s, o)
				renderObservationShowText(w, nil, raw, issues)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&includeRaw, "include-raw", false, "decode bounded raw sample records from the primary observation artifact")
	return c
}

func loadObservationRaw(s *store.Store, o *entity.Observation) (*observationRaw, []string) {
	issues := []string{}
	if o == nil {
		return nil, append(issues, "observation is nil")
	}
	primary := o.Primary()
	if primary == nil {
		return nil, append(issues, fmt.Sprintf("observation %s has no artifacts", o.ID))
	}

	artifact := *primary
	data, issue := readObservationRawArtifact(s, &artifact)
	raw := &observationRaw{Artifact: artifact}
	if issue != "" {
		return raw, append(issues, issue)
	}

	samples, issue := decodeObservationRawSamples(data, artifact, o)
	if issue != "" {
		issues = append(issues, issue)
	}
	raw.Samples = samples
	return raw, issues
}

func readObservationRawArtifact(s *store.Store, artifact *entity.Artifact) ([]byte, string) {
	if strings.TrimSpace(artifact.Path) == "" {
		if strings.TrimSpace(artifact.SHA) == "" {
			return nil, "primary artifact has no path or sha"
		}
		sha, rel, abs, err := s.ArtifactLocation(artifact.SHA)
		if err != nil {
			return nil, fmt.Sprintf("resolve raw artifact %s: %v", artifact.SHA, err)
		}
		artifact.SHA = sha
		artifact.Path = rel
		return readObservationRawArtifactAt(abs, artifact)
	}
	return readObservationRawArtifactAt(filepath.Join(s.DirPath(), artifact.Path), artifact)
}

func readObservationRawArtifactAt(abs string, artifact *entity.Artifact) ([]byte, string) {
	fi, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Sprintf("raw artifact %s is unreadable: %v", observationArtifactLabel(*artifact), err)
	}
	if artifact.Bytes == 0 {
		artifact.Bytes = fi.Size()
	}
	if fi.Size() > defaultShowMaxBytes {
		return nil, fmt.Sprintf("raw artifact %s is %d bytes, exceeds max_bytes=%d", observationArtifactLabel(*artifact), fi.Size(), defaultShowMaxBytes)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Sprintf("raw artifact %s is unreadable: %v", observationArtifactLabel(*artifact), err)
	}
	return data, ""
}

func observationArtifactLabel(a entity.Artifact) string {
	if a.SHA != "" {
		return shortSHA(a.SHA)
	}
	if a.Path != "" {
		return a.Path
	}
	return a.Name
}

func decodeObservationRawSamples(data []byte, artifact entity.Artifact, o *entity.Observation) ([]map[string]any, string) {
	trimmed := bytes.TrimSpace(data)
	if observationArtifactLooksJSON(artifact, trimmed) {
		var doc any
		dec := json.NewDecoder(bytes.NewReader(trimmed))
		dec.UseNumber()
		if err := dec.Decode(&doc); err != nil {
			return nil, fmt.Sprintf("raw artifact %s is invalid JSON: %v", observationArtifactLabel(artifact), err)
		}
		return normalizeObservationRawJSONSamples(doc, artifact)
	}
	return []map[string]any{synthesizeObservationTextSample(data, o)}, ""
}

func observationArtifactLooksJSON(artifact entity.Artifact, data []byte) bool {
	mime := strings.ToLower(artifact.Mime)
	path := strings.ToLower(artifact.Path)
	return strings.Contains(mime, "json") ||
		strings.HasSuffix(path, ".json") ||
		bytes.HasPrefix(data, []byte("{")) ||
		bytes.HasPrefix(data, []byte("["))
}

func normalizeObservationRawJSONSamples(doc any, artifact entity.Artifact) ([]map[string]any, string) {
	var rawSamples any
	switch v := doc.(type) {
	case map[string]any:
		var ok bool
		rawSamples, ok = v["samples"]
		if !ok {
			return nil, fmt.Sprintf("raw artifact %s has no samples array", observationArtifactLabel(artifact))
		}
	case []any:
		rawSamples = v
	default:
		return nil, fmt.Sprintf("raw artifact %s is JSON but not an object or sample array", observationArtifactLabel(artifact))
	}

	sampleArray, ok := rawSamples.([]any)
	if !ok {
		return nil, fmt.Sprintf("raw artifact %s has non-array samples", observationArtifactLabel(artifact))
	}
	samples := make([]map[string]any, 0, len(sampleArray))
	for i, sample := range sampleArray {
		normalized, ok := normalizeObservationRawSample(sample)
		if !ok {
			return nil, fmt.Sprintf("raw artifact %s sample %d is not decodable", observationArtifactLabel(artifact), i)
		}
		normalized["index"] = i
		addObservationRawPassed(normalized)
		samples = append(samples, normalized)
	}
	if len(samples) == 0 {
		return nil, fmt.Sprintf("raw artifact %s has no sample records", observationArtifactLabel(artifact))
	}
	return samples, ""
}

func normalizeObservationRawSample(sample any) (map[string]any, bool) {
	switch v := sample.(type) {
	case map[string]any:
		out := make(map[string]any, len(v)+2)
		for key, val := range v {
			out[key] = val
		}
		return out, true
	case string, bool, nil, json.Number, float64:
		return map[string]any{"value": v}, true
	default:
		return nil, false
	}
}

func addObservationRawPassed(sample map[string]any) {
	if _, ok := sample["passed"]; ok {
		return
	}
	if pass, ok := sample["pass"].(bool); ok {
		sample["passed"] = pass
		return
	}
	exit, ok := observationRawInt(sample["exit_code"])
	if ok {
		sample["passed"] = exit == 0
	}
}

func synthesizeObservationTextSample(data []byte, o *entity.Observation) map[string]any {
	sample := map[string]any{
		"index":     0,
		"stdout":    string(data),
		"exit_code": o.ExitCode,
		"passed":    o.ExitCode == 0,
	}
	if o.Pass != nil {
		sample["passed"] = *o.Pass
	}
	return sample
}

func renderObservationShowText(w *output.Writer, o *entity.Observation, raw *observationRaw, issues []string) {
	if o != nil {
		renderObservationSummaryText(w, o)
	}
	if raw != nil || len(issues) > 0 {
		renderObservationRawText(w, raw, issues)
	}
}

func renderObservationSummaryText(w *output.Writer, o *entity.Observation) {
	w.Textf("id:          %s\n", o.ID)
	w.Textf("experiment:  %s\n", o.Experiment)
	w.Textf("instrument:  %s\n", o.Instrument)
	w.Textf("value:       %g %s\n", o.Value, o.Unit)
	w.Textf("samples:     %d\n", o.Samples)
	w.Textf("measured_at: %s\n", o.MeasuredAt.UTC().Format(time.RFC3339))
	w.Textf("author:      %s\n", o.Author)
	if o.CILow != nil && o.CIHigh != nil {
		w.Textf("ci:          [%g, %g] (%s)\n", *o.CILow, *o.CIHigh, emptyDash(o.CIMethod))
	}
	if o.Pass != nil {
		w.Textf("pass:        %v\n", *o.Pass)
	}
	w.Textf("exit:        %d\n", o.ExitCode)
	if o.Worktree != "" {
		w.Textf("worktree:    %s\n", o.Worktree)
	}
	if o.CandidateRef != "" {
		w.Textf("candidate:   %s", o.CandidateRef)
		if o.CandidateSHA != "" {
			w.Textf(" (%s)", shortSHA(o.CandidateSHA))
		}
		w.Textln("")
	}
	if o.BaselineSHA != "" {
		w.Textf("baseline:    %s\n", shortSHA(o.BaselineSHA))
	}
	if len(o.Artifacts) > 0 {
		w.Textln("artifacts:")
		for _, a := range o.Artifacts {
			w.Textf("  - %-12s %s  (%s)  sha=%s\n", a.Name, a.Path, humanBytes(a.Bytes), shortSHA(a.SHA))
		}
	}
	if len(o.EvidenceFailures) > 0 {
		w.Textln("evidence failures:")
		for _, failure := range o.EvidenceFailures {
			w.Textf("  - %s\n", formatEvidenceFailure(failure))
		}
	}
	if len(o.PerSample) > 0 {
		w.Textf("per_sample:  %v\n", o.PerSample)
	}
	if strings.TrimSpace(o.Command) != "" {
		w.Textf("command:     %s\n", o.Command)
	}
	if len(o.Aux) > 0 {
		w.Textln("aux:")
		for _, key := range sortedObservationAuxKeys(o.Aux) {
			w.Textf("  %s: %v\n", key, o.Aux[key])
		}
	}
}

func renderObservationRawText(w *output.Writer, raw *observationRaw, issues []string) {
	w.Textln("raw:")
	if raw != nil {
		a := raw.Artifact
		w.Textf("  artifact: %s %s (%s) sha=%s\n", a.Name, a.Path, humanBytes(a.Bytes), shortSHA(a.SHA))
		if len(raw.Samples) > 0 {
			w.Textln("  samples:")
			for _, sample := range raw.Samples {
				w.Textf("    - %s\n", formatObservationRawSampleLine(sample))
				if stdout, ok := observationRawString(sample["stdout"]); ok {
					w.Textf("      stdout: %s\n", truncate(oneLine(stdout), 160))
				}
			}
		}
	}
	if len(issues) > 0 {
		w.Textln("  raw_read_issues:")
		for _, issue := range issues {
			w.Textf("    - %s\n", issue)
		}
	}
}

func formatObservationRawSampleLine(sample map[string]any) string {
	parts := []string{"index=" + fmt.Sprint(sample["index"])}
	for _, key := range []string{"run", "exit_code", "passed", "value", "elapsed_s"} {
		if val, ok := sample[key]; ok {
			parts = append(parts, fmt.Sprintf("%s=%v", key, val))
		}
	}
	return strings.Join(parts, "  ")
}

func sortedObservationAuxKeys(aux map[string]any) []string {
	keys := make([]string, 0, len(aux))
	for key := range aux {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func observationRawInt(v any) (int64, bool) {
	switch x := v.(type) {
	case int:
		return int64(x), true
	case int64:
		return x, true
	case float64:
		return int64(x), true
	case json.Number:
		i, err := strconv.ParseInt(x.String(), 10, 64)
		if err == nil {
			return i, true
		}
		f, err := strconv.ParseFloat(x.String(), 64)
		if err != nil {
			return 0, false
		}
		return int64(f), true
	default:
		return 0, false
	}
}

func observationRawString(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}
