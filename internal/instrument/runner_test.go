package instrument_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bytter/autoresearch/internal/instrument"
	"github.com/bytter/autoresearch/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("instrument runner parsers", func() {
	It("maps command exit status into pass/fail observations", func() {
		dir := GinkgoT().TempDir()
		r, err := instrument.Run(context.Background(), instrument.Config{
			ProjectDir:  dir,
			WorktreeDir: dir,
			Name:        "host_test",
			Instrument: store.Instrument{
				Cmd:    []string{"sh", "-c", "true"},
				Parser: "builtin:passfail",
			},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(r.Pass).NotTo(BeNil())
		Expect(*r.Pass).To(BeTrue())
		Expect(r.Value).To(Equal(1.0))
		Expect(r.Artifacts).To(HaveLen(1))
		Expect(r.Artifacts[0].Name).To(Equal("stdout"))

		r2, err := instrument.Run(context.Background(), instrument.Config{
			ProjectDir:  dir,
			WorktreeDir: dir,
			Name:        "host_test",
			Instrument: store.Instrument{
				Cmd:    []string{"sh", "-c", "exit 3"},
				Parser: "builtin:passfail",
			},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(r2.Pass).NotTo(BeNil())
		Expect(*r2.Pass).To(BeFalse())
		Expect(r2.Value).To(Equal(0.0))
		Expect(r2.ExitCode).To(Equal(3))
	})

	It("records repeated timing samples and confidence intervals", func() {
		dir := GinkgoT().TempDir()
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
		Expect(err).NotTo(HaveOccurred())
		Expect(r.SamplesN).To(Equal(5))
		Expect(r.PerSample).To(HaveLen(5))
		Expect(r.Unit).To(Equal("seconds"))
		Expect(r.CILow).NotTo(BeNil())
		Expect(r.CIHigh).NotTo(BeNil())
		Expect(*r.CILow).To(BeNumerically("<=", r.Value))
		Expect(*r.CIHigh).To(BeNumerically(">=", r.Value))
	})

	It("parses GNU size output into text size and auxiliary sections", func() {
		dir := GinkgoT().TempDir()
		r, err := instrument.Run(context.Background(), instrument.Config{
			ProjectDir:  dir,
			WorktreeDir: dir,
			Name:        "size_flash",
			Instrument: store.Instrument{
				Cmd:    []string{"sh", "-c", `printf '   text\tdata\t bss\t dec\t hex\tfilename\n  1024\t 256\t  64\t1344\t540\ta.out\n'`},
				Parser: "builtin:size",
			},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(r.Value).To(Equal(1024.0))
		Expect(r.Aux["text"]).To(Equal(int64(1024)))
		Expect(r.Aux["data"]).To(Equal(int64(256)))
		Expect(r.Aux["bss"]).To(Equal(int64(64)))
	})

	It("parses Mach-O size output with normalized section names", func() {
		dir := GinkgoT().TempDir()
		r, err := instrument.Run(context.Background(), instrument.Config{
			ProjectDir:  dir,
			WorktreeDir: dir,
			Instrument: store.Instrument{
				Cmd:    []string{"sh", "-c", `printf '__TEXT\t__DATA\t__OBJC\tothers\tdec\thex\n4096\t0\t0\t1024\t5120\t1400\n'`},
				Parser: "builtin:size",
			},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(r.Value).To(Equal(4096.0))
		Expect(r.Aux["text"]).To(Equal(int64(4096)))
	})

	It("rejects unknown parser names", func() {
		_, err := instrument.Run(context.Background(), instrument.Config{
			Instrument: store.Instrument{Cmd: []string{"true"}, Parser: "builtin:nope"},
		})
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("scalar parser", func() {
	It("extracts repeated keyword-form scalar samples", func() {
		dir := GinkgoT().TempDir()
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
		Expect(err).NotTo(HaveOccurred())
		Expect(r.Unit).To(Equal("cycles"))
		Expect(r.SamplesN).To(Equal(3))
		Expect(r.PerSample).To(Equal([]float64{123456, 123456, 123456}))
		Expect(r.Value).To(Equal(123456.0))
	})

	It("extracts values from JSON-like output", func() {
		dir := GinkgoT().TempDir()
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
		Expect(err).NotTo(HaveOccurred())
		Expect(r.Value).To(Equal(987.0))
	})

	It("honors case-insensitive regular expressions", func() {
		dir := GinkgoT().TempDir()
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
		Expect(err).NotTo(HaveOccurred())
		Expect(r.Value).To(Equal(42000.0))
	})

	DescribeTable("rejects invalid scalar configurations or output",
		func(inst store.Instrument) {
			dir := GinkgoT().TempDir()
			_, err := instrument.Run(context.Background(), instrument.Config{
				WorktreeDir: dir,
				Samples:     1,
				Instrument:  inst,
			})
			Expect(err).To(HaveOccurred())
		},
		Entry("without a matching output line", store.Instrument{
			Cmd:     []string{"sh", "-c", "echo hello"},
			Parser:  "builtin:scalar",
			Pattern: `cycles:\s*(\d+)`,
			Unit:    "cycles",
		}),
		Entry("without a pattern", store.Instrument{
			Cmd:    []string{"echo", "hello"},
			Parser: "builtin:scalar",
			Unit:   "cycles",
		}),
		Entry("with multiple capture groups", store.Instrument{
			Cmd:     []string{"sh", "-c", "echo cycles: 100"},
			Parser:  "builtin:scalar",
			Pattern: `(cycles):\s*(\d+)`,
			Unit:    "cycles",
		}),
		Entry("with a non-integer capture", store.Instrument{
			Cmd:     []string{"sh", "-c", "echo cycles: twelve"},
			Parser:  "builtin:scalar",
			Pattern: `cycles:\s*(\w+)`,
			Unit:    "cycles",
		}),
	)

	It("preserves configured units", func() {
		dir := GinkgoT().TempDir()
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
		Expect(err).NotTo(HaveOccurred())
		Expect(r.Unit).To(Equal("instructions"))
		Expect(r.Value).To(Equal(42.0))
	})
})

var _ = Describe("evidence capture", func() {
	It("runs evidence commands in the project/worktree shell context", func() {
		dir := GinkgoT().TempDir()
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
		Expect(err).NotTo(HaveOccurred())
		Expect(r.EvidenceFailures).To(BeEmpty())

		ev := artifactByName(r.Artifacts, "evidence/mechanism")
		Expect(ev.Filename).To(Equal("mechanism.txt"))
		Expect(ev.Mime).To(Equal("text/plain"))
		parts := strings.Split(string(ev.Content), "|")
		Expect(parts).To(HaveLen(3))
		Expect(parts[0]).To(Equal(dir))
		Expect(parts[1]).To(Equal(dir))

		gotPWD, err := filepath.EvalSymlinks(parts[2])
		Expect(err).NotTo(HaveOccurred())
		wantPWD, err := filepath.EvalSymlinks(dir)
		Expect(err).NotTo(HaveOccurred())
		Expect(gotPWD).To(Equal(wantPWD))
	})

	It("records evidence command failures without failing the primary measurement", func() {
		dir := GinkgoT().TempDir()
		marker := filepath.Join(dir, "evidence.txt")
		Expect(os.WriteFile(marker, []byte("trace=ok\n"), 0o644)).To(Succeed())

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
		Expect(err).NotTo(HaveOccurred())
		Expect(r.Value).To(Equal(99.0))
		Expect(findArtifact(r.Artifacts, "scalar")).To(BeTrue())
		Expect(findArtifact(r.Artifacts, "evidence/mechanism")).To(BeTrue())
		Expect(findArtifact(r.Artifacts, "evidence/broken")).To(BeFalse())
		Expect(r.EvidenceFailures).To(HaveLen(1))
		Expect(r.EvidenceFailures[0].Name).To(Equal("broken"))
		Expect(r.EvidenceFailures[0].ExitCode).To(Equal(7))
		Expect(r.EvidenceFailures[0].Error).To(BeEmpty())
	})

	It("records spawn failures without an exit code or artifact", func() {
		dir := GinkgoT().TempDir()
		shell, err := exec.LookPath("sh")
		Expect(err).NotTo(HaveOccurred())
		oldPath := os.Getenv("PATH")
		Expect(os.Setenv("PATH", "")).To(Succeed())
		DeferCleanup(os.Setenv, "PATH", oldPath)

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
		Expect(err).NotTo(HaveOccurred())
		Expect(r.Value).To(Equal(99.0))
		Expect(r.EvidenceFailures).To(HaveLen(1))
		got := r.EvidenceFailures[0]
		Expect(got.Name).To(Equal("mechanism"))
		Expect(got.ExitCode).To(Equal(0))
		Expect(got.Error).To(ContainSubstring(`spawn "sh -c echo trace"`))
		Expect(findArtifact(r.Artifacts, "evidence/mechanism")).To(BeFalse())
	})
})

func artifactByName(arts []instrument.ArtifactContent, name string) instrument.ArtifactContent {
	GinkgoHelper()
	for _, art := range arts {
		if art.Name == name {
			return art
		}
	}
	Fail("artifact not found: " + name)
	return instrument.ArtifactContent{}
}

func findArtifact(arts []instrument.ArtifactContent, name string) bool {
	for _, art := range arts {
		if art.Name == name {
			return true
		}
	}
	return false
}
