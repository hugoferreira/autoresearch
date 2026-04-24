package cli

import (
	"strings"

	"github.com/bytter/autoresearch/internal/testkit"
	"github.com/onsi/ginkgo/v2"
)

// TestInstrumentRegister_CmdPreservesCommas is a regression anchor for the
// CSV hazard: before this fix, --cmd used StringSliceVar which split argv
// elements on commas, shredding shell pipelines like `awk '{gsub(/ /,"",x)}'`
// into multiple argv elements.
var _ = ginkgo.Describe("TestInstrumentRegister_CmdPreservesCommas", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		saveGlobals(t)
		dir, s := setupGoalStore(t)

		awkArg := `awk {gsub(/ /,"",x)}`

		root := Root()
		root.SetArgs([]string{
			"-C", dir,
			"instrument", "register", "awkprobe",
			"--cmd", awkArg,
			"--parser", "builtin:passfail",
			"--unit", "bool",
		})
		if err := root.Execute(); err != nil {
			t.Fatalf("execute: %v", err)
		}

		cfg, err := s.Config()
		if err != nil {
			t.Fatal(err)
		}
		inst, ok := cfg.Instruments["awkprobe"]
		if !ok {
			t.Fatal("awkprobe not registered")
		}
		if len(inst.Cmd) != 1 {
			t.Fatalf("Cmd split into %d elements, want 1: %q", len(inst.Cmd), inst.Cmd)
		}
		if inst.Cmd[0] != awkArg {
			t.Errorf("Cmd[0] = %q, want %q", inst.Cmd[0], awkArg)
		}
	})
})

// TestInstrumentRegister_CmdRepeatableArgv verifies the repeated-flag form
// still works (one --cmd per argv element).
var _ = ginkgo.Describe("TestInstrumentRegister_CmdRepeatableArgv", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		saveGlobals(t)
		dir, s := setupGoalStore(t)

		root := Root()
		root.SetArgs([]string{
			"-C", dir,
			"instrument", "register", "make_test",
			"--cmd", "make",
			"--cmd", "-f",
			"--cmd", "Makefile.sim",
			"--cmd", "test",
			"--parser", "builtin:passfail",
			"--unit", "bool",
		})
		if err := root.Execute(); err != nil {
			t.Fatalf("execute: %v", err)
		}

		cfg, err := s.Config()
		if err != nil {
			t.Fatal(err)
		}
		inst := cfg.Instruments["make_test"]
		want := []string{"make", "-f", "Makefile.sim", "test"}
		if len(inst.Cmd) != len(want) {
			t.Fatalf("Cmd = %q, want %q", inst.Cmd, want)
		}
		for i, w := range want {
			if inst.Cmd[i] != w {
				t.Errorf("Cmd[%d] = %q, want %q", i, inst.Cmd[i], w)
			}
		}
	})
})

var _ = ginkgo.Describe("TestInstrumentRegister_EvidenceStoredInOrder", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		saveGlobals(t)
		dir, s := setupGoalStore(t)

		root := Root()
		root.SetArgs([]string{
			"-C", dir,
			"instrument", "register", "timing_probe",
			"--cmd", "sh",
			"--cmd", "-c",
			"--cmd", "echo cycles: 42",
			"--parser", "builtin:scalar",
			"--pattern", `cycles:\s*(\d+)`,
			"--unit", "cycles",
			"--evidence", "mechanism=printf candidate",
			"--evidence", "summary=printf summary",
		})
		if err := root.Execute(); err != nil {
			t.Fatalf("execute: %v", err)
		}

		cfg, err := s.Config()
		if err != nil {
			t.Fatal(err)
		}
		inst := cfg.Instruments["timing_probe"]
		if got, want := len(inst.Evidence), 2; got != want {
			t.Fatalf("Evidence len = %d, want %d", got, want)
		}
		if inst.Evidence[0].Name != "mechanism" || inst.Evidence[0].Cmd != "printf candidate" {
			t.Fatalf("Evidence[0] = %+v", inst.Evidence[0])
		}
		if inst.Evidence[1].Name != "summary" || inst.Evidence[1].Cmd != "printf summary" {
			t.Fatalf("Evidence[1] = %+v", inst.Evidence[1])
		}
	})
})

var _ = ginkgo.Describe("TestInstrumentRegister_EvidenceValidation", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		saveGlobals(t)
		dir, _ := setupGoalStore(t)

		cases := []struct {
			name string
			args []string
			want string
		}{
			{
				name: "missing equals",
				args: []string{"--evidence", "mechanism"},
				want: "want name=cmd",
			},
			{
				name: "missing name",
				args: []string{"--evidence", "=printf trace"},
				want: "name is required",
			},
			{
				name: "missing command",
				args: []string{"--evidence", "mechanism="},
				want: "command is required",
			},
			{
				name: "duplicate names",
				args: []string{"--evidence", "mechanism=printf one", "--evidence", "mechanism=printf two"},
				want: "duplicate --evidence name",
			},
			{
				name: "path separator in name",
				args: []string{"--evidence", "bad/name=printf trace"},
				want: "must not contain path separators",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t testkit.T) {
				root := Root()
				root.SetArgs(append([]string{
					"-C", dir,
					"instrument", "register", "timing_probe",
					"--cmd", "true",
					"--parser", "builtin:passfail",
					"--unit", "bool",
				}, tc.args...))
				err := root.Execute()
				if err == nil {
					t.Fatal("expected register to fail")
				}
				if !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("error %q does not contain %q", err, tc.want)
				}
			})
		}
	})
})
