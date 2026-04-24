package instrument_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bytter/autoresearch/internal/instrument"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/bytter/autoresearch/internal/testkit"
	"github.com/onsi/ginkgo/v2"
)

var _ = ginkgo.Describe("TestPassFail", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		dir := t.TempDir()
		r, err := instrument.Run(context.Background(), instrument.Config{
			ProjectDir:  dir,
			WorktreeDir: dir,
			Name:        "host_test",
			Instrument: store.Instrument{
				Cmd:    []string{"sh", "-c", "true"},
				Parser: "builtin:passfail",
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if r.Pass == nil || !*r.Pass || r.Value != 1.0 {
			t.Errorf("pass case: %+v", r)
		}
		if len(r.Artifacts) != 1 || r.Artifacts[0].Name != "stdout" {
			t.Errorf("artifacts: %+v", r.Artifacts)
		}

		r2, err := instrument.Run(context.Background(), instrument.Config{
			ProjectDir:  dir,
			WorktreeDir: dir,
			Name:        "host_test",
			Instrument: store.Instrument{
				Cmd:    []string{"sh", "-c", "exit 3"},
				Parser: "builtin:passfail",
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if r2.Pass == nil || *r2.Pass || r2.Value != 0.0 || r2.ExitCode != 3 {
			t.Errorf("fail case: %+v", r2)
		}
	})
})

var _ = ginkgo.Describe("TestTiming", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		dir := t.TempDir()
		r, err := instrument.Run(context.Background(), instrument.Config{
			ProjectDir:  dir,
			WorktreeDir: dir,
			Name:        "host_timing",
			Samples:     5,
			Instrument: store.Instrument{
				Cmd:    []string{"sh", "-c", "true"},
				Parser: "builtin:timing",
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if r.SamplesN != 5 || len(r.PerSample) != 5 {
			t.Errorf("samples: got %d/%d want 5/5", r.SamplesN, len(r.PerSample))
		}
		if r.Unit != "seconds" || r.CILow == nil || r.CIHigh == nil {
			t.Errorf("timing result: %+v", r)
		}
		if *r.CILow > r.Value || *r.CIHigh < r.Value {
			t.Errorf("CI does not bracket mean: low=%v mean=%v high=%v", *r.CILow, r.Value, *r.CIHigh)
		}
	})
})

var _ = ginkgo.Describe("TestSize_GNUFormat", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		dir := t.TempDir()
		// Synthesize GNU-style size output via `printf`.
		r, err := instrument.Run(context.Background(), instrument.Config{
			ProjectDir:  dir,
			WorktreeDir: dir,
			Name:        "size_flash",
			Instrument: store.Instrument{
				Cmd:    []string{"sh", "-c", `printf '   text\tdata\t bss\t dec\t hex\tfilename\n  1024\t 256\t  64\t1344\t540\ta.out\n'`},
				Parser: "builtin:size",
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if r.Value != 1024 {
			t.Errorf("text size: got %v, want 1024", r.Value)
		}
		if r.Aux["text"] != int64(1024) || r.Aux["data"] != int64(256) || r.Aux["bss"] != int64(64) {
			t.Errorf("aux fields: %+v", r.Aux)
		}
	})
})

var _ = ginkgo.Describe("TestSize_MachOFormat", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		dir := t.TempDir()
		r, err := instrument.Run(context.Background(), instrument.Config{
			ProjectDir:  dir,
			WorktreeDir: dir,
			Instrument: store.Instrument{
				Cmd:    []string{"sh", "-c", `printf '__TEXT\t__DATA\t__OBJC\tothers\tdec\thex\n4096\t0\t0\t1024\t5120\t1400\n'`},
				Parser: "builtin:size",
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if r.Value != 4096 {
			t.Errorf("text size: got %v, want 4096", r.Value)
		}
		if r.Aux["text"] != int64(4096) {
			t.Errorf("text aux (leading underscore stripped + lowercased): %+v", r.Aux)
		}
	})
})

var _ = ginkgo.Describe("TestUnknownParser", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		_, err := instrument.Run(context.Background(), instrument.Config{
			Instrument: store.Instrument{Cmd: []string{"true"}, Parser: "builtin:nope"},
		})
		if err == nil {
			t.Error("unknown parser should error")
		}
	})
})

var _ = ginkgo.Describe("TestScalar_KeywordFormat", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		dir := t.TempDir()
		r, err := instrument.Run(context.Background(), instrument.Config{
			ProjectDir:  dir,
			WorktreeDir: dir,
			Samples:     3,
			Instrument: store.Instrument{
				Cmd:     []string{"sh", "-c", `echo "preamble"; echo "cycles: 123456"`},
				Parser:  "builtin:scalar",
				Pattern: `cycles:\s*(\d+)`,
				Unit:    "cycles",
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if r.Unit != "cycles" || r.SamplesN != 3 || len(r.PerSample) != 3 {
			t.Errorf("result: %+v", r)
		}
		for _, v := range r.PerSample {
			if v != 123456 {
				t.Errorf("sample: got %v, want 123456", v)
			}
		}
		if r.Value != 123456 {
			t.Errorf("mean: %v", r.Value)
		}
	})
})

var _ = ginkgo.Describe("TestScalar_JsonFormat", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		dir := t.TempDir()
		r, err := instrument.Run(context.Background(), instrument.Config{
			WorktreeDir: dir,
			Samples:     2,
			Instrument: store.Instrument{
				Cmd:     []string{"sh", "-c", `echo '{"cycles": 987, "note": "synthetic"}'`},
				Parser:  "builtin:scalar",
				Pattern: `"cycles"\s*:\s*(\d+)`,
				Unit:    "cycles",
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if r.Value != 987 {
			t.Errorf("json parse: got %v, want 987", r.Value)
		}
	})
})

var _ = ginkgo.Describe("TestScalar_CaseInsensitive", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		dir := t.TempDir()
		r, err := instrument.Run(context.Background(), instrument.Config{
			WorktreeDir: dir,
			Samples:     2,
			Instrument: store.Instrument{
				Cmd:     []string{"sh", "-c", `echo "ICOUNT=42000"`},
				Parser:  "builtin:scalar",
				Pattern: `(?i)icount\s*[:=]\s*(\d+)`,
				Unit:    "cycles",
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if r.Value != 42000 {
			t.Errorf("icount parse: got %v, want 42000", r.Value)
		}
	})
})

var _ = ginkgo.Describe("TestScalar_NoMatchRejected", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		dir := t.TempDir()
		_, err := instrument.Run(context.Background(), instrument.Config{
			WorktreeDir: dir,
			Samples:     1,
			Instrument: store.Instrument{
				Cmd:     []string{"sh", "-c", "echo hello"},
				Parser:  "builtin:scalar",
				Pattern: `cycles:\s*(\d+)`,
				Unit:    "cycles",
			},
		})
		if err == nil {
			t.Error("should have rejected output with no match")
		}
	})
})

var _ = ginkgo.Describe("TestScalar_MissingPatternRejected", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		dir := t.TempDir()
		_, err := instrument.Run(context.Background(), instrument.Config{
			WorktreeDir: dir,
			Samples:     1,
			Instrument: store.Instrument{
				Cmd:    []string{"echo", "hello"},
				Parser: "builtin:scalar",
				Unit:   "cycles",
				// Pattern intentionally empty
			},
		})
		if err == nil {
			t.Error("should require Pattern")
		}
	})
})

var _ = ginkgo.Describe("TestScalar_MultipleGroupsRejected", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		dir := t.TempDir()
		_, err := instrument.Run(context.Background(), instrument.Config{
			WorktreeDir: dir,
			Samples:     1,
			Instrument: store.Instrument{
				Cmd:     []string{"sh", "-c", "echo cycles: 100"},
				Parser:  "builtin:scalar",
				Pattern: `(cycles):\s*(\d+)`,
				Unit:    "cycles",
			},
		})
		if err == nil {
			t.Error("should reject pattern with multiple capture groups")
		}
	})
})

var _ = ginkgo.Describe("TestScalar_NonIntegerCaptureRejected", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		dir := t.TempDir()
		_, err := instrument.Run(context.Background(), instrument.Config{
			WorktreeDir: dir,
			Samples:     1,
			Instrument: store.Instrument{
				Cmd:     []string{"sh", "-c", "echo cycles: twelve"},
				Parser:  "builtin:scalar",
				Pattern: `cycles:\s*(\w+)`,
				Unit:    "cycles",
			},
		})
		if err == nil {
			t.Error("should reject non-integer capture")
		}
	})
})

var _ = ginkgo.Describe("TestScalar_UnitHonored", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		dir := t.TempDir()
		r, err := instrument.Run(context.Background(), instrument.Config{
			WorktreeDir: dir,
			Samples:     1,
			Instrument: store.Instrument{
				Cmd:     []string{"sh", "-c", "echo instructions retired: 42"},
				Parser:  "builtin:scalar",
				Pattern: `retired:\s*(\d+)`,
				Unit:    "instructions",
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if r.Unit != "instructions" || r.Value != 42 {
			t.Errorf("result: %+v", r)
		}
	})
})

var _ = ginkgo.Describe("TestEvidence_ArtifactUsesShellContext", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		dir := t.TempDir()
		r, err := instrument.Run(context.Background(), instrument.Config{
			ProjectDir:  dir,
			WorktreeDir: dir,
			Instrument: store.Instrument{
				Cmd:     []string{"sh", "-c", "echo cycles: 42"},
				Parser:  "builtin:scalar",
				Pattern: `cycles:\s*(\d+)`,
				Unit:    "cycles",
				Evidence: []store.EvidenceSpec{{
					Name: "mechanism",
					Cmd:  `printf '%s|%s|%s' "$WORKTREE" "$PROJECT" "$(pwd)"`,
				}},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if got, want := len(r.EvidenceFailures), 0; got != want {
			t.Fatalf("EvidenceFailures len = %d, want %d", got, want)
		}
		ev := artifactByName(t, r.Artifacts, "evidence/mechanism")
		if ev.Filename != "mechanism.txt" {
			t.Fatalf("evidence filename = %q, want mechanism.txt", ev.Filename)
		}
		if ev.Mime != "text/plain" {
			t.Fatalf("evidence mime = %q, want text/plain", ev.Mime)
		}
		parts := strings.Split(string(ev.Content), "|")
		if got, want := len(parts), 3; got != want {
			t.Fatalf("evidence content parts = %d, want %d (%q)", got, want, ev.Content)
		}
		if parts[0] != dir || parts[1] != dir {
			t.Fatalf("WORKTREE/PROJECT not propagated: %q", ev.Content)
		}
		gotPWD, err := filepath.EvalSymlinks(parts[2])
		if err != nil {
			t.Fatalf("EvalSymlinks(%q): %v", parts[2], err)
		}
		wantPWD, err := filepath.EvalSymlinks(dir)
		if err != nil {
			t.Fatalf("EvalSymlinks(%q): %v", dir, err)
		}
		if gotPWD != wantPWD {
			t.Fatalf("pwd = %q, want %q (from %q)", gotPWD, wantPWD, ev.Content)
		}
	})
})

var _ = ginkgo.Describe("TestEvidence_FailureNonFatal", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		dir := t.TempDir()
		marker := filepath.Join(dir, "evidence.txt")
		if err := os.WriteFile(marker, []byte("trace=ok\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		r, err := instrument.Run(context.Background(), instrument.Config{
			ProjectDir:  dir,
			WorktreeDir: dir,
			Instrument: store.Instrument{
				Cmd:     []string{"sh", "-c", "echo cycles: 99"},
				Parser:  "builtin:scalar",
				Pattern: `cycles:\s*(\d+)`,
				Unit:    "cycles",
				Evidence: []store.EvidenceSpec{
					{Name: "mechanism", Cmd: "cat evidence.txt"},
					{Name: "broken", Cmd: "echo nope >&2; exit 7"},
				},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if r.Value != 99 {
			t.Fatalf("primary value = %v, want 99", r.Value)
		}
		if _, ok := findArtifact(r.Artifacts, "scalar"); !ok {
			t.Fatal("primary scalar artifact missing")
		}
		if _, ok := findArtifact(r.Artifacts, "evidence/mechanism"); !ok {
			t.Fatal("successful evidence artifact missing")
		}
		if _, ok := findArtifact(r.Artifacts, "evidence/broken"); ok {
			t.Fatal("failed evidence should not produce an artifact")
		}
		if got, want := len(r.EvidenceFailures), 1; got != want {
			t.Fatalf("EvidenceFailures len = %d, want %d", got, want)
		}
		if got := r.EvidenceFailures[0]; got.Name != "broken" || got.ExitCode != 7 || got.Error != "" {
			t.Fatalf("unexpected evidence failure: %+v", got)
		}
	})
})

var _ = ginkgo.Describe("TestEvidence_SpawnFailureRecordsErrorWithoutExitCode", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		dir := t.TempDir()
		shell, err := exec.LookPath("sh")
		if err != nil {
			t.Fatalf("look up sh: %v", err)
		}
		t.Setenv("PATH", "")

		r, err := instrument.Run(context.Background(), instrument.Config{
			ProjectDir:  dir,
			WorktreeDir: dir,
			Instrument: store.Instrument{
				Cmd:     []string{shell, "-c", "echo cycles: 99"},
				Parser:  "builtin:scalar",
				Pattern: `cycles:\s*(\d+)`,
				Unit:    "cycles",
				Evidence: []store.EvidenceSpec{
					{Name: "mechanism", Cmd: "echo trace"},
				},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if r.Value != 99 {
			t.Fatalf("primary value = %v, want 99", r.Value)
		}
		if got, want := len(r.EvidenceFailures), 1; got != want {
			t.Fatalf("EvidenceFailures len = %d, want %d", got, want)
		}
		got := r.EvidenceFailures[0]
		if got.Name != "mechanism" {
			t.Fatalf("EvidenceFailures[0].Name = %q, want %q", got.Name, "mechanism")
		}
		if got.ExitCode != 0 {
			t.Fatalf("EvidenceFailures[0].ExitCode = %d, want 0 for spawn failure", got.ExitCode)
		}
		if got.Error == "" {
			t.Fatal("EvidenceFailures[0].Error is empty, want spawn failure detail")
		}
		if !strings.Contains(got.Error, `spawn "sh -c echo trace"`) {
			t.Fatalf("EvidenceFailures[0].Error = %q, want spawn context", got.Error)
		}
		if _, ok := findArtifact(r.Artifacts, "evidence/mechanism"); ok {
			t.Fatal("spawn-failed evidence should not produce an artifact")
		}
	})
})

func artifactByName(t testkit.T, arts []instrument.ArtifactContent, name string) instrument.ArtifactContent {
	t.Helper()
	if art, ok := findArtifact(arts, name); ok {
		return art
	}
	t.Fatalf("artifact %q not found in %+v", name, arts)
	return instrument.ArtifactContent{}
}

func findArtifact(arts []instrument.ArtifactContent, name string) (instrument.ArtifactContent, bool) {
	for _, art := range arts {
		if art.Name == name {
			return art, true
		}
	}
	return instrument.ArtifactContent{}, false
}
