package readmodel

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/firewall"
	"github.com/bytter/autoresearch/internal/store"
)

const LessonSummaryClaimLimit = 160

// LessonReadView is the read-side projection used by lesson list/show and
// other read-only lesson surfaces.
type LessonReadView struct {
	*entity.Lesson
}

// LessonListOptions captures the shared read-side filters for lesson list
// surfaces. SinceID is exclusive: rows must have a larger lesson ordinal.
type LessonListOptions struct {
	Scope   string
	Status  string
	Subject string
	Tag     string
	SinceID string
}

// LessonSummaryView is the low-context lesson projection intended for boot-time
// orchestrator reads and other agent consumers that only need notebook
// steering, not the full lesson body.
type LessonSummaryView struct {
	ID             string   `json:"id"`
	Scope          string   `json:"scope"`
	Status         string   `json:"status"`
	Tags           []string `json:"tags"`
	Claim          string   `json:"claim"`
	ClaimTruncated bool     `json:"claim_truncated"`
}

type LessonField string

const (
	LessonFieldID              LessonField = "id"
	LessonFieldClaim           LessonField = "claim"
	LessonFieldScope           LessonField = "scope"
	LessonFieldSubjects        LessonField = "subjects"
	LessonFieldTags            LessonField = "tags"
	LessonFieldStatus          LessonField = "status"
	LessonFieldSourceChain     LessonField = "source_chain"
	LessonFieldPredictedEffect LessonField = "predicted_effect"
	LessonFieldSupersedes      LessonField = "supersedes"
	LessonFieldSupersededBy    LessonField = "superseded_by"
	LessonFieldAuthor          LessonField = "author"
	LessonFieldCreatedAt       LessonField = "created_at"
	LessonFieldBody            LessonField = "body"
)

var lessonFieldSet = map[LessonField]struct{}{
	LessonFieldID:              {},
	LessonFieldClaim:           {},
	LessonFieldScope:           {},
	LessonFieldSubjects:        {},
	LessonFieldTags:            {},
	LessonFieldStatus:          {},
	LessonFieldSourceChain:     {},
	LessonFieldPredictedEffect: {},
	LessonFieldSupersedes:      {},
	LessonFieldSupersededBy:    {},
	LessonFieldAuthor:          {},
	LessonFieldCreatedAt:       {},
	LessonFieldBody:            {},
}

func LessonStatusForSourceChain(sourceChain string) (string, bool) {
	switch sourceChain {
	case entity.LessonSourceSystem, entity.LessonSourceReviewedDecisive:
		return entity.LessonStatusActive, true
	case entity.LessonSourceUnreviewedDecisive:
		return entity.LessonStatusProvisional, true
	case entity.LessonSourceInconclusive:
		return entity.LessonStatusInvalidated, true
	default:
		return "", false
	}
}

func LessonDisplayStatus(l *entity.Lesson, sourceChain string) string {
	if l != nil && l.EffectiveStatus() == entity.LessonStatusSuperseded {
		return entity.LessonStatusSuperseded
	}
	if status, ok := LessonStatusForSourceChain(sourceChain); ok {
		return status
	}
	if l == nil {
		return entity.LessonStatusActive
	}
	return l.EffectiveStatus()
}

func AnnotateLessonForRead(s *store.Store, l *entity.Lesson) (*LessonReadView, error) {
	if l == nil {
		return nil, nil
	}
	view := *l
	sourceChain, err := firewall.AssessLessonSourceChain(s, l)
	if err != nil {
		// Legacy lessons may still be readable even if a referenced subject
		// has gone missing. Preserve the stored metadata rather than failing
		// the whole read surface.
		sourceChain = l.EffectiveSourceChain()
		if sourceChain == "" && l.Scope == entity.LessonScopeSystem {
			sourceChain = entity.LessonSourceSystem
		}
	}
	view.Provenance = &entity.LessonProvenance{SourceChain: sourceChain}
	view.Status = LessonDisplayStatus(l, sourceChain)
	return &LessonReadView{Lesson: &view}, nil
}

func AnnotateLessonsForRead(s *store.Store, lessons []*entity.Lesson) ([]*LessonReadView, error) {
	out := make([]*LessonReadView, 0, len(lessons))
	for _, l := range lessons {
		view, err := AnnotateLessonForRead(s, l)
		if err != nil {
			return nil, err
		}
		if view != nil {
			out = append(out, view)
		}
	}
	return out, nil
}

func ListLessonsForRead(s *store.Store, lessons []*entity.Lesson, opts LessonListOptions) ([]*LessonReadView, error) {
	views, err := AnnotateLessonsForRead(s, lessons)
	if err != nil {
		return nil, err
	}
	return FilterLessonReadViews(views, opts)
}

func FilterLessonReadViews(views []*LessonReadView, opts LessonListOptions) ([]*LessonReadView, error) {
	sinceOrdinal := 0
	if strings.TrimSpace(opts.SinceID) != "" {
		n, err := parseLessonOrdinal(opts.SinceID)
		if err != nil {
			return nil, err
		}
		sinceOrdinal = n
	}

	out := make([]*LessonReadView, 0, len(views))
	for _, view := range views {
		if view == nil || view.Lesson == nil {
			continue
		}
		if sinceOrdinal > 0 {
			n, err := parseLessonOrdinal(view.ID)
			if err != nil {
				return nil, err
			}
			if n <= sinceOrdinal {
				continue
			}
		}
		if opts.Scope != "" && view.Scope != opts.Scope {
			continue
		}
		if opts.Status != "" && view.Status != opts.Status {
			continue
		}
		if opts.Subject != "" && !slices.Contains(view.Subjects, opts.Subject) {
			continue
		}
		if opts.Tag != "" && !slices.Contains(view.Tags, opts.Tag) {
			continue
		}
		out = append(out, view)
	}
	return out, nil
}

func BuildLessonSummaryViews(views []*LessonReadView) []LessonSummaryView {
	out := make([]LessonSummaryView, 0, len(views))
	for _, view := range views {
		if view == nil || view.Lesson == nil {
			continue
		}
		claim, truncated := SummarizeLessonClaim(view.Claim, LessonSummaryClaimLimit)
		out = append(out, LessonSummaryView{
			ID:             view.ID,
			Scope:          view.Scope,
			Status:         view.Status,
			Tags:           copyStringSlice(view.Tags),
			Claim:          claim,
			ClaimTruncated: truncated,
		})
	}
	return out
}

func SummarizeLessonClaim(claim string, limit int) (string, bool) {
	if limit <= 0 {
		limit = LessonSummaryClaimLimit
	}
	oneLine := strings.Join(strings.Fields(strings.TrimSpace(claim)), " ")
	if oneLine == "" {
		return "", false
	}
	runes := []rune(oneLine)
	if len(runes) <= limit {
		return oneLine, false
	}
	if limit <= 1 {
		return "…", true
	}
	return string(runes[:limit-1]) + "…", true
}

func ParseLessonFields(spec string) ([]LessonField, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, errors.New("--fields requires a comma-separated list")
	}
	parts := strings.Split(spec, ",")
	fields := make([]LessonField, 0, len(parts))
	seen := map[LessonField]bool{}
	for _, part := range parts {
		field := LessonField(strings.TrimSpace(part))
		if field == "" {
			return nil, errors.New("--fields contains an empty field name")
		}
		if _, ok := lessonFieldSet[field]; !ok {
			return nil, fmt.Errorf("unknown lesson field %q", field)
		}
		if seen[field] {
			continue
		}
		seen[field] = true
		fields = append(fields, field)
	}
	return fields, nil
}

func ProjectLessonReadViews(views []*LessonReadView, fields []LessonField) []map[string]any {
	out := make([]map[string]any, 0, len(views))
	for _, view := range views {
		if view == nil || view.Lesson == nil {
			continue
		}
		row := make(map[string]any, len(fields))
		for _, field := range fields {
			row[string(field)] = lessonFieldValue(view, field)
		}
		out = append(out, row)
	}
	return out
}

func lessonFieldValue(view *LessonReadView, field LessonField) any {
	switch field {
	case LessonFieldID:
		return view.ID
	case LessonFieldClaim:
		return view.Claim
	case LessonFieldScope:
		return view.Scope
	case LessonFieldSubjects:
		return copyStringSlice(view.Subjects)
	case LessonFieldTags:
		return copyStringSlice(view.Tags)
	case LessonFieldStatus:
		return view.Status
	case LessonFieldSourceChain:
		if view.Provenance == nil {
			return ""
		}
		return view.Provenance.SourceChain
	case LessonFieldPredictedEffect:
		return view.PredictedEffect
	case LessonFieldSupersedes:
		return view.SupersedesID
	case LessonFieldSupersededBy:
		return view.SupersededByID
	case LessonFieldAuthor:
		return view.Author
	case LessonFieldCreatedAt:
		return view.CreatedAt
	case LessonFieldBody:
		return view.Body
	default:
		return nil
	}
}

func copyStringSlice(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func parseLessonOrdinal(id string) (int, error) {
	id = strings.TrimSpace(id)
	if !strings.HasPrefix(id, "L-") {
		return 0, fmt.Errorf("--since %q: want L-NNNN", id)
	}
	n, err := strconv.Atoi(id[2:])
	if err != nil || n < 0 {
		return 0, fmt.Errorf("--since %q: want L-NNNN", id)
	}
	return n, nil
}
