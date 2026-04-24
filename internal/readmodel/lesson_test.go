package readmodel

import (
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/testkit"
)

var _ = testkit.Spec("TestListLessonsForRead_FiltersAndSince", func(t testkit.T) {
	f := newBaselineFixture(t)
	lessons := []*entity.Lesson{
		{
			ID:        "L-0001",
			Claim:     "system lesson",
			Scope:     entity.LessonScopeSystem,
			Status:    entity.LessonStatusActive,
			Tags:      []string{"cache"},
			Author:    "agent:analyst",
			CreatedAt: f.now,
		},
		{
			ID:        "L-0002",
			Claim:     "linked lesson",
			Scope:     entity.LessonScopeHypothesis,
			Subjects:  []string{"H-0007"},
			Status:    entity.LessonStatusProvisional,
			Tags:      []string{"cache", "audit"},
			Author:    "agent:analyst",
			CreatedAt: f.now,
		},
		{
			ID:        "L-0003",
			Claim:     "superseded lesson",
			Scope:     entity.LessonScopeSystem,
			Status:    entity.LessonStatusSuperseded,
			Tags:      []string{"cache"},
			Author:    "agent:analyst",
			CreatedAt: f.now,
		},
	}

	got, err := ListLessonsForRead(f.s, lessons, LessonListOptions{
		SinceID: "L-0001",
		Status:  entity.LessonStatusProvisional,
		Subject: "H-0007",
		Tag:     "audit",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotLen, want := len(got), 1; gotLen != want {
		t.Fatalf("filtered len = %d, want %d", gotLen, want)
	}
	if gotID, want := got[0].ID, "L-0002"; gotID != want {
		t.Fatalf("filtered[0].ID = %q, want %q", gotID, want)
	}
})

var _ = testkit.Spec("TestListLessonsForRead_SinceUsesOrdinalNotLexicalOrder", func(t testkit.T) {
	f := newBaselineFixture(t)
	for _, l := range []*entity.Lesson{
		{
			ID:        "L-10000",
			Claim:     "newer",
			Scope:     entity.LessonScopeSystem,
			Status:    entity.LessonStatusActive,
			Author:    "agent:analyst",
			CreatedAt: f.now,
		},
		{
			ID:        "L-9999",
			Claim:     "older",
			Scope:     entity.LessonScopeSystem,
			Status:    entity.LessonStatusActive,
			Author:    "agent:analyst",
			CreatedAt: f.now,
		},
		{
			ID:        "L-10001",
			Claim:     "newest",
			Scope:     entity.LessonScopeSystem,
			Status:    entity.LessonStatusActive,
			Author:    "agent:analyst",
			CreatedAt: f.now,
		},
	} {
		if err := f.s.WriteLesson(l); err != nil {
			t.Fatalf("WriteLesson(%s): %v", l.ID, err)
		}
	}

	lessons, err := f.s.ListLessons()
	if err != nil {
		t.Fatalf("ListLessons: %v", err)
	}

	got, err := ListLessonsForRead(f.s, lessons, LessonListOptions{SinceID: "L-9998"})
	if err != nil {
		t.Fatal(err)
	}
	if gotLen, want := len(got), 3; gotLen != want {
		t.Fatalf("filtered len = %d, want %d", gotLen, want)
	}
	wantIDs := []string{"L-9999", "L-10000", "L-10001"}
	for i, want := range wantIDs {
		if got[i].ID != want {
			t.Fatalf("filtered[%d].ID = %q, want %q", i, got[i].ID, want)
		}
	}
})

var _ = testkit.Spec("TestBuildLessonSummaryViews_TruncatesClaimAndPreservesTags", func(t testkit.T) {
	view := &LessonReadView{Lesson: &entity.Lesson{
		ID:     "L-0007",
		Claim:  strings.Repeat("mechanism ", 30),
		Scope:  entity.LessonScopeSystem,
		Status: entity.LessonStatusActive,
		Tags:   []string{"cache", "audit"},
		Author: "agent:analyst",
	}}

	got := BuildLessonSummaryViews([]*LessonReadView{view})
	if gotLen, want := len(got), 1; gotLen != want {
		t.Fatalf("summary len = %d, want %d", gotLen, want)
	}
	if !got[0].ClaimTruncated {
		t.Fatal("claim_truncated = false, want true")
	}
	if gotLen, want := len([]rune(got[0].Claim)), LessonSummaryClaimLimit; gotLen != want {
		t.Fatalf("summary claim rune len = %d, want %d", gotLen, want)
	}
	if got[0].Tags[0] != "cache" || got[0].Tags[1] != "audit" {
		t.Fatalf("summary tags = %+v", got[0].Tags)
	}
})

var _ = testkit.Spec("TestParseLessonFieldsAndProject", func(t testkit.T) {
	fields, err := ParseLessonFields("id,scope,tags,status,source_chain,id")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(fields), 5; got != want {
		t.Fatalf("fields len = %d, want %d", got, want)
	}

	rows := ProjectLessonReadViews([]*LessonReadView{{
		Lesson: &entity.Lesson{
			ID:     "L-0001",
			Claim:  "system lesson",
			Scope:  entity.LessonScopeSystem,
			Status: entity.LessonStatusActive,
			Tags:   []string{"cache"},
			Author: "agent:analyst",
			Provenance: &entity.LessonProvenance{
				SourceChain: entity.LessonSourceSystem,
			},
		},
	}}, fields)

	if got, want := len(rows), 1; got != want {
		t.Fatalf("projected rows len = %d, want %d", got, want)
	}
	if got, want := rows[0]["id"], any("L-0001"); got != want {
		t.Fatalf("projected id = %#v, want %#v", got, want)
	}
	tags, ok := rows[0]["tags"].([]string)
	if !ok {
		t.Fatalf("projected tags type = %T, want []string", rows[0]["tags"])
	}
	if got, want := len(tags), 1; got != want || tags[0] != "cache" {
		t.Fatalf("projected tags = %+v, want [cache]", tags)
	}
	if got, want := rows[0]["source_chain"], any(entity.LessonSourceSystem); got != want {
		t.Fatalf("projected source_chain = %#v, want %#v", got, want)
	}
})

var _ = testkit.Spec("TestParseLessonFields_RejectsUnknownField", func(t testkit.T) {
	if _, err := ParseLessonFields("id,nope"); err == nil {
		t.Fatal("ParseLessonFields succeeded for unknown field")
	}
})
