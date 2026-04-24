package cli

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("instrument register", func() {
	BeforeEach(saveGlobals)

	It("preserves commas inside a single command argument", func() {
		dir, s := setupGoalStore()
		awkArg := `awk {gsub(/ /,"",x)}`

		runCLI(dir,
			"instrument", "register", "awkprobe",
			"--cmd", awkArg,
			"--parser", "builtin:passfail",
			"--unit", "bool",
		)

		cfg, err := s.Config()
		Expect(err).NotTo(HaveOccurred())
		inst, ok := cfg.Instruments["awkprobe"]
		Expect(ok).To(BeTrue())
		Expect(inst.Cmd).To(Equal([]string{awkArg}))
	})

	It("preserves repeated --cmd flags as argv elements", func() {
		dir, s := setupGoalStore()

		runCLI(dir,
			"instrument", "register", "make_test",
			"--cmd", "make",
			"--cmd", "-f",
			"--cmd", "Makefile.sim",
			"--cmd", "test",
			"--parser", "builtin:passfail",
			"--unit", "bool",
		)

		cfg, err := s.Config()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.Instruments["make_test"].Cmd).To(Equal([]string{"make", "-f", "Makefile.sim", "test"}))
	})

	It("stores evidence commands in registration order", func() {
		dir, s := setupGoalStore()

		runCLI(dir,
			"instrument", "register", "timing_probe",
			"--cmd", "sh",
			"--cmd", "-c",
			"--cmd", "echo cycles: 42",
			"--parser", "builtin:scalar",
			"--pattern", `cycles:\s*(\d+)`,
			"--unit", "cycles",
			"--evidence", "mechanism=printf candidate",
			"--evidence", "summary=printf summary",
		)

		cfg, err := s.Config()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.Instruments["timing_probe"].Evidence).To(HaveLen(2))
		Expect(cfg.Instruments["timing_probe"].Evidence[0].Name).To(Equal("mechanism"))
		Expect(cfg.Instruments["timing_probe"].Evidence[0].Cmd).To(Equal("printf candidate"))
		Expect(cfg.Instruments["timing_probe"].Evidence[1].Name).To(Equal("summary"))
		Expect(cfg.Instruments["timing_probe"].Evidence[1].Cmd).To(Equal("printf summary"))
	})

	DescribeTable("validates evidence flag shape",
		func(extraArgs []string, wantErr string) {
			dir, _ := setupGoalStore()
			root := Root()
			root.SetArgs(append([]string{
				"-C", dir,
				"instrument", "register", "timing_probe",
				"--cmd", "true",
				"--parser", "builtin:passfail",
				"--unit", "bool",
			}, extraArgs...))

			err := root.Execute()
			Expect(err).To(MatchError(ContainSubstring(wantErr)))
		},
		Entry("missing equals", []string{"--evidence", "mechanism"}, "want name=cmd"),
		Entry("missing name", []string{"--evidence", "=printf trace"}, "name is required"),
		Entry("missing command", []string{"--evidence", "mechanism="}, "command is required"),
		Entry("duplicate names", []string{"--evidence", "mechanism=printf one", "--evidence", "mechanism=printf two"}, "duplicate --evidence name"),
		Entry("path separator in name", []string{"--evidence", "bad/name=printf trace"}, "must not contain path separators"),
	)
})
