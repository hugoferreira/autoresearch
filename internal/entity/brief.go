package entity

import "time"

// BriefFileName is the name of the read-only context file written into an
// experiment worktree by `experiment implement`. Subagents (implementer,
// observer) read this snapshot instead of reaching back to the main store,
// which is unreachable from inside a worktree.
const BriefFileName = ".autoresearch-brief.json"

// Brief is a frozen snapshot of the research context an experiment was
// created under. It is written once by the CLI into the worktree root and
// never updated. Subagents treat it as read-only ground truth for the
// duration of their work.
type Brief struct {
	GeneratedAt         time.Time                 `json:"generated_at"`
	GeneratedBy         string                    `json:"generated_by"`
	Goal                BriefGoal                 `json:"goal"`
	Hypothesis          BriefHypothesis           `json:"hypothesis"`
	Experiment          BriefExperiment           `json:"experiment"`
	Lessons             []BriefLesson             `json:"lessons"`
	InstrumentContracts []BriefInstrumentContract `json:"instrument_contracts,omitempty"`
	ForbiddenChanges    []string                  `json:"forbidden_changes,omitempty"`
}

type BriefGoal struct {
	ID          string       `json:"id"`
	Objective   Objective    `json:"objective"`
	Constraints []Constraint `json:"constraints"`
	Steering    string       `json:"steering,omitempty"`
}

type BriefHypothesis struct {
	ID       string   `json:"id"`
	Claim    string   `json:"claim"`
	Predicts Predicts `json:"predicts"`
	KillIf   []string `json:"kill_if"`
}

type BriefExperiment struct {
	ID          string   `json:"id"`
	Instruments []string `json:"instruments"`
	Baseline    string   `json:"baseline"`
	BaselineSHA string   `json:"baseline_sha,omitempty"`
	Worktree    string   `json:"worktree"`
	Branch      string   `json:"branch"`
	DesignNotes string   `json:"design_notes,omitempty"`
	ImplNotes   string   `json:"impl_notes,omitempty"`
}

type BriefLesson struct {
	ID              string               `json:"id"`
	Claim           string               `json:"claim"`
	Scope           string               `json:"scope"`
	Status          string               `json:"status,omitempty"`
	SourceChain     string               `json:"source_chain,omitempty"`
	Tags            []string             `json:"tags,omitempty"`
	PredictedEffect *PredictedEffect     `json:"predicted_effect,omitempty"`
	Accuracy        *BriefLessonAccuracy `json:"accuracy,omitempty"`
}

type BriefLessonAccuracy struct {
	Total      int    `json:"total"`
	Hit        int    `json:"hit"`
	Overshoot  int    `json:"overshoot"`
	Undershoot int    `json:"undershoot"`
	Trend      string `json:"trend,omitempty"`
}

type BriefInstrumentContract struct {
	Name       string              `json:"name"`
	Cmd        []string            `json:"cmd,omitempty"`
	Parser     string              `json:"parser,omitempty"`
	Pattern    string              `json:"pattern,omitempty"`
	Unit       string              `json:"unit,omitempty"`
	MinSamples int                 `json:"min_samples,omitempty"`
	Requires   []string            `json:"requires,omitempty"`
	Evidence   []BriefEvidenceSpec `json:"evidence,omitempty"`
}

type BriefEvidenceSpec struct {
	Name string `json:"name"`
	Cmd  string `json:"cmd"`
}
