package cli

// View kinds. Used by tuiModel.jumpTo to canonicalize the breadcrumb when a
// top-level jump key is pressed, so pressing H repeatedly does not stack
// multiple hypothesis lists. Keep the strings stable — if a future test
// asserts on kinds it will anchor on these values.
const (
	kindDashboard        = "dashboard"
	kindHypothesisList   = "hypothesis.list"
	kindHypothesisReport = "hypothesis.list.report" // list in "pick for report" mode
	kindHypothesisDetail = "hypothesis.detail"
	kindExperimentList   = "experiment.list"
	kindExperimentDetail = "experiment.detail"
	kindConclusionList   = "conclusion.list"
	kindConclusionDetail = "conclusion.detail"
	kindEventList        = "event.list"
	kindEventDetail      = "event.detail"
	kindTree             = "tree"
	kindFrontier         = "frontier"
	kindGoal             = "goal"
	kindStatus           = "status"
	kindArtifactList     = "artifact.list"
	kindArtifactView     = "artifact.view"
	kindArtifactDiff     = "artifact.diff"
	kindReport           = "report"
	kindInstrumentList   = "instrument.list"
	kindLessonList       = "lesson.list"
	kindLessonDetail     = "lesson.detail"
)

func (v *dashboardView) kind() string         { return kindDashboard }
func (v *hypothesisListView) kind() string {
	if v.reportMode {
		return kindHypothesisReport
	}
	return kindHypothesisList
}
func (v *hypothesisDetailView) kind() string  { return kindHypothesisDetail }
func (v *experimentListView) kind() string    { return kindExperimentList }
func (v *experimentDetailView) kind() string  { return kindExperimentDetail }
func (v *conclusionListView) kind() string    { return kindConclusionList }
func (v *conclusionDetailView) kind() string  { return kindConclusionDetail }
func (v *eventListView) kind() string         { return kindEventList }
func (v *eventDetailView) kind() string       { return kindEventDetail }
func (v *treeView) kind() string              { return kindTree }
func (v *frontierView) kind() string          { return kindFrontier }
func (v *goalView) kind() string              { return kindGoal }
func (v *statusView) kind() string            { return kindStatus }
func (v *artifactListView) kind() string      { return kindArtifactList }
func (v *artifactView) kind() string          { return kindArtifactView }
func (v *artifactDiffView) kind() string      { return kindArtifactDiff }
func (v *reportView) kind() string            { return kindReport }
func (v *instrumentListView) kind() string    { return kindInstrumentList }
func (v *lessonListView) kind() string        { return kindLessonList }
func (v *lessonDetailView) kind() string      { return kindLessonDetail }
