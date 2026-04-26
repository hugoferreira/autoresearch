package readmodel

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
)

const DefaultRelevantLessonLimit = 8

type LessonRelevanceContext struct {
	Goal                *entity.Goal
	Hypothesis          *entity.Hypothesis
	OpenHypotheses      []*entity.Hypothesis
	InFlightExperiments []*entity.Experiment
	FrontierBest        *FrontierRow
	Conclusions         []*entity.Conclusion
	Hypotheses          []*entity.Hypothesis
	Limit               int
}

type RelevantLessonView struct {
	ID             string   `json:"id"`
	Score          int      `json:"score"`
	Reasons        []string `json:"reasons"`
	Scope          string   `json:"scope"`
	Status         string   `json:"status"`
	SourceChain    string   `json:"source_chain,omitempty"`
	Tags           []string `json:"tags"`
	Subjects       []string `json:"subjects,omitempty"`
	Claim          string   `json:"claim"`
	ClaimTruncated bool     `json:"claim_truncated"`
}

func RankRelevantLessons(views []*LessonReadView, ctx LessonRelevanceContext) []RelevantLessonView {
	if ctx.Limit <= 0 {
		ctx.Limit = DefaultRelevantLessonLimit
	}
	maxOrdinal := maxLessonOrdinal(views)
	focus := buildLessonRelevanceFocus(ctx)
	decisiveCitations := lessonDecisiveCitationCounts(ctx.Hypotheses, ctx.Conclusions)

	rows := make([]RelevantLessonView, 0, len(views))
	for _, view := range views {
		if view == nil || view.Lesson == nil {
			continue
		}
		row, eligible := scoreRelevantLesson(view, focus, decisiveCitations, maxOrdinal)
		if eligible {
			rows = append(rows, row)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Score != rows[j].Score {
			return rows[i].Score > rows[j].Score
		}
		return lessonIDSortValue(rows[i].ID) > lessonIDSortValue(rows[j].ID)
	})
	if len(rows) > ctx.Limit {
		rows = rows[:ctx.Limit]
	}
	if rows == nil {
		return []RelevantLessonView{}
	}
	return rows
}

type lessonRelevanceFocus struct {
	instruments       map[string]struct{}
	tags              map[string]struct{}
	subjectReasons    map[string]string
	currentInspiredBy map[string]struct{}
	openInspiredBy    map[string]struct{}
}

func buildLessonRelevanceFocus(ctx LessonRelevanceContext) lessonRelevanceFocus {
	f := lessonRelevanceFocus{
		instruments:       map[string]struct{}{},
		tags:              map[string]struct{}{},
		subjectReasons:    map[string]string{},
		currentInspiredBy: map[string]struct{}{},
		openInspiredBy:    map[string]struct{}{},
	}
	addInstrument := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		f.instruments[name] = struct{}{}
		f.tags[name] = struct{}{}
	}
	addTag := func(tag string) {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			f.tags[tag] = struct{}{}
		}
	}
	addSubject := func(id, reason string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := f.subjectReasons[id]; !ok {
			f.subjectReasons[id] = reason
		}
	}

	if ctx.Goal != nil {
		addInstrument(ctx.Goal.Objective.Instrument)
		for _, c := range ctx.Goal.Constraints {
			addInstrument(c.Instrument)
		}
		for _, r := range ctx.Goal.Rescuers {
			addInstrument(r.Instrument)
		}
	}
	if ctx.Hypothesis != nil {
		addSubject(ctx.Hypothesis.ID, "current hypothesis")
		addInstrument(ctx.Hypothesis.Predicts.Instrument)
		addTag(ctx.Hypothesis.Predicts.Target)
		for _, tag := range ctx.Hypothesis.Tags {
			addTag(tag)
		}
		for _, id := range ctx.Hypothesis.InspiredBy {
			f.currentInspiredBy[id] = struct{}{}
		}
	}
	if ctx.FrontierBest != nil {
		addSubject(ctx.FrontierBest.Hypothesis, "frontier hypothesis")
		addSubject(ctx.FrontierBest.Candidate, "frontier experiment")
		addSubject(ctx.FrontierBest.Conclusion, "frontier conclusion")
	}
	for _, h := range ctx.OpenHypotheses {
		if h == nil {
			continue
		}
		addSubject(h.ID, "open hypothesis")
		addInstrument(h.Predicts.Instrument)
		addTag(h.Predicts.Target)
		for _, tag := range h.Tags {
			addTag(tag)
		}
		for _, id := range h.InspiredBy {
			f.openInspiredBy[id] = struct{}{}
		}
	}
	for _, exp := range ctx.InFlightExperiments {
		if exp == nil {
			continue
		}
		addSubject(exp.ID, "in-flight experiment")
		addSubject(exp.Hypothesis, "in-flight hypothesis")
		for _, name := range exp.Instruments {
			addInstrument(name)
		}
	}
	return f
}

func scoreRelevantLesson(view *LessonReadView, focus lessonRelevanceFocus, decisiveCitations map[string]int, maxOrdinal int) (RelevantLessonView, bool) {
	claim, truncated := SummarizeLessonClaim(view.Claim, LessonSummaryClaimLimit)
	row := RelevantLessonView{
		ID:             view.ID,
		Scope:          view.Scope,
		Status:         view.Status,
		Tags:           copyStringSlice(view.Tags),
		Subjects:       copyStringSlice(view.Subjects),
		Claim:          claim,
		ClaimTruncated: truncated,
	}
	if view.Provenance != nil {
		row.SourceChain = view.Provenance.SourceChain
	}

	eligible := false
	add := func(points int, reason string) {
		row.Score += points
		addLessonRelevanceReason(&row, reason)
	}
	switch view.Status {
	case entity.LessonStatusActive:
		add(20, "active")
	case entity.LessonStatusProvisional:
		add(8, "provisional")
	case entity.LessonStatusInvalidated:
		add(-5, "invalidated")
	case entity.LessonStatusSuperseded:
		add(-20, "superseded")
	}
	if view.Scope == entity.LessonScopeSystem {
		add(8, "system scope")
	}
	if _, ok := focus.currentInspiredBy[view.ID]; ok {
		eligible = true
		add(100, "cited by current hypothesis")
	}
	if _, ok := focus.openInspiredBy[view.ID]; ok {
		eligible = true
		add(25, "cited by open hypothesis")
	}
	for _, subject := range view.Subjects {
		if reason, ok := focus.subjectReasons[subject]; ok {
			eligible = true
			add(60, "matches "+reason+" "+subject)
		}
	}
	if view.PredictedEffect != nil {
		if _, ok := focus.instruments[view.PredictedEffect.Instrument]; ok {
			eligible = true
			add(25, "predicts relevant instrument "+view.PredictedEffect.Instrument)
		}
	}
	matchingTags := lessonMatchingTags(view.Tags, focus.tags)
	if len(matchingTags) > 0 {
		eligible = true
		add(10*len(matchingTags), "tag match: "+strings.Join(matchingTags, ","))
	}
	if n := decisiveCitations[view.ID]; n > 0 {
		add(8*n, fmt.Sprintf("cited by %d decisive conclusion(s)", n))
	}
	if n := lessonIDSortValue(view.ID); n > 0 && maxOrdinal > 0 {
		switch age := maxOrdinal - n; {
		case age <= 2:
			add(5, "recent")
		case age <= 5:
			add(2, "recent")
		}
	}
	return row, eligible
}

func lessonMatchingTags(tags []string, focus map[string]struct{}) []string {
	var matches []string
	seen := map[string]struct{}{}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, ok := focus[tag]; !ok {
			continue
		}
		if _, dup := seen[tag]; dup {
			continue
		}
		seen[tag] = struct{}{}
		matches = append(matches, tag)
	}
	sort.Strings(matches)
	return matches
}

func lessonDecisiveCitationCounts(hyps []*entity.Hypothesis, concls []*entity.Conclusion) map[string]int {
	inspiredByHyp := map[string][]string{}
	for _, h := range hyps {
		if h == nil || len(h.InspiredBy) == 0 {
			continue
		}
		inspiredByHyp[h.ID] = h.InspiredBy
	}
	out := map[string]int{}
	for _, c := range concls {
		if c == nil {
			continue
		}
		if c.Verdict != entity.VerdictSupported && c.Verdict != entity.VerdictRefuted {
			continue
		}
		for _, id := range inspiredByHyp[c.Hypothesis] {
			out[id]++
		}
	}
	return out
}

func addLessonRelevanceReason(row *RelevantLessonView, reason string) {
	if strings.TrimSpace(reason) == "" || slices.Contains(row.Reasons, reason) {
		return
	}
	row.Reasons = append(row.Reasons, reason)
}

func maxLessonOrdinal(views []*LessonReadView) int {
	maxN := 0
	for _, view := range views {
		if view == nil {
			continue
		}
		if n := lessonIDSortValue(view.ID); n > maxN {
			maxN = n
		}
	}
	return maxN
}

func lessonIDSortValue(id string) int {
	n, err := parseLessonOrdinal(id)
	if err != nil {
		return 0
	}
	return n
}
