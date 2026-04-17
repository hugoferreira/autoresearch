package cli

import (
	"testing"
)

// TestInstrumentRegister_CmdPreservesCommas is a regression anchor for the
// CSV hazard: before this fix, --cmd used StringSliceVar which split argv
// elements on commas, shredding shell pipelines like `awk '{gsub(/ /,"",x)}'`
// into multiple argv elements.
func TestInstrumentRegister_CmdPreservesCommas(t *testing.T) {
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
}

// TestInstrumentRegister_CmdRepeatableArgv verifies the repeated-flag form
// still works (one --cmd per argv element).
func TestInstrumentRegister_CmdRepeatableArgv(t *testing.T) {
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
}
