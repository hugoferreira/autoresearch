package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/readmodel"
)

func TestLessonListJSONSummarySupportsSince(t *testing.T) {
	saveGlobals(t)
	dir, s := setupGoalStore(t)
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)

	for _, l := range []*entity.Lesson{
		{
			ID:        "L-0001",
			Claim:     "old lesson",
			Scope:     entity.LessonScopeSystem,
			Status:    entity.LessonStatusActive,
			Author:    "agent:analyst",
			CreatedAt: now,
		},
		{
			ID:        "L-0002",
			Claim:     strings.Repeat("mechanism ", 30),
			Scope:     entity.LessonScopeSystem,
			Status:    entity.LessonStatusActive,
			Tags:      []string{"cache", "audit"},
			Author:    "agent:analyst",
			CreatedAt: now,
		},
		{
			ID:        "L-0003",
			Claim:     "superseded lesson",
			Scope:     entity.LessonScopeSystem,
			Status:    entity.LessonStatusSuperseded,
			Author:    "agent:analyst",
			CreatedAt: now,
		},
	} {
		if err := s.WriteLesson(l); err != nil {
			t.Fatalf("WriteLesson(%s): %v", l.ID, err)
		}
	}

	got := runCLIJSON[[]readmodel.LessonSummaryView](t, dir,
		"lesson", "list",
		"--status", "active",
		"--summary",
		"--since", "L-0001",
	)
	if gotLen, want := len(got), 1; gotLen != want {
		t.Fatalf("summary len = %d, want %d", gotLen, want)
	}
	if gotID, want := got[0].ID, "L-0002"; gotID != want {
		t.Fatalf("summary[0].ID = %q, want %q", gotID, want)
	}
	if !got[0].ClaimTruncated {
		t.Fatal("summary[0].claim_truncated = false, want true")
	}
	if gotLen, want := len([]rune(got[0].Claim)), readmodel.LessonSummaryClaimLimit; gotLen != want {
		t.Fatalf("summary claim rune len = %d, want %d", gotLen, want)
	}
	if gotTags, want := len(got[0].Tags), 2; gotTags != want {
		t.Fatalf("summary tags len = %d, want %d", gotTags, want)
	}
}

func TestLessonListJSONFieldsProjectsRequestedKeys(t *testing.T) {
	saveGlobals(t)
	dir, s := setupGoalStore(t)
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)

	if err := s.WriteLesson(&entity.Lesson{
		ID:        "L-0001",
		Claim:     "projected lesson",
		Scope:     entity.LessonScopeSystem,
		Status:    entity.LessonStatusActive,
		Tags:      []string{"cache"},
		Author:    "agent:analyst",
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("WriteLesson: %v", err)
	}

	got := runCLIJSON[[]map[string]any](t, dir,
		"lesson", "list",
		"--fields", "id,scope,tags,status",
	)
	if gotLen, want := len(got), 1; gotLen != want {
		t.Fatalf("projected len = %d, want %d", gotLen, want)
	}
	if gotKeys, want := len(got[0]), 4; gotKeys != want {
		t.Fatalf("projected key count = %d, want %d", gotKeys, want)
	}
	if gotID, want := got[0]["id"], any("L-0001"); gotID != want {
		t.Fatalf("projected id = %#v, want %#v", gotID, want)
	}
	if gotScope, want := got[0]["scope"], any(entity.LessonScopeSystem); gotScope != want {
		t.Fatalf("projected scope = %#v, want %#v", gotScope, want)
	}
	tags, ok := got[0]["tags"].([]any)
	if !ok {
		t.Fatalf("projected tags type = %T, want []any", got[0]["tags"])
	}
	if gotLen, want := len(tags), 1; gotLen != want || tags[0] != "cache" {
		t.Fatalf("projected tags = %+v, want [cache]", tags)
	}
}

func TestLessonListRejectsSummaryAndFieldsTogether(t *testing.T) {
	saveGlobals(t)
	dir, _ := setupGoalStore(t)

	root := Root()
	root.SetArgs([]string{
		"-C", dir,
		"lesson", "list",
		"--summary",
		"--fields", "id,status",
	})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected lesson list to reject --summary with --fields")
	}
	if !strings.Contains(err.Error(), "--summary and --fields cannot be combined") {
		t.Fatalf("unexpected error: %v", err)
	}
}
