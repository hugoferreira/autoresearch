package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/output"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/spf13/cobra"
)

// defaultShowMaxBytes caps `artifact show` output for agent safety. It can be
// raised per-invocation or bypassed with --all.
const defaultShowMaxBytes = 262144

// printLimits makes every default explicit before the content. Agents reading
// the output cannot miss that the result is bounded — "defaults applied: ..."
// appears as the first line in text mode, and a `limits` key in JSON mode.
func printLimits(w *output.Writer, limits map[string]any) {
	if w.IsJSON() {
		return
	}
	keys := make([]string, 0, len(limits))
	for k := range limits {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, limits[k]))
	}
	w.Textf("[defaults applied: %s]  (override with flags; see --help)\n", strings.Join(parts, " "))
}

func artifactCommands() []*cobra.Command {
	a := &cobra.Command{
		Use:   "artifact",
		Short: "List and navigate instrument-produced artifacts",
	}
	a.AddCommand(
		artifactListCmd(),
		artifactStatCmd(),
		artifactPathCmd(),
		artifactHeadCmd(),
		artifactTailCmd(),
		artifactRangeCmd(),
		artifactGrepCmd(),
		artifactDiffCmd(),
		artifactShowCmd(),
	)
	return []*cobra.Command{a}
}

// ---- helpers ----

func resolveArtifact(s *store.Store, shaOrPrefix string) (sha, rel, abs string, err error) {
	return s.ArtifactLocation(shaOrPrefix)
}

type fileStat struct {
	Lines int
	Bytes int64
}

func statFile(path string) (fileStat, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return fileStat{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return fileStat{}, err
	}
	defer f.Close()
	lines := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		lines++
	}
	if err := scanner.Err(); err != nil {
		return fileStat{}, err
	}
	return fileStat{Lines: lines, Bytes: fi.Size()}, nil
}

// findArtifactOwners reports observations that reference a given sha, so
// `artifact stat` can show provenance.
func findArtifactOwners(s *store.Store, sha string) []string {
	obs, err := s.ListObservations()
	if err != nil {
		return nil
	}
	var out []string
	for _, o := range obs {
		for _, a := range o.Artifacts {
			if a.SHA == sha {
				out = append(out, fmt.Sprintf("%s/%s", o.ID, a.Name))
			}
		}
	}
	return out
}

// ---- list ----

func artifactListCmd() *cobra.Command {
	var experiment, observation, goalFlag string
	c := &cobra.Command{
		Use:   "list",
		Short: "List artifacts produced by observations",
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			s, err := openStore()
			if err != nil {
				return err
			}
			scope, err := resolveGoalScope(s, goalFlag)
			if err != nil {
				return err
			}
			resolver := newGoalScopeResolver(s, scope)

			var obs []*entity.Observation
			switch {
			case observation != "":
				o, err := s.ReadObservation(observation)
				if err != nil {
					return err
				}
				obs = []*entity.Observation{o}
			case experiment != "":
				obs, err = s.ListObservationsForExperiment(experiment)
				if err != nil {
					return err
				}
			default:
				obs, err = s.ListObservations()
				if err != nil {
					return err
				}
			}
			obs, err = resolver.filterObservations(obs)
			if err != nil {
				return err
			}

			type row struct {
				Observation string `json:"observation"`
				Instrument  string `json:"instrument"`
				Name        string `json:"name"`
				SHA         string `json:"sha"`
				Bytes       int64  `json:"bytes"`
				Path        string `json:"path"`
			}
			var rows []row
			for _, o := range obs {
				for _, a := range o.Artifacts {
					rows = append(rows, row{
						Observation: o.ID,
						Instrument:  o.Instrument,
						Name:        a.Name,
						SHA:         a.SHA,
						Bytes:       a.Bytes,
						Path:        a.Path,
					})
				}
			}
			if w.IsJSON() {
				return w.JSON(mergeGoalScopePayload(map[string]any{"artifacts": rows}, scope))
			}
			if len(rows) == 0 {
				w.Textln("(no artifacts)")
				return nil
			}
			for _, r := range rows {
				shaShort := r.SHA
				if len(shaShort) > 12 {
					shaShort = shaShort[:12]
				}
				w.Textf("  %-10s %-14s %-8s  %s…  %10d B  %s\n",
					r.Observation, r.Instrument, r.Name, shaShort, r.Bytes, r.Path)
			}
			return nil
		},
	}
	c.Flags().StringVar(&experiment, "experiment", "", "filter by experiment id")
	c.Flags().StringVar(&observation, "observation", "", "filter by observation id")
	c.Flags().StringVar(&goalFlag, "goal", "", "goal to scope the list to (defaults to active goal; use 'all' for every goal)")
	return c
}

// ---- stat ----

func artifactStatCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stat <sha>",
		Short: "Show size, line count, and provenance for an artifact",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			s, err := openStore()
			if err != nil {
				return err
			}
			sha, rel, abs, err := resolveArtifact(s, args[0])
			if err != nil {
				return err
			}
			st, err := statFile(abs)
			if err != nil {
				return err
			}
			owners := findArtifactOwners(s, sha)
			payload := map[string]any{
				"sha":    sha,
				"path":   rel,
				"bytes":  st.Bytes,
				"lines":  st.Lines,
				"owners": owners,
			}
			if w.IsJSON() {
				return w.JSON(payload)
			}
			w.Textf("sha:    %s\n", sha)
			w.Textf("path:   %s\n", rel)
			w.Textf("bytes:  %d\n", st.Bytes)
			w.Textf("lines:  %d\n", st.Lines)
			if len(owners) > 0 {
				w.Textf("owners: %s\n", strings.Join(owners, ", "))
			}
			return nil
		},
	}
}

// ---- path ----

func artifactPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path <sha>",
		Short: "Print the absolute filesystem path of an artifact",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			s, err := openStore()
			if err != nil {
				return err
			}
			_, _, abs, err := resolveArtifact(s, args[0])
			if err != nil {
				return err
			}
			return w.Emit(abs, map[string]string{"path": abs})
		},
	}
}

// ---- head / tail / range ----

func readSomeLines(path string, n int) ([]string, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	lines := make([]string, 0, n)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() && len(lines) < n {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return nil, 0, err
	}
	st, _ := statFile(path)
	return lines, st.Lines, nil
}

func readLastLines(path string, n int) ([]string, int, error) {
	st, err := statFile(path)
	if err != nil {
		return nil, 0, err
	}
	from := st.Lines - n
	if from < 0 {
		from = 0
	}
	lines, _, err := readLineRange(path, from+1, st.Lines)
	return lines, st.Lines, err
}

func readLineRange(path string, from, to int) ([]string, int, error) {
	if from < 1 {
		from = 1
	}
	if to < from {
		return nil, 0, fmt.Errorf("--to (%d) must be >= --from (%d)", to, from)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	var out []string
	i := 0
	total := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		i++
		total++
		if i >= from && i <= to {
			out = append(out, sc.Text())
		}
	}
	if err := sc.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func artifactHeadCmd() *cobra.Command {
	var lines int
	c := &cobra.Command{
		Use:   "head <sha>",
		Short: "Print the first N lines of an artifact",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			s, err := openStore()
			if err != nil {
				return err
			}
			sha, rel, abs, err := resolveArtifact(s, args[0])
			if err != nil {
				return err
			}
			got, total, err := readSomeLines(abs, lines)
			if err != nil {
				return err
			}
			truncated := total > lines
			limits := map[string]any{"lines": lines}
			if w.IsJSON() {
				return w.JSON(map[string]any{
					"sha":         sha,
					"path":        rel,
					"limits":      limits,
					"total_lines": total,
					"shown_lines": len(got),
					"truncated":   truncated,
					"content":     strings.Join(got, "\n"),
				})
			}
			printLimits(w, limits)
			w.Textf("[file: %s, %d lines total]\n", rel, total)
			for _, l := range got {
				w.Textln(l)
			}
			if truncated {
				w.Textf("[truncated: showing first %d of %d lines — use --lines N or `artifact range`/`artifact grep`]\n", len(got), total)
			}
			return nil
		},
	}
	c.Flags().IntVar(&lines, "lines", 50, "number of lines to show")
	return c
}

func artifactTailCmd() *cobra.Command {
	var lines int
	c := &cobra.Command{
		Use:   "tail <sha>",
		Short: "Print the last N lines of an artifact",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			s, err := openStore()
			if err != nil {
				return err
			}
			sha, rel, abs, err := resolveArtifact(s, args[0])
			if err != nil {
				return err
			}
			got, total, err := readLastLines(abs, lines)
			if err != nil {
				return err
			}
			truncated := total > lines
			limits := map[string]any{"lines": lines}
			if w.IsJSON() {
				return w.JSON(map[string]any{
					"sha":         sha,
					"path":        rel,
					"limits":      limits,
					"total_lines": total,
					"shown_lines": len(got),
					"truncated":   truncated,
					"content":     strings.Join(got, "\n"),
				})
			}
			printLimits(w, limits)
			w.Textf("[file: %s, %d lines total]\n", rel, total)
			if truncated {
				w.Textf("[showing last %d of %d lines]\n", len(got), total)
			}
			for _, l := range got {
				w.Textln(l)
			}
			return nil
		},
	}
	c.Flags().IntVar(&lines, "lines", 50, "number of lines to show")
	return c
}

func artifactRangeCmd() *cobra.Command {
	var from, to int
	c := &cobra.Command{
		Use:   "range <sha>",
		Short: "Print a specific line range of an artifact",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			if from <= 0 || to <= 0 {
				return errors.New("--from and --to are required (1-indexed, inclusive)")
			}
			s, err := openStore()
			if err != nil {
				return err
			}
			sha, rel, abs, err := resolveArtifact(s, args[0])
			if err != nil {
				return err
			}
			got, total, err := readLineRange(abs, from, to)
			if err != nil {
				return err
			}
			if w.IsJSON() {
				return w.JSON(map[string]any{
					"sha":         sha,
					"path":        rel,
					"limits":      map[string]any{"from": from, "to": to},
					"total_lines": total,
					"shown_lines": len(got),
					"content":     strings.Join(got, "\n"),
				})
			}
			printLimits(w, map[string]any{"from": from, "to": to})
			w.Textf("[file: %s, %d lines total; showing %d–%d]\n", rel, total, from, to)
			for _, l := range got {
				w.Textln(l)
			}
			return nil
		},
	}
	c.Flags().IntVar(&from, "from", 0, "starting line (1-indexed, inclusive)")
	c.Flags().IntVar(&to, "to", 0, "ending line (1-indexed, inclusive)")
	return c
}

// ---- grep ----

type grepMatch struct {
	Line    int      `json:"line"`
	Text    string   `json:"text"`
	Context []string `json:"context,omitempty"`
}

func artifactGrepCmd() *cobra.Command {
	var (
		contextN   int
		maxMatches int
	)
	c := &cobra.Command{
		Use:   "grep <sha> <regex>",
		Short: "Regex-search an artifact; returns matches with line numbers",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			pattern := args[1]
			re, err := regexp.Compile(pattern)
			if err != nil {
				return fmt.Errorf("bad regex: %w", err)
			}
			s, err := openStore()
			if err != nil {
				return err
			}
			sha, rel, abs, err := resolveArtifact(s, args[0])
			if err != nil {
				return err
			}

			f, err := os.Open(abs)
			if err != nil {
				return err
			}
			defer f.Close()

			// Read all lines to support context windows. For text artifacts up
			// to a few hundred MB this is fine; larger files need a streaming
			// approach we'll add if it comes up.
			sc := bufio.NewScanner(f)
			sc.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
			var all []string
			for sc.Scan() {
				all = append(all, sc.Text())
			}
			if err := sc.Err(); err != nil {
				return err
			}

			var matches []grepMatch
			totalMatches := 0
			for i, line := range all {
				if !re.MatchString(line) {
					continue
				}
				totalMatches++
				if len(matches) >= maxMatches {
					continue
				}
				var ctx []string
				if contextN > 0 {
					lo := i - contextN
					if lo < 0 {
						lo = 0
					}
					hi := i + contextN
					if hi >= len(all) {
						hi = len(all) - 1
					}
					for j := lo; j <= hi; j++ {
						prefix := "  "
						if j == i {
							prefix = "> "
						}
						ctx = append(ctx, fmt.Sprintf("%s%d: %s", prefix, j+1, all[j]))
					}
				}
				matches = append(matches, grepMatch{
					Line:    i + 1,
					Text:    line,
					Context: ctx,
				})
			}

			truncated := totalMatches > len(matches)
			limits := map[string]any{"max_matches": maxMatches, "context": contextN}
			if w.IsJSON() {
				return w.JSON(map[string]any{
					"sha":           sha,
					"path":          rel,
					"pattern":       pattern,
					"limits":        limits,
					"total_matches": totalMatches,
					"shown_matches": len(matches),
					"truncated":     truncated,
					"matches":       matches,
				})
			}
			printLimits(w, limits)
			w.Textf("[file: %s, pattern: /%s/]\n", rel, pattern)
			for _, m := range matches {
				if len(m.Context) > 0 {
					for _, c := range m.Context {
						w.Textln(c)
					}
					w.Textln("--")
				} else {
					w.Textf("%d: %s\n", m.Line, m.Text)
				}
			}
			w.Textf("[matches: %d shown of %d total]\n", len(matches), totalMatches)
			if truncated {
				w.Textln("[truncated: raise --max-matches or narrow the pattern]")
			}
			return nil
		},
	}
	c.Flags().IntVar(&contextN, "context", 0, "lines of context to show around each match")
	c.Flags().IntVar(&maxMatches, "max-matches", 100, "maximum number of matches to return")
	return c
}

// ---- diff ----

func artifactDiffCmd() *cobra.Command {
	var contextN, maxLines int
	c := &cobra.Command{
		Use:   "diff <sha-a> <sha-b>",
		Short: "Unified diff between two artifacts",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			s, err := openStore()
			if err != nil {
				return err
			}
			shaA, relA, absA, err := resolveArtifact(s, args[0])
			if err != nil {
				return fmt.Errorf("a: %w", err)
			}
			shaB, relB, absB, err := resolveArtifact(s, args[1])
			if err != nil {
				return fmt.Errorf("b: %w", err)
			}
			diffBin, err := exec.LookPath("diff")
			if err != nil {
				return fmt.Errorf("diff binary not found in PATH")
			}
			lines, err := runDiff(diffBin, absA, absB, contextN)
			if err != nil {
				return err
			}
			totalLines := len(lines)
			truncated := false
			if totalLines > maxLines {
				lines = lines[:maxLines]
				truncated = true
			}
			limits := map[string]any{"context": contextN, "max_lines": maxLines}
			if w.IsJSON() {
				return w.JSON(map[string]any{
					"a":           map[string]string{"sha": shaA, "path": relA},
					"b":           map[string]string{"sha": shaB, "path": relB},
					"limits":      limits,
					"total_lines": totalLines,
					"shown_lines": len(lines),
					"truncated":   truncated,
					"diff":        strings.Join(lines, "\n"),
				})
			}
			printLimits(w, limits)
			w.Textf("--- a/%s\n+++ b/%s\n", relA, relB)
			for _, l := range lines {
				w.Textln(l)
			}
			if totalLines == 0 {
				w.Textln("[no differences]")
			}
			if truncated {
				w.Textf("[truncated: showing %d of %d diff lines — raise --max-lines]\n", len(lines), totalLines)
			}
			return nil
		},
	}
	c.Flags().IntVar(&contextN, "context", 3, "lines of unified context")
	c.Flags().IntVar(&maxLines, "max-lines", 500, "maximum number of diff lines to return")
	return c
}

// ---- show ----

func artifactShowCmd() *cobra.Command {
	var (
		maxBytes int64
		all      bool
	)
	c := &cobra.Command{
		Use:   "show <sha>",
		Short: "Print an artifact's full contents, subject to a byte cap",
		Long: `Print the full contents of an artifact. Refuses to dump files above
--max-bytes unless --all is passed, to prevent an agent from loading
a multi-megabyte disassembly into context by accident. For navigating
large artifacts, use head/tail/range/grep/diff instead.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			s, err := openStore()
			if err != nil {
				return err
			}
			sha, rel, abs, err := resolveArtifact(s, args[0])
			if err != nil {
				return err
			}
			fi, err := os.Stat(abs)
			if err != nil {
				return err
			}
			limits := map[string]any{"max_bytes": maxBytes, "all": all}
			if !all && fi.Size() > maxBytes {
				return fmt.Errorf("artifact %s is %d bytes, exceeds --max-bytes=%d. Use `artifact head/tail/range/grep/diff` to navigate, or pass --all to force the full dump", sha[:12], fi.Size(), maxBytes)
			}
			f, err := os.Open(abs)
			if err != nil {
				return err
			}
			defer f.Close()
			data, err := io.ReadAll(f)
			if err != nil {
				return err
			}
			if w.IsJSON() {
				return w.JSON(map[string]any{
					"sha":     sha,
					"path":    rel,
					"limits":  limits,
					"bytes":   fi.Size(),
					"content": string(data),
				})
			}
			printLimits(w, limits)
			w.Textf("[file: %s, %d bytes]\n", rel, fi.Size())
			_, _ = w.Raw().Write(data)
			if len(data) > 0 && data[len(data)-1] != '\n' {
				w.Textln("")
			}
			return nil
		},
	}
	c.Flags().Int64Var(&maxBytes, "max-bytes", defaultShowMaxBytes, "refuse to show files larger than this (agent safety)")
	c.Flags().BoolVar(&all, "all", false, "bypass --max-bytes and show the full file")
	return c
}
