package entity_test

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/testkit"
)

var _ = testkit.Spec("TestLessonRoundTrip", func(t testkit.T) {
	l := &entity.Lesson{
		ID:         "L-0002",
		Claim:      "Loop unrolling past 8× shows no win on FIR_NTAPS=32 — cache line pressure dominates.",
		Scope:      entity.LessonScopeHypothesis,
		Subjects:   []string{"H-0003", "C-0003", "C-0005"},
		Tags:       []string{"cache", "unroll"},
		Status:     entity.LessonStatusProvisional,
		Provenance: &entity.LessonProvenance{SourceChain: entity.LessonSourceUnreviewedDecisive},
		Author:     "agent:analyst",
		CreatedAt:  time.Date(2026, 4, 12, 10, 0, 0, 0, time.UTC),
		Body:       "# Lesson\n\nObserved across three experiments; DeltaFrac plateaus at ~-0.07.\n",
	}
	data, err := l.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	back, err := entity.ParseLesson(data)
	if err != nil {
		t.Fatal(err)
	}
	if back.ID != l.ID || back.Claim != l.Claim {
		t.Errorf("round trip mismatch: %+v", back)
	}
	if back.Scope != entity.LessonScopeHypothesis {
		t.Errorf("scope: %q", back.Scope)
	}
	if len(back.Subjects) != 3 {
		t.Errorf("subjects: %+v", back.Subjects)
	}
	if back.Provenance == nil || back.Provenance.SourceChain != entity.LessonSourceUnreviewedDecisive {
		t.Errorf("provenance round-trip mismatch: %+v", back.Provenance)
	}
	if back.Body != l.Body {
		t.Errorf("body round-trip:\n want: %q\n  got: %q", l.Body, back.Body)
	}
})

var _ = testkit.Spec("TestLessonSupersedeChain", func(t testkit.T) {
	l := &entity.Lesson{
		ID:             "L-0001",
		Claim:          "obsolete",
		Scope:          entity.LessonScopeSystem,
		Status:         entity.LessonStatusSuperseded,
		SupersededByID: "L-0002",
		CreatedAt:      time.Now().UTC(),
	}
	data, err := l.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	back, err := entity.ParseLesson(data)
	if err != nil {
		t.Fatal(err)
	}
	if back.Status != entity.LessonStatusSuperseded {
		t.Errorf("status lost: %q", back.Status)
	}
	if back.SupersededByID != "L-0002" {
		t.Errorf("superseded_by lost: %q", back.SupersededByID)
	}
})

var _ = testkit.Spec("TestLessonBodyJSON", func(t testkit.T) {
	l := &entity.Lesson{
		ID: "L-0001", Claim: "x", Scope: entity.LessonScopeSystem, Status: entity.LessonStatusActive,
		Body: "# Lesson\n\nsome insight\n",
	}
	data, err := json.Marshal(l)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"body"`)) {
		t.Errorf("json output missing body key: %s", data)
	}
	var back entity.Lesson
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.Body != l.Body {
		t.Errorf("body json round-trip:\n want: %q\n  got: %q", l.Body, back.Body)
	}
})
