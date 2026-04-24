package store_test

import (
	"os"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/bytter/autoresearch/internal/testkit"
	"github.com/onsi/ginkgo/v2"
)

var _ = ginkgo.Describe("TestLessonCRUD", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		s, _ := mustCreate(t)

		id, err := s.AllocID(store.KindLesson)
		if err != nil {
			t.Fatal(err)
		}
		if id != "L-0001" {
			t.Errorf("first lesson id: got %q, want L-0001", id)
		}
		l := &entity.Lesson{
			ID:        id,
			Claim:     "cache line pressure dominates past 8× unroll",
			Scope:     entity.LessonScopeHypothesis,
			Subjects:  []string{"H-0003", "C-0005"},
			Tags:      []string{"cache"},
			Status:    entity.LessonStatusActive,
			Author:    "agent:analyst",
			CreatedAt: time.Now().UTC(),
		}
		if err := s.WriteLesson(l); err != nil {
			t.Fatal(err)
		}

		back, err := s.ReadLesson(id)
		if err != nil {
			t.Fatal(err)
		}
		if back.Claim != l.Claim {
			t.Errorf("claim mismatch: %q vs %q", back.Claim, l.Claim)
		}
		if back.Scope != entity.LessonScopeHypothesis {
			t.Errorf("scope lost: %q", back.Scope)
		}

		list, err := s.ListLessons()
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 1 || list[0].ID != id {
			t.Errorf("list: %+v", list)
		}

		// Exists check.
		ok, err := s.LessonExists(id)
		if err != nil || !ok {
			t.Errorf("LessonExists: ok=%v err=%v", ok, err)
		}
		ok, err = s.LessonExists("L-9999")
		if err != nil || ok {
			t.Errorf("LessonExists(L-9999): ok=%v err=%v", ok, err)
		}
	})
})

var _ = ginkgo.Describe("TestLessonListByScopeAndSubject", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		s, _ := mustCreate(t)
		now := time.Now().UTC()

		hyp := &entity.Lesson{
			ID: "L-0001", Claim: "hyp lesson",
			Scope: entity.LessonScopeHypothesis, Subjects: []string{"H-0001", "C-0002"},
			Status: entity.LessonStatusActive, Author: "agent:analyst", CreatedAt: now,
		}
		sys := &entity.Lesson{
			ID: "L-0002", Claim: "system lesson",
			Scope:  entity.LessonScopeSystem,
			Status: entity.LessonStatusActive, Author: "agent:critic", CreatedAt: now,
		}
		if err := s.WriteLesson(hyp); err != nil {
			t.Fatal(err)
		}
		if err := s.WriteLesson(sys); err != nil {
			t.Fatal(err)
		}

		hypLessons, err := s.ListLessonsByScope(entity.LessonScopeHypothesis)
		if err != nil {
			t.Fatal(err)
		}
		if len(hypLessons) != 1 || hypLessons[0].ID != "L-0001" {
			t.Errorf("scope=hypothesis filter: %+v", hypLessons)
		}

		sysLessons, err := s.ListLessonsByScope(entity.LessonScopeSystem)
		if err != nil {
			t.Fatal(err)
		}
		if len(sysLessons) != 1 || sysLessons[0].ID != "L-0002" {
			t.Errorf("scope=system filter: %+v", sysLessons)
		}

		bySubject, err := s.ListLessonsForSubject("C-0002")
		if err != nil {
			t.Fatal(err)
		}
		if len(bySubject) != 1 || bySubject[0].ID != "L-0001" {
			t.Errorf("subject filter: %+v", bySubject)
		}

		none, err := s.ListLessonsForSubject("C-9999")
		if err != nil {
			t.Fatal(err)
		}
		if len(none) != 0 {
			t.Errorf("subject filter miss should be empty: %+v", none)
		}
	})
})

var _ = ginkgo.Describe("TestListLessonsOnMissingDir", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		// Older .research/ installs pre-date the lessons/ directory. ListLessons
		// must tolerate the missing directory and return an empty slice instead
		// of erroring out — the dashboard calls it on every refresh.
		s, _ := mustCreate(t)
		// Delete the directory that Create() wrote.
		if err := os.RemoveAll(s.LessonsDir()); err != nil {
			t.Fatal(err)
		}
		list, err := s.ListLessons()
		if err != nil {
			t.Fatalf("ListLessons on missing dir: %v", err)
		}
		if len(list) != 0 {
			t.Errorf("empty list expected, got %+v", list)
		}

		// Counts() should also tolerate it.
		counts, err := s.Counts()
		if err != nil {
			t.Fatalf("Counts on missing lessons dir: %v", err)
		}
		if counts["lessons"] != 0 {
			t.Errorf("counts lessons: got %d, want 0", counts["lessons"])
		}

		// WriteLesson should recreate the directory lazily.
		l := &entity.Lesson{
			ID: "L-0001", Claim: "x", Scope: entity.LessonScopeSystem,
			Status: entity.LessonStatusActive, Author: "agent:analyst", CreatedAt: time.Now().UTC(),
		}
		if err := s.WriteLesson(l); err != nil {
			t.Fatalf("WriteLesson should recreate the directory: %v", err)
		}
		back, err := s.ReadLesson("L-0001")
		if err != nil || back.Claim != "x" {
			t.Errorf("round-trip after lazy recreation failed: err=%v back=%+v", err, back)
		}
	})
})

var _ = ginkgo.Describe("TestListLessonsOrdersByNumericIDAcrossFiveDigits", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		s, _ := mustCreate(t)
		now := time.Now().UTC()

		for _, l := range []*entity.Lesson{
			{ID: "L-10000", Claim: "ten thousand", Scope: entity.LessonScopeSystem, Status: entity.LessonStatusActive, Author: "agent:analyst", CreatedAt: now},
			{ID: "L-9998", Claim: "nine nine nine eight", Scope: entity.LessonScopeSystem, Status: entity.LessonStatusActive, Author: "agent:analyst", CreatedAt: now},
			{ID: "L-10001", Claim: "ten thousand one", Scope: entity.LessonScopeSystem, Status: entity.LessonStatusActive, Author: "agent:analyst", CreatedAt: now},
			{ID: "L-9999", Claim: "nine nine nine nine", Scope: entity.LessonScopeSystem, Status: entity.LessonStatusActive, Author: "agent:analyst", CreatedAt: now},
		} {
			if err := s.WriteLesson(l); err != nil {
				t.Fatalf("WriteLesson(%s): %v", l.ID, err)
			}
		}

		got, err := s.ListLessons()
		if err != nil {
			t.Fatalf("ListLessons: %v", err)
		}
		if gotLen, want := len(got), 4; gotLen != want {
			t.Fatalf("ListLessons len = %d, want %d", gotLen, want)
		}

		wantIDs := []string{"L-9998", "L-9999", "L-10000", "L-10001"}
		for i, want := range wantIDs {
			if got[i].ID != want {
				t.Fatalf("ListLessons[%d].ID = %q, want %q (full=%v)", i, got[i].ID, want, []string{got[0].ID, got[1].ID, got[2].ID, got[3].ID})
			}
		}
	})
})
