package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
)

func seedHypothesisForLifecycleGuard(t *testing.T, s *store.Store, status string) string {
	t.Helper()
	now := time.Now().UTC()
	h := &entity.Hypothesis{
		ID:        "H-0001",
		GoalID:    "G-0001",
		Claim:     "tighten loop",
		Predicts:  entity.Predicts{Instrument: "timing", Target: "fir", Direction: "decrease", MinEffect: 0.1},
		KillIf:    []string{"tests fail"},
		Status:    status,
		Author:    "agent:analyst",
		CreatedAt: now,
	}
	if err := s.WriteHypothesis(h); err != nil {
		t.Fatal(err)
	}
	return h.ID
}

func TestConcludeRejectsNonReopenedLifecycleStates(t *testing.T) {
	cases := []struct {
		name       string
		status     string
		wantSubstr string
	}{
		{
			name:       "unreviewed",
			status:     entity.StatusUnreviewed,
			wantSubstr: "resolve the pending conclusion",
		},
		{
			name:       "supported",
			status:     entity.StatusSupported,
			wantSubstr: "use `conclusion withdraw` or `conclusion downgrade` before concluding again",
		},
		{
			name:       "refuted",
			status:     entity.StatusRefuted,
			wantSubstr: "use `conclusion withdraw` or `conclusion downgrade` before concluding again",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			saveGlobals(t)
			dir, s := setupGoalStore(t)
			hypID := seedHypothesisForLifecycleGuard(t, s, tc.status)

			root := Root()
			root.SetArgs([]string{
				"-C", dir,
				"conclude", hypID,
				"--verdict", "supported",
				"--observations", "O-0001",
			})
			err := root.Execute()
			if err == nil {
				t.Fatal("expected conclude to be rejected")
			}
			if !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Fatalf("error = %q, want substring %q", err, tc.wantSubstr)
			}
		})
	}
}

func TestHypothesisKillRejectsDecisiveOrPendingStates(t *testing.T) {
	cases := []struct {
		name       string
		status     string
		wantSubstr string
	}{
		{
			name:       "unreviewed",
			status:     entity.StatusUnreviewed,
			wantSubstr: "resolve the pending conclusion",
		},
		{
			name:       "supported",
			status:     entity.StatusSupported,
			wantSubstr: "use `conclusion withdraw` or `conclusion downgrade` before killing it",
		},
		{
			name:       "refuted",
			status:     entity.StatusRefuted,
			wantSubstr: "use `conclusion withdraw` or `conclusion downgrade` before killing it",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			saveGlobals(t)
			dir, s := setupGoalStore(t)
			hypID := seedHypothesisForLifecycleGuard(t, s, tc.status)

			root := Root()
			root.SetArgs([]string{
				"-C", dir,
				"hypothesis", "kill", hypID,
				"--reason", "stop this line",
			})
			err := root.Execute()
			if err == nil {
				t.Fatal("expected hypothesis kill to be rejected")
			}
			if !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Fatalf("error = %q, want substring %q", err, tc.wantSubstr)
			}
		})
	}
}
