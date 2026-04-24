package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/firewall"
	"github.com/bytter/autoresearch/internal/instrument"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/bytter/autoresearch/internal/worktree"
)

const (
	observeActionRecorded = "recorded"
	observeActionSkipped  = "skipped"
)

// observationResult holds the output of a single instrument observation,
// used by observeAll to collect results for display.
type observationResult struct {
	ID             string   `json:"id,omitempty"`
	IDs            []string `json:"ids,omitempty"`
	Inst           string   `json:"instrument"`
	Value          float64  `json:"value,omitempty"`
	Unit           string   `json:"unit,omitempty"`
	Action         string   `json:"action,omitempty"`
	Samples        int      `json:"samples,omitempty"`
	CurrentSamples int      `json:"current_samples,omitempty"`
	TargetSamples  int      `json:"target_samples,omitempty"`
}

func (r observationResult) skipped() bool {
	return r.Action == observeActionSkipped
}

type observeExecution struct {
	Check        observeSampleCheck
	Result       observationResult
	Observations []*entity.Observation
	Latest       *entity.Observation
}

type observeScope struct {
	Attempt      int
	CandidateRef string
	CandidateSHA string
}

type observeAllExecution struct {
	Results             []observationResult
	CurrentObservations []*entity.Observation
	NewObservations     []*entity.Observation
}

type observeResultSummary struct {
	Action        string
	CurrentIDs    []string
	RecordedIDs   []string
	ReusedIDs     []string
	RecordedCount int
	SkippedCount  int
}

type observeSampleCheck struct {
	Experiment        string `json:"experiment"`
	Instrument        string `json:"instrument"`
	RequestedSamples  int    `json:"requested_samples,omitempty"`
	MinSamples        int    `json:"min_samples,omitempty"`
	MinSatisfied      bool   `json:"min_satisfied"`
	CurrentSamples    int    `json:"current_samples"`
	TargetSamples     int    `json:"target_samples"`
	TargetSource      string `json:"target_source"`
	TargetSatisfied   bool   `json:"target_satisfied"`
	AdditionalSamples int    `json:"additional_samples"`
}

func buildObserveSampleCheck(cfg *store.Config, expID, instName string, requestedSamples int, observations []*entity.Observation) (observeSampleCheck, error) {
	inst, ok := cfg.Instruments[instName]
	if !ok {
		return observeSampleCheck{}, fmt.Errorf("instrument %q is not registered in config.yaml", instName)
	}
	plan := instrument.PlanSamples(inst, requestedSamples)
	current := samplesForObservedInstrument(inst, observations, instName)
	check := observeSampleCheck{
		Experiment:        expID,
		Instrument:        instName,
		MinSamples:        inst.MinSamples,
		MinSatisfied:      inst.MinSamples == 0 || current >= inst.MinSamples,
		CurrentSamples:    current,
		TargetSamples:     plan.Target,
		TargetSource:      plan.Source,
		TargetSatisfied:   current >= plan.Target,
		AdditionalSamples: max(plan.Target-current, 0),
	}
	if requestedSamples > 0 {
		check.RequestedSamples = requestedSamples
	}
	return check, nil
}

func resolveObserveScope(exp *entity.Experiment, candidateRef string) (observeScope, error) {
	if exp == nil {
		return observeScope{}, fmt.Errorf("experiment is required")
	}
	if strings.TrimSpace(exp.Worktree) == "" {
		return observeScope{}, fmt.Errorf("experiment %s has no worktree; run `autoresearch experiment implement %s` first", exp.ID, exp.ID)
	}
	dirtyPaths, err := observeScopeDirtyPaths(exp.Worktree)
	if err != nil {
		return observeScope{}, fmt.Errorf("inspect candidate dirtiness for %s: %w", exp.ID, err)
	}

	headSHA, err := worktree.ResolveRef(exp.Worktree, "HEAD")
	if err != nil {
		return observeScope{}, fmt.Errorf("resolve candidate HEAD for %s: %w", exp.ID, err)
	}
	if exp.IsBaseline {
		if strings.TrimSpace(candidateRef) != "" {
			return observeScope{}, fmt.Errorf("--candidate-ref is only valid for non-baseline experiments")
		}
		if len(dirtyPaths) > 0 {
			return observeScope{}, fmt.Errorf(
				"baseline experiment %s worktree has uncommitted changes (%s); observe requires a clean checkout",
				exp.ID, formatObserveDirtyPaths(dirtyPaths),
			)
		}
		return observeScope{
			Attempt:      exp.Attempt,
			CandidateSHA: headSHA,
		}, nil
	}

	normRef, err := normalizeObserveCandidateRef(exp, candidateRef)
	if err != nil {
		return observeScope{}, err
	}
	if len(dirtyPaths) > 0 {
		return observeScope{}, fmt.Errorf(
			"experiment %s worktree has uncommitted changes (%s); observe requires a clean checkout that matches --candidate-ref %s",
			exp.ID, formatObserveDirtyPaths(dirtyPaths), normRef,
		)
	}
	refSHA, err := worktree.ResolveRef(exp.Worktree, normRef)
	if err != nil {
		return observeScope{}, fmt.Errorf("resolve candidate ref %q for %s: %w", normRef, exp.ID, err)
	}
	if headSHA != refSHA {
		return observeScope{}, fmt.Errorf(
			"experiment %s worktree HEAD %s does not match --candidate-ref %s (%s)",
			exp.ID, shortSHA(headSHA), normRef, shortSHA(refSHA),
		)
	}
	return observeScope{
		Attempt:      exp.Attempt,
		CandidateRef: normRef,
		CandidateSHA: headSHA,
	}, nil
}

func loadCurrentObservations(s *store.Store, exp *entity.Experiment, candidateRef string) (observeScope, []*entity.Observation, error) {
	scope, err := resolveObserveScope(exp, candidateRef)
	if err != nil {
		return observeScope{}, nil, err
	}
	all, err := s.ListObservationsForExperiment(exp.ID)
	if err != nil {
		return observeScope{}, nil, err
	}
	return scope, filterObservationsByScope(all, scope), nil
}

func observeScopeDirtyPaths(worktreeDir string) ([]string, error) {
	paths, err := worktree.DirtyPaths(worktreeDir)
	if err != nil {
		return nil, err
	}
	managed, err := autoresearchManagedCheckoutPaths()
	if err != nil {
		return nil, err
	}
	filtered := make([]string, 0, len(paths))
	for _, path := range paths {
		if path == entity.BriefFileName || isAutoresearchManagedCheckoutPath(path, managed) {
			continue
		}
		filtered = append(filtered, path)
	}
	return filtered, nil
}

func normalizeObserveCandidateRef(exp *entity.Experiment, candidateRef string) (string, error) {
	ref := strings.TrimSpace(candidateRef)
	if ref == "" {
		return "", fmt.Errorf(
			"experiment %s requires --candidate-ref <ref>; create a unique reviewable git ref for the measured candidate (for example `git -C %s branch <name> HEAD`) and rerun observe",
			exp.ID, exp.Worktree,
		)
	}
	sym, err := worktree.SymbolicFullName(exp.Worktree, ref)
	if err != nil {
		return "", fmt.Errorf("resolve candidate ref %q for %s: %w", ref, exp.ID, err)
	}
	if !strings.HasPrefix(sym, "refs/") {
		return "", fmt.Errorf(
			"candidate ref %q is not a named git ref; create a branch or tag for the measured candidate and rerun observe with --candidate-ref <ref>",
			ref,
		)
	}
	return sym, nil
}

func formatObserveDirtyPaths(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	if len(paths) <= 3 {
		return strings.Join(paths, ", ")
	}
	return fmt.Sprintf("%s (+%d more)", strings.Join(paths[:3], ", "), len(paths)-3)
}

func filterObservationsByScope(observations []*entity.Observation, scope observeScope) []*entity.Observation {
	out := make([]*entity.Observation, 0, len(observations))
	for _, o := range observations {
		if observationInScope(o, scope) {
			out = append(out, o)
		}
	}
	return out
}

func observationInScope(o *entity.Observation, scope observeScope) bool {
	if o == nil {
		return false
	}
	if o.Attempt != scope.Attempt {
		return false
	}
	if scope.CandidateRef != "" {
		if strings.TrimSpace(o.CandidateRef) != scope.CandidateRef {
			return false
		}
		if scope.CandidateSHA == "" || o.CandidateSHA == "" {
			return false
		}
		return o.CandidateSHA == scope.CandidateSHA
	}
	if strings.TrimSpace(o.CandidateRef) != "" {
		return false
	}
	if scope.CandidateSHA == "" || o.CandidateSHA == "" {
		return false
	}
	return o.CandidateSHA == scope.CandidateSHA
}

func samplesForObservedInstrument(inst store.Instrument, observations []*entity.Observation, instName string) int {
	total := 0
	for _, o := range observations {
		if o == nil || o.Instrument != instName {
			continue
		}
		if !observationCountsTowardTarget(inst, o) {
			continue
		}
		total += observationSampleCount(o)
	}
	return total
}

func observationCountsTowardTarget(inst store.Instrument, o *entity.Observation) bool {
	if o == nil {
		return false
	}
	if inst.Parser != "builtin:passfail" {
		return true
	}
	return o.Pass != nil && *o.Pass
}

func observationSampleCount(o *entity.Observation) int {
	if o == nil {
		return 0
	}
	if len(o.PerSample) > 0 {
		return len(o.PerSample)
	}
	if o.Samples > 0 {
		return o.Samples
	}
	return 1
}

func sumObservationSamples(observations []*entity.Observation) int {
	total := 0
	for _, o := range observations {
		total += observationSampleCount(o)
	}
	return total
}

func latestObservation(observations []*entity.Observation) *entity.Observation {
	if len(observations) == 0 {
		return nil
	}
	return observations[len(observations)-1]
}

func markExperimentMeasuredIfNeeded(s *store.Store, exp *entity.Experiment) error {
	if exp == nil || exp.Status != entity.ExpImplemented {
		return nil
	}
	exp.Status = entity.ExpMeasured
	if err := s.WriteExperiment(exp); err != nil {
		return fmt.Errorf("update experiment status: %w", err)
	}
	return nil
}

func collectObservationRuns(
	s *store.Store,
	cfg *store.Config,
	exp *entity.Experiment,
	scope observeScope,
	instName string,
	check observeSampleCheck,
	appendMode bool,
	author string,
) ([]*entity.Observation, error) {
	if appendMode {
		obs, err := runAndRecordObservation(s, cfg, exp, scope, instName, check.TargetSamples, author)
		if err != nil {
			return nil, err
		}
		return []*entity.Observation{obs}, nil
	}
	if check.AdditionalSamples <= 0 {
		return nil, fmt.Errorf("instrument %s has no additional samples to record", instName)
	}
	plan := instrument.PlanSamples(cfg.Instruments[instName], 0)
	if plan.MultiSample {
		obs, err := runAndRecordObservation(s, cfg, exp, scope, instName, check.AdditionalSamples, author)
		if err != nil {
			return nil, err
		}
		return []*entity.Observation{obs}, nil
	}

	out := make([]*entity.Observation, 0, check.AdditionalSamples)
	for i := 0; i < check.AdditionalSamples; i++ {
		obs, err := runAndRecordObservation(s, cfg, exp, scope, instName, 1, author)
		if err != nil {
			return out, err
		}
		out = append(out, obs)
	}
	return out, nil
}

func buildSkippedObservationResult(check observeSampleCheck) observationResult {
	return observationResult{
		Inst:           check.Instrument,
		Action:         observeActionSkipped,
		CurrentSamples: check.CurrentSamples,
		TargetSamples:  check.TargetSamples,
	}
}

func buildRecordedObservationResult(check observeSampleCheck, observations []*entity.Observation, currentSamples int) (observeExecution, error) {
	last := latestObservation(observations)
	if last == nil {
		return observeExecution{}, fmt.Errorf("instrument %s produced no observation", check.Instrument)
	}
	added := sumObservationSamples(observations)
	return observeExecution{
		Check:        check,
		Observations: observations,
		Latest:       last,
		Result: observationResult{
			ID:             last.ID,
			IDs:            observationIDs(observations),
			Inst:           check.Instrument,
			Value:          last.Value,
			Unit:           last.Unit,
			Action:         observeActionRecorded,
			Samples:        added,
			CurrentSamples: currentSamples,
			TargetSamples:  check.TargetSamples,
		},
	}, nil
}

func executeObservationRun(
	s *store.Store,
	cfg *store.Config,
	exp *entity.Experiment,
	scope observeScope,
	check observeSampleCheck,
	appendMode bool,
	author string,
) (observeExecution, error) {
	observations, err := collectObservationRuns(s, cfg, exp, scope, check.Instrument, check, appendMode, author)
	if err != nil {
		return observeExecution{}, err
	}
	currentSamples := check.CurrentSamples + samplesForObservedInstrument(cfg.Instruments[check.Instrument], observations, check.Instrument)
	return buildRecordedObservationResult(check, observations, currentSamples)
}

func describeObserveAction(exp *entity.Experiment, check observeSampleCheck, appendMode bool) (string, map[string]any) {
	samplesToRecord := check.TargetSamples
	actionText := fmt.Sprintf("run instrument %q against %s", check.Instrument, exp.Worktree)
	if appendMode {
		actionText = fmt.Sprintf("append another %q observation on %s", check.Instrument, exp.ID)
	} else if check.CurrentSamples > 0 {
		actionText = fmt.Sprintf("top up %q on %s by %d sample(s)", check.Instrument, exp.ID, check.AdditionalSamples)
		samplesToRecord = check.AdditionalSamples
	}
	return actionText, map[string]any{
		"instrument":        check.Instrument,
		"worktree":          exp.Worktree,
		"current_samples":   check.CurrentSamples,
		"target_samples":    check.TargetSamples,
		"samples_to_record": samplesToRecord,
		"append":            appendMode,
	}
}

func buildObserveResultSummary(results []observationResult, currentObservations, newObservations []*entity.Observation) observeResultSummary {
	summary := observeResultSummary{
		Action:      observeActionRecorded,
		CurrentIDs:  observationIDs(currentObservations),
		RecordedIDs: observationIDs(newObservations),
		ReusedIDs:   currentObservationReuseIDs(currentObservations, newObservations),
	}
	for _, r := range results {
		if r.skipped() {
			summary.SkippedCount++
			continue
		}
		summary.RecordedCount++
	}
	if summary.RecordedCount == 0 && summary.SkippedCount > 0 {
		summary.Action = observeActionSkipped
	}
	return summary
}

func currentObservationReuseIDs(currentObservations, newObservations []*entity.Observation) []string {
	recorded := make(map[string]struct{}, len(newObservations))
	for _, o := range newObservations {
		if o != nil {
			recorded[o.ID] = struct{}{}
		}
	}
	reused := make([]string, 0, len(currentObservations))
	for _, o := range currentObservations {
		if o == nil {
			continue
		}
		if _, ok := recorded[o.ID]; ok {
			continue
		}
		reused = append(reused, o.ID)
	}
	return reused
}

func recordedObservationPayload(exec observeExecution) map[string]any {
	return map[string]any{
		"status":        "ok",
		"action":        exec.Result.Action,
		"id":            exec.Result.ID,
		"ids":           exec.Result.IDs,
		"samples_added": exec.Result.Samples,
		"observation":   exec.Latest,
		"observations":  exec.Observations,
	}
}

func formatObserveSatisfiedText(check observeSampleCheck) string {
	return fmt.Sprintf("have %d samples; pass `--append` to add more, or `--samples N` with N>%d to top up",
		check.CurrentSamples, check.CurrentSamples)
}

func formatObserveCheckText(check observeSampleCheck) string {
	minStatus := "satisfied"
	if !check.MinSatisfied {
		minStatus = "not satisfied"
	}
	if check.MinSamples == 0 {
		minStatus = "not set"
	}
	line := fmt.Sprintf("%s on %s: have %d sample", check.Instrument, check.Experiment, check.CurrentSamples)
	if check.CurrentSamples != 1 {
		line += "s"
	}
	line += fmt.Sprintf("; target=%d (%s)", check.TargetSamples, check.TargetSource)
	if check.MinSamples > 0 {
		line += fmt.Sprintf("; min_samples=%d (%s)", check.MinSamples, minStatus)
	}
	if check.TargetSatisfied {
		line += "; target satisfied"
	} else {
		line += fmt.Sprintf("; need %d more", check.AdditionalSamples)
	}
	return line
}

// runAndRecordObservation runs a single instrument against an experiment's
// worktree, writes the observation and artifacts to the store, emits the
// observation.record event, and returns the observation entity.
//
// This is the shared core used by `observe`, `observe --all`, and
// `experiment baseline`. It does NOT check firewall gates (observation
// request validation, instrument dependencies, unchanged-worktree guard)
// — the caller is responsible for those.
func runAndRecordObservation(
	s *store.Store,
	cfg *store.Config,
	exp *entity.Experiment,
	scope observeScope,
	instName string,
	samples int,
	author string,
) (*entity.Observation, error) {
	inst := cfg.Instruments[instName]

	ctx := context.Background()
	result, err := instrument.Run(ctx, instrument.Config{
		ProjectDir:  globalProjectDir,
		WorktreeDir: exp.Worktree,
		Name:        instName,
		Instrument:  inst,
		Samples:     samples,
	})
	if err != nil {
		return nil, fmt.Errorf("instrument %s: %w", instName, err)
	}

	var obsArts []entity.Artifact
	for _, ac := range result.Artifacts {
		sha, rel, err := s.WriteArtifact(ac.Content, ac.Filename)
		if err != nil {
			return nil, fmt.Errorf("write artifact %q: %w", ac.Name, err)
		}
		obsArts = append(obsArts, entity.Artifact{
			Name:  ac.Name,
			SHA:   sha,
			Path:  rel,
			Bytes: int64(len(ac.Content)),
			Mime:  ac.Mime,
		})
	}

	id, err := s.AllocID(store.KindObservation)
	if err != nil {
		return nil, err
	}
	unit := result.Unit
	if unit == "" {
		unit = inst.Unit
	}
	obs := &entity.Observation{
		ID:               id,
		Experiment:       exp.ID,
		Instrument:       instName,
		MeasuredAt:       result.FinishedAt.UTC(),
		Value:            result.Value,
		Unit:             unit,
		Samples:          result.SamplesN,
		PerSample:        result.PerSample,
		CILow:            result.CILow,
		CIHigh:           result.CIHigh,
		CIMethod:         result.CIMethod,
		Pass:             result.Pass,
		Artifacts:        obsArts,
		EvidenceFailures: result.EvidenceFailures,
		Command:          result.Command,
		ExitCode:         result.ExitCode,
		Worktree:         exp.Worktree,
		Attempt:          scope.Attempt,
		CandidateRef:     scope.CandidateRef,
		CandidateSHA:     scope.CandidateSHA,
		BaselineSHA:      exp.Baseline.SHA,
		Author:           or(author, "agent:observer"),
		Aux:              result.Aux,
	}
	obs.Normalize()
	if err := s.WriteObservation(obs); err != nil {
		return nil, fmt.Errorf("write observation: %w", err)
	}

	artShas := make([]string, 0, len(obsArts))
	for _, a := range obsArts {
		artShas = append(artShas, a.SHA)
	}
	eventData := map[string]any{
		"experiment":    exp.ID,
		"instrument":    instName,
		"value":         result.Value,
		"unit":          unit,
		"samples":       result.SamplesN,
		"artifact_shas": artShas,
		"attempt":       scope.Attempt,
		"candidate_ref": scope.CandidateRef,
		"candidate_sha": scope.CandidateSHA,
	}
	if result.Pass != nil {
		eventData["pass"] = *result.Pass
	}
	if result.CILow != nil {
		eventData["ci_low"] = *result.CILow
	}
	if result.CIHigh != nil {
		eventData["ci_high"] = *result.CIHigh
	}
	if result.ExitCode != 0 {
		eventData["exit_code"] = result.ExitCode
	}
	if len(result.EvidenceFailures) > 0 {
		eventData["evidence_failures"] = summarizeEvidenceFailures(result.EvidenceFailures)
	}
	if err := emitEvent(s, "observation.record", or(author, "agent:observer"), id, eventData); err != nil {
		return nil, err
	}

	return obs, nil
}

func summarizeEvidenceFailures(failures []entity.EvidenceFailure) []map[string]any {
	out := make([]map[string]any, 0, len(failures))
	for _, f := range failures {
		rec := map[string]any{
			"name":      f.Name,
			"exit_code": f.ExitCode,
		}
		if snippet := truncate(strings.TrimSpace(f.Error), 200); snippet != "" {
			rec["error"] = snippet
		}
		out = append(out, rec)
	}
	return out
}

// observeAll runs all given instruments in dependency-safe order against an
// experiment. It iterates instruments, skipping those whose requires deps
// are not yet satisfied, and retries until all are done or no progress is
// made. Returns the list of observation results for display.
func observeAll(
	s *store.Store,
	cfg *store.Config,
	exp *entity.Experiment,
	scope observeScope,
	priorObs []*entity.Observation,
	instruments []string,
	samples int,
	appendMode bool,
	author string,
) (observeAllExecution, error) {
	var results []observationResult
	var newObs []*entity.Observation
	recordedAny := false

	strict := cfg.Mode == "" || cfg.Mode == "strict"
	for _, instName := range instruments {
		if err := firewall.CheckObservationRequest(instName, samples, exp, cfg, strict); err != nil {
			return observeAllExecution{}, err
		}
	}

	remaining := make([]string, len(instruments))
	copy(remaining, instruments)

	for len(remaining) > 0 {
		progress := false
		var deferred []string
		for _, instName := range remaining {
			check, err := buildObserveSampleCheck(cfg, exp.ID, instName, samples, priorObs)
			if err != nil {
				return observeAllExecution{}, err
			}
			if !appendMode && check.TargetSatisfied {
				results = append(results, buildSkippedObservationResult(check))
				progress = true
				continue
			}
			if err := firewall.CheckInstrumentDependencies(instName, cfg, priorObs); err != nil {
				deferred = append(deferred, instName)
				continue
			}

			exec, err := executeObservationRun(s, cfg, exp, scope, check, appendMode, author)
			if err != nil {
				return observeAllExecution{}, err
			}
			priorObs = append(priorObs, exec.Observations...)
			newObs = append(newObs, exec.Observations...)
			recordedAny = true
			results = append(results, exec.Result)
			progress = true
		}
		if !progress {
			return observeAllExecution{}, fmt.Errorf("stuck: instruments %v have unsatisfied dependencies", deferred)
		}
		remaining = deferred
	}

	// Bump experiment status to measured if this was the first observation.
	if recordedAny {
		if err := markExperimentMeasuredIfNeeded(s, exp); err != nil {
			return observeAllExecution{}, err
		}
	}

	return observeAllExecution{
		Results:             results,
		CurrentObservations: priorObs,
		NewObservations:     newObs,
	}, nil
}
