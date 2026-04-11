package instrument_test

import (
	"context"
	"testing"

	"github.com/bytter/autoresearch/internal/instrument"
	"github.com/bytter/autoresearch/internal/store"
)

func TestPassFail(t *testing.T) {
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
}

func TestTiming(t *testing.T) {
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
}

func TestSize_GNUFormat(t *testing.T) {
	dir := t.TempDir()
	// Synthesize GNU-style size output via `printf`.
	r, err := instrument.Run(context.Background(), instrument.Config{
		ProjectDir:  dir,
		WorktreeDir: dir,
		Name:        "size_flash",
		Instrument: store.Instrument{
			Cmd: []string{"sh", "-c", `printf '   text\tdata\t bss\t dec\t hex\tfilename\n  1024\t 256\t  64\t1344\t540\ta.out\n'`},
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
}

func TestSize_MachOFormat(t *testing.T) {
	dir := t.TempDir()
	r, err := instrument.Run(context.Background(), instrument.Config{
		ProjectDir:  dir,
		WorktreeDir: dir,
		Instrument: store.Instrument{
			Cmd: []string{"sh", "-c", `printf '__TEXT\t__DATA\t__OBJC\tothers\tdec\thex\n4096\t0\t0\t1024\t5120\t1400\n'`},
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
}

func TestUnknownParser(t *testing.T) {
	_, err := instrument.Run(context.Background(), instrument.Config{
		Instrument: store.Instrument{Cmd: []string{"true"}, Parser: "builtin:nope"},
	})
	if err == nil {
		t.Error("unknown parser should error")
	}
}

func TestScalar_KeywordFormat(t *testing.T) {
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
}

func TestScalar_JsonFormat(t *testing.T) {
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
}

func TestScalar_CaseInsensitive(t *testing.T) {
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
}

func TestScalar_NoMatchRejected(t *testing.T) {
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
}

func TestScalar_MissingPatternRejected(t *testing.T) {
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
}

func TestScalar_MultipleGroupsRejected(t *testing.T) {
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
}

func TestScalar_NonIntegerCaptureRejected(t *testing.T) {
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
}

func TestScalar_UnitHonored(t *testing.T) {
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
}
