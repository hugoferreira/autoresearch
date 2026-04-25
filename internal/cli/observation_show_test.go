package cli

import (
	"strings"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type observationShowRawResponse struct {
	ID         string `json:"id"`
	Experiment string `json:"experiment"`
	Instrument string `json:"instrument"`
	Samples    int    `json:"samples"`
	Raw        *struct {
		Artifact entity.Artifact  `json:"artifact"`
		Samples  []map[string]any `json:"samples"`
	} `json:"raw,omitempty"`
	RawReadIssues []string `json:"raw_read_issues"`
}

var _ = Describe("observation show", func() {
	BeforeEach(saveGlobals)

	It("returns the observation entity in JSON without raw fields by default", func() {
		dir, s := setupGoalStore()
		writeObservationFixture(s, &entity.Observation{
			ID:         "O-0001",
			Experiment: "E-0001",
			Instrument: "timing",
			MeasuredAt: time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC),
			Value:      1.2,
			Unit:       "s",
			Samples:    2,
			PerSample:  []float64{1.1, 1.3},
			Command:    "./bench",
			Author:     "agent:observer",
		})

		got := runCLIJSON[map[string]any](dir, "observation", "show", "O-0001")
		Expect(got).To(HaveKeyWithValue("id", "O-0001"))
		Expect(got).To(HaveKeyWithValue("samples", BeNumerically("==", 2)))
		Expect(got).NotTo(HaveKey("raw"))
		Expect(got).NotTo(HaveKey("raw_read_issues"))
	})

	It("decodes scalar JSON raw samples when requested", func() {
		dir, s := setupGoalStore()
		raw := `{
  "command": "./bench",
  "pattern": "cycles:(\\d+)",
  "worktree": "/tmp/wt",
  "samples": [
    {"run": 1, "exit_code": 0, "value": 123400, "stdout": "cycles: 123400"},
    {"run": 2, "exit_code": 0, "value": 123500, "stdout": "cycles: 123500"}
  ]
}`
		artifact := writeArtifactFixture(s, "scalar.json", []byte(raw), "scalar", "application/json")
		writeObservationFixture(s, &entity.Observation{
			ID:         "O-0001",
			Experiment: "E-0001",
			Instrument: "qemu_cycles",
			MeasuredAt: time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC),
			Value:      123450,
			Unit:       "cycles",
			Samples:    2,
			PerSample:  []float64{123400, 123500},
			Artifacts:  []entity.Artifact{artifact},
			Command:    "./bench",
			Author:     "agent:observer",
		})

		got := runCLIJSON[observationShowRawResponse](dir, "observation", "show", "O-0001", "--include-raw")
		Expect(got.Raw).NotTo(BeNil())
		Expect(got.Raw.Artifact.Name).To(Equal("scalar"))
		Expect(got.Raw.Samples).To(HaveLen(2))
		Expect(got.Raw.Samples[0]).To(HaveKeyWithValue("index", BeNumerically("==", 0)))
		Expect(got.Raw.Samples[0]).To(HaveKeyWithValue("stdout", "cycles: 123400"))
		Expect(got.Raw.Samples[0]).To(HaveKeyWithValue("exit_code", BeNumerically("==", 0)))
		Expect(got.Raw.Samples[0]).To(HaveKeyWithValue("passed", true))
		Expect(got.Raw.Samples[0]).To(HaveKeyWithValue("value", BeNumerically("==", 123400)))
		Expect(got.RawReadIssues).To(BeEmpty())
	})

	It("synthesizes a raw sample for text artifacts", func() {
		dir, s := setupGoalStore()
		pass := true
		artifact := writeArtifactFixture(s, "stdout.txt", []byte("host tests passed\n"), "stdout", "text/plain")
		writeObservationFixture(s, &entity.Observation{
			ID:         "O-0001",
			Experiment: "E-0001",
			Instrument: "host_test",
			MeasuredAt: time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC),
			Value:      1,
			Unit:       "pass",
			Samples:    1,
			Pass:       &pass,
			Artifacts:  []entity.Artifact{artifact},
			Command:    "go test ./...",
			ExitCode:   0,
			Author:     "agent:observer",
		})

		got := runCLIJSON[observationShowRawResponse](dir, "observation", "show", "O-0001", "--include-raw")
		Expect(got.Raw).NotTo(BeNil())
		Expect(got.Raw.Samples).To(HaveLen(1))
		Expect(got.Raw.Samples[0]).To(HaveKeyWithValue("stdout", "host tests passed\n"))
		Expect(got.Raw.Samples[0]).To(HaveKeyWithValue("exit_code", BeNumerically("==", 0)))
		Expect(got.Raw.Samples[0]).To(HaveKeyWithValue("passed", true))
		Expect(got.RawReadIssues).To(BeEmpty())
	})

	It("surfaces raw read issues without failing when the artifact is missing", func() {
		dir, s := setupGoalStore()
		writeObservationFixture(s, &entity.Observation{
			ID:         "O-0001",
			Experiment: "E-0001",
			Instrument: "timing",
			MeasuredAt: time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC),
			Value:      1.2,
			Unit:       "s",
			Samples:    1,
			Artifacts: []entity.Artifact{{
				Name: "stdout",
				SHA:  "abcd1234",
				Path: "artifacts/ab/missing/stdout.txt",
			}},
			Author: "agent:observer",
		})

		got := runCLIJSON[observationShowRawResponse](dir, "observation", "show", "O-0001", "--include-raw")
		Expect(got.Raw).NotTo(BeNil())
		Expect(got.RawReadIssues).To(ContainElement(ContainSubstring("unreadable")))
	})

	It("surfaces raw read issues without failing when the artifact is too large", func() {
		dir, s := setupGoalStore()
		artifact := writeArtifactFixture(s, "stdout.txt", []byte(strings.Repeat("x", defaultShowMaxBytes+1)), "stdout", "text/plain")
		writeObservationFixture(s, &entity.Observation{
			ID:         "O-0001",
			Experiment: "E-0001",
			Instrument: "timing",
			MeasuredAt: time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC),
			Value:      1.2,
			Unit:       "s",
			Samples:    1,
			Artifacts:  []entity.Artifact{artifact},
			Author:     "agent:observer",
		})

		got := runCLIJSON[observationShowRawResponse](dir, "observation", "show", "O-0001", "--include-raw")
		Expect(got.Raw).NotTo(BeNil())
		Expect(got.Raw.Samples).To(BeEmpty())
		Expect(got.RawReadIssues).To(ContainElement(ContainSubstring("exceeds max_bytes")))
	})

	It("renders text summaries with raw sample snippets", func() {
		dir, s := setupGoalStore()
		artifact := writeArtifactFixture(s, "stdout.txt", []byte("cycles: 42\n"), "stdout", "text/plain")
		writeObservationFixture(s, &entity.Observation{
			ID:         "O-0001",
			Experiment: "E-0001",
			Instrument: "qemu_cycles",
			MeasuredAt: time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC),
			Value:      42,
			Unit:       "cycles",
			Samples:    1,
			Artifacts:  []entity.Artifact{artifact},
			Command:    "./bench",
			Author:     "agent:observer",
		})

		out := runCLI(dir, "observation", "show", "O-0001", "--include-raw")
		expectText(out,
			"id:          O-0001",
			"artifacts:",
			"sha="+shortSHA(artifact.SHA),
			"raw:",
			"stdout: cycles: 42",
		)
	})
})

func writeArtifactFixture(s interface {
	WriteArtifact([]byte, string) (string, string, error)
}, filename string, content []byte, name, mime string) entity.Artifact {
	GinkgoHelper()
	sha, rel, err := s.WriteArtifact(content, filename)
	Expect(err).NotTo(HaveOccurred())
	return entity.Artifact{Name: name, SHA: sha, Path: rel, Bytes: int64(len(content)), Mime: mime}
}

func writeObservationFixture(s interface {
	WriteObservation(*entity.Observation) error
}, o *entity.Observation) {
	GinkgoHelper()
	Expect(s.WriteObservation(o)).To(Succeed())
}
