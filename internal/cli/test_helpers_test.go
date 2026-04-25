package cli

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func saveGlobals() {
	GinkgoHelper()
	j, p, d := globalJSON, globalProjectDir, globalDryRun
	DeferCleanup(func() {
		globalJSON = j
		globalProjectDir = p
		globalDryRun = d
	})
}

func createCLIStore() *store.Store {
	GinkgoHelper()
	_, s := createCLIStoreDir()
	return s
}

func createCLIStoreDir() (string, *store.Store) {
	GinkgoHelper()
	dir := GinkgoT().TempDir()
	s, err := store.Create(dir, store.Config{
		Build: store.CommandSpec{Command: "true"},
		Test:  store.CommandSpec{Command: "true"},
	})
	Expect(err).NotTo(HaveOccurred())
	return dir, s
}

func setupGoalStore() (string, *store.Store) {
	GinkgoHelper()
	dir := GinkgoT().TempDir()
	s, err := store.Create(dir, store.Config{
		Build: store.CommandSpec{Command: "true"},
		Test:  store.CommandSpec{Command: "true"},
		Instruments: map[string]store.Instrument{
			"timing":      {Unit: "s"},
			"binary_size": {Unit: "bytes"},
			"host_test":   {Unit: "bool"},
		},
	})
	Expect(err).NotTo(HaveOccurred())

	now := time.Now().UTC()
	max := 131072.0
	goal := &entity.Goal{
		ID:        "G-0001",
		Status:    entity.GoalStatusActive,
		CreatedAt: &now,
		Objective: entity.Objective{Instrument: "timing", Direction: "decrease"},
		Constraints: []entity.Constraint{
			{Instrument: "binary_size", Max: &max},
			{Instrument: "host_test", Require: "pass"},
		},
	}
	Expect(s.WriteGoal(goal)).To(Succeed())
	Expect(s.UpdateState(func(st *store.State) error {
		st.CurrentGoalID = goal.ID
		st.Counters["G"] = 1
		return nil
	})).To(Succeed())
	return dir, s
}

func findLastEvent(s *store.Store, kind string) *store.Event {
	GinkgoHelper()
	events, err := s.Events(0)
	Expect(err).NotTo(HaveOccurred())
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Kind == kind {
			return &events[i]
		}
	}
	return nil
}

func decodePayload(e *store.Event) map[string]any {
	GinkgoHelper()
	var payload map[string]any
	Expect(json.Unmarshal(e.Data, &payload)).To(Succeed())
	return payload
}

func runCLIResult(dir string, args ...string) (string, string, error) {
	GinkgoHelper()

	oldStdout, oldStderr := os.Stdout, os.Stderr
	rOut, wOut, err := os.Pipe()
	Expect(err).NotTo(HaveOccurred(), "stdout pipe")
	rErr, wErr, err := os.Pipe()
	Expect(err).NotTo(HaveOccurred(), "stderr pipe")

	outCh := make(chan string, 1)
	errCh := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(rOut)
		outCh <- string(data)
	}()
	go func() {
		data, _ := io.ReadAll(rErr)
		errCh <- string(data)
	}()

	os.Stdout = wOut
	os.Stderr = wErr
	defer func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()

	root := Root()
	root.SetArgs(append([]string{"-C", dir}, args...))
	execErr := root.Execute()

	_ = wOut.Close()
	_ = wErr.Close()
	return <-outCh, <-errCh, execErr
}

func runCLI(dir string, args ...string) string {
	GinkgoHelper()
	stdout, stderr, err := runCLIResult(dir, args...)
	Expect(err).NotTo(HaveOccurred(),
		"autoresearch %s\nstdout:\n%s\nstderr:\n%s",
		strings.Join(args, " "), stdout, stderr,
	)
	return stdout
}

func runCLIJSON[T any](dir string, args ...string) T {
	GinkgoHelper()
	out := runCLI(dir, append([]string{"--json"}, args...)...)
	var got T
	Expect(json.Unmarshal([]byte(out), &got)).To(Succeed(), "decode JSON for %q:\n%s", strings.Join(args, " "), out)
	return got
}

func expectText(out string, wants ...string) {
	GinkgoHelper()
	for _, want := range wants {
		Expect(out).To(ContainSubstring(want), "rendered output:\n%s", out)
	}
}

func expectNoText(out string, bads ...string) {
	GinkgoHelper()
	for _, bad := range bads {
		Expect(out).NotTo(ContainSubstring(bad), "rendered output:\n%s", out)
	}
}
