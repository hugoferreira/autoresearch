package readmodel

import (
	"sort"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
)

type ScratchWorkspaceView struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	FromRef    string     `json:"from_ref"`
	FromSHA    string     `json:"from_sha"`
	Worktree   string     `json:"worktree"`
	Branch     string     `json:"branch"`
	CreatedAt  time.Time  `json:"created_at"`
	AgeMinutes float64    `json:"age_minutes"`
	CleanedAt  *time.Time `json:"cleaned_at,omitempty"`
}

func BuildScratchWorkspaceViews(scratch []*entity.Scratch, now time.Time) []ScratchWorkspaceView {
	out := make([]ScratchWorkspaceView, 0, len(scratch))
	for _, sc := range scratch {
		if sc == nil {
			continue
		}
		out = append(out, ScratchWorkspaceView{
			ID:         sc.ID,
			Name:       sc.Name,
			Status:     sc.EffectiveStatus(),
			FromRef:    sc.FromRef,
			FromSHA:    sc.FromSHA,
			Worktree:   sc.Worktree,
			Branch:     sc.Branch,
			CreatedAt:  sc.CreatedAt,
			AgeMinutes: now.Sub(sc.CreatedAt).Minutes(),
			CleanedAt:  sc.CleanedAt,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

func ActiveScratchWorkspaces(scratch []*entity.Scratch, now time.Time) []ScratchWorkspaceView {
	views := BuildScratchWorkspaceViews(scratch, now)
	out := make([]ScratchWorkspaceView, 0, len(views))
	for _, view := range views {
		if view.Status == entity.ScratchStatusActive {
			out = append(out, view)
		}
	}
	return out
}

func StaleScratchWorkspaces(scratch []*entity.Scratch, threshold time.Duration, now time.Time) []ScratchWorkspaceView {
	if threshold <= 0 {
		return nil
	}
	active := ActiveScratchWorkspaces(scratch, now)
	out := make([]ScratchWorkspaceView, 0, len(active))
	for _, view := range active {
		if now.Sub(view.CreatedAt) >= threshold {
			out = append(out, view)
		}
	}
	return out
}
