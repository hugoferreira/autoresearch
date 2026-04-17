# DATAMODEL.md

Reference for the entities, state machines, and on-disk layout autoresearch
implements. Primary audience is maintainers hacking on `internal/entity/` or
`internal/store/`; secondary audience is users who want to understand what
lives under `.research/` and why.

> The authoritative source is `internal/entity/` and `internal/store/`. If
> this document disagrees with the code, the code wins — please file a PR to
> fix the doc.

---

## Overview

Autoresearch drives an optimization research loop (goal → hypothesis →
experiment → observe → analyze → conclude → lesson). The datamodel is the
durable substrate an otherwise-stateless agent steers through; the CLI is
the single writer and the firewall (`internal/firewall/`) enforces the
invariants on every mutation.

Six primary entities, one event log, one state file, one config file:

| Entity       | ID prefix  | Primary refs                                      | On-disk path                          | Format                    |
| ------------ | ---------- | ------------------------------------------------- | ------------------------------------- | ------------------------- |
| Goal         | `G-NNNN`   | —                                                 | `.research/goals/G-NNNN.md`           | YAML frontmatter + MD     |
| Hypothesis   | `H-NNNN`   | `GoalID`, `Parent` (H-), `InspiredBy[]` (L-)      | `.research/hypotheses/H-NNNN.md`      | YAML frontmatter + MD     |
| Experiment   | `E-NNNN`   | `Hypothesis` (H-), `Baseline.Experiment` (E-)     | `.research/experiments/E-NNNN.md`     | YAML frontmatter + MD     |
| Observation  | `O-NNNN`   | `Experiment` (E-), `Artifacts[].SHA`              | `.research/observations/O-NNNN.json`  | pure JSON                 |
| Conclusion   | `C-NNNN`   | `Hypothesis`, `CandidateExp`, `BaselineExp`, `IncrementalExp`, `Observations[]` | `.research/conclusions/C-NNNN.md` | YAML frontmatter + MD     |
| Lesson       | `L-NNNN`   | `Subjects[]` (H/E/C), `SupersedesID` (L-)         | `.research/lessons/L-NNNN.md`         | YAML frontmatter + MD     |

Supporting types: `Config` (`.research/config.yaml`), `State`
(`.research/state.json`), `Event` (`.research/events.jsonl` — append only),
`Artifact` (content-addressed under `.research/artifacts/`), `Brief`
(per-worktree JSON snapshot, written once by `experiment implement`).

---

## ID allocation

IDs are minted only by `store.AllocID(kind)` in `internal/store/ids.go`. The
store increments a per-kind counter in `State.Counters` atomically and
formats `<kind>-<4-digit-zero-padded>`.

| `EntityKind` | Prefix | Counter key |
| ------------ | ------ | ----------- |
| `KindGoal`        | `G` | `"G"` |
| `KindHypothesis`  | `H` | `"H"` |
| `KindExperiment`  | `E` | `"E"` |
| `KindObservation` | `O` | `"O"` |
| `KindConclusion`  | `C` | `"C"` |
| `KindLesson`      | `L` | `"L"` |

IDs are global, not goal-scoped. An H-0042 from a concluded goal keeps that
ID forever. Counters never decrement; killed/refuted/abandoned entities
still occupy their slot.

---

## Goal

Defines what "better" means for the current research session. One `active`
goal at a time; the rest are `concluded` or `abandoned`.

**Struct:** `internal/entity/goal.go` — `Goal`.

| Field | Type | YAML key | Notes |
| ----- | ---- | -------- | ----- |
| `SchemaVersion` | `int` | `schema_version` | Current version is `4` (`GoalSchemaVersion`). |
| `ID` | `string` | `id` | `G-NNNN`. |
| `Status` | `string` | `status` | `active` / `concluded` / `abandoned`. |
| `DerivedFrom` | `string` | `derived_from` | Parent goal ID for derived goals. |
| `Trigger` | `string` | `trigger` | Why this goal was created (free-form). |
| `CreatedAt` | `*time.Time` | `created_at` | |
| `ClosedAt` | `*time.Time` | `closed_at` | Set on `goal conclude` / `goal abandon`. |
| `ClosureReason` | `string` | `closure_reason` | |
| `Objective` | `Objective` | `objective` | The single metric being optimized. |
| `Completion` | `*Completion` | `completion` | Optional stopping condition. |
| `Constraints` | `[]Constraint` | `constraints` | At least one required. |
| `Rescuers` | `[]Rescuer` | `rescuers` | Optional secondary-objective clauses that can rescue a "supported" verdict when the primary is neutral. See [Rescuer verdict dynamics](#conclusion). |
| `NeutralBandFrac` | `float64` | `neutral_band_frac` | `\|delta_frac\|` on the primary within this band counts as "neutral" for rescue purposes. Required `> 0` when `Rescuers` is set; rejected otherwise. |
| `Body` | `string` | — (Markdown body) | Steering notes extracted via `Steering()` = `ExtractSection(body, "Steering")`. |

**Sub-structs**:

| `Objective` field | Type | Notes |
| --- | --- | --- |
| `Instrument` | `string` | Must be registered in `Config.Instruments`. |
| `Target` | `string` | Free-form target name. |
| `Direction` | `string` | `increase` or `decrease`. |

| `Completion` field | Type | Notes |
| --- | --- | --- |
| `Threshold` | `float64` | Must be `> 0`. |
| `OnThreshold` | `string` | `ask_human` / `stop` / `continue_until_stall` / `continue_until_budget_cap`. Defaults to `ask_human` when a threshold is set. |

| `Constraint` field | Type | Notes |
| --- | --- | --- |
| `Instrument` | `string` | Must be registered. |
| `Max` | `*float64` | At least one of `Max`, `Min`, `Require` is required per constraint. |
| `Min` | `*float64` | |
| `Require` | `string` | e.g. `pass` — pairs with `Instrument.Requires`. |

| `Rescuer` field | Type | Notes |
| --- | --- | --- |
| `Instrument` | `string` | Must be registered; must not equal `Objective.Instrument`. Implicitly in-goal for hypotheses (`CheckHypothesisInstrumentWithinGoal`). |
| `Direction` | `string` | `increase` or `decrease`. |
| `MinEffect` | `float64` | Required `>= 0`. `0` means the rescuer is directional — any clean-CI effect in `Direction` rescues; `> 0` adds a quantitative threshold (same strict check as a primary prediction). |

**Lifecycle** (status values in `internal/entity/goal.go:11-14`):

| From     | To          | Trigger                        |
| -------- | ----------- | ------------------------------ |
| —        | `active`    | `goal new` / `goal derive`     |
| `active` | `concluded` | `goal conclude --reason`       |
| `active` | `abandoned` | `goal abandon --reason`        |

Only one `active` goal exists at any time; the ID is pinned in
`State.CurrentGoalID`. `goal new` requires no active goal (firewall:
`RequireNoActiveGoal`).

---

## Hypothesis

A falsifiable claim about the target system. Every hypothesis is scoped to a
goal and names the instrument that will measure its predicted effect.

**Struct:** `internal/entity/hypothesis.go` — `Hypothesis`.

| Field | Type | YAML key | Notes |
| ----- | ---- | -------- | ----- |
| `ID` | `string` | `id` | `H-NNNN`. |
| `GoalID` | `string` | `goal_id` | Bound to one goal. |
| `Parent` | `string` | `parent` | Another H- for sub-hypotheses. Firewall requires a reviewed parent before deriving (`CheckParentReviewed`). |
| `Claim` | `string` | `claim` | One-sentence falsifiable claim. |
| `Predicts` | `Predicts` | `predicts` | What the hypothesis predicts. |
| `KillIf` | `[]string` | `kill_if` | At least one kill criterion required. |
| `InspiredBy` | `[]string` | `inspired_by` | Lesson IDs (`L-NNNN`). Firewall accepts only `active` lessons on `system` or `reviewed_decisive` chains. |
| `Status` | `string` | `status` | See state machine below. |
| `Priority` | `string` | `priority` | Set to `human` by `hypothesis promote`. Generator picks priority=human first. |
| `Author` | `string` | `author` | |
| `CreatedAt` | `time.Time` | `created_at` | |
| `Tags` | `[]string` | `tags` | |
| `Body` | `string` | — | Rationale under `# Rationale`. |

**Sub-struct `Predicts`**:

| Field | Type | Notes |
| --- | --- | --- |
| `Instrument` | `string` | Must be the goal's objective, one of its constraint instruments, or one of its rescuer instruments (`CheckHypothesisInstrumentWithinGoal`). |
| `Target` | `string` | |
| `Direction` | `string` | `increase` or `decrease`. |
| `MinEffect` | `float64` | Required `>= 0`. `> 0` is a quantitative prediction — strict mode enforces `\|delta_frac\| >= min_effect` at conclude time. `== 0` marks the hypothesis as **directional**: the CI-clean-side gate still runs, but the magnitude check is skipped. Use directional when no prior evidence grounds a specific threshold. |

**Lifecycle** (constants in `internal/entity/hypothesis.go:10-17`):

| From                      | To                 | Trigger                                           |
| ------------------------- | ------------------ | ------------------------------------------------- |
| —                         | `open`             | `hypothesis add`                                  |
| `open`                    | `unreviewed`       | `conclude` with decisive verdict (`supported`/`refuted`) |
| `open`                    | `inconclusive`     | `conclude inconclusive` (no gate review needed)   |
| `unreviewed`              | `supported` / `refuted` | `conclusion accept --reviewed-by`            |
| `supported` / `refuted`   | `inconclusive`     | `conclusion downgrade --reason` (critic)          |
| `inconclusive`            | `unreviewed`       | `conclusion appeal --reason`                      |
| `open`                    | `killed`           | `hypothesis kill --reason`                        |
| `killed`                  | `open`             | `hypothesis reopen --reason`                      |

`unreviewed` is the post-conclude / pre-review holding state and exists to
keep the two-agent firewall honest: the generator cannot derive a child
hypothesis from, nor can `hypothesis apply` ship, a conclusion that hasn't
been independently reviewed.

---

## Experiment

One attempt at moving the objective, rooted in a worktree branched off a
recorded baseline SHA. Baseline experiments are special: they measure the
unmodified code and serve as comparators for hypothesis-bound experiments.

**Struct:** `internal/entity/experiment.go` — `Experiment`.

| Field | Type | YAML key | Notes |
| ----- | ---- | -------- | ----- |
| `ID` | `string` | `id` | `E-NNNN`. |
| `Hypothesis` | `string` | `hypothesis` | H- id. Empty for baselines (`IsBaseline=true`). |
| `IsBaseline` | `bool` | `is_baseline` | Flag — baselines have no hypothesis. |
| `Status` | `string` | `status` | See state machine. |
| `Baseline` | `Baseline` | `baseline` | Git ref, SHA, and prior-experiment reference. |
| `Instruments` | `[]string` | `instruments` | Registered instrument names. |
| `Worktree` | `string` | `worktree` | Absolute path (set by `experiment implement`). |
| `Branch` | `string` | `branch` | `autoresearch/<exp-id>`. |
| `Budget` | `Budget` | `budget` | Wall time and sample caps. |
| `Author` | `string` | `author` | |
| `CreatedAt` | `time.Time` | `created_at` | |
| `ReferencedAsBaselineBy` | `[]string` | `referenced_as_baseline_by` | C- ids that used this experiment as a comparator. Written by `conclude`. |
| `Body` | `string` | — | Design and implementation notes. |

**Sub-structs**:

| `Baseline` field | Type | Notes |
| --- | --- | --- |
| `Ref` | `string` | Git ref (e.g. `HEAD`, a tag). |
| `SHA` | `string` | Full resolved SHA at `experiment design` time. |
| `Experiment` | `string` | Optional E- id of a prior experiment used as the baseline. |

| `Budget` field | Type | Notes |
| --- | --- | --- |
| `WallTimeS` | `int` | Per-experiment wall-time cap. |
| `MaxSamples` | `int` | Per-experiment sample cap. |

**Lifecycle** (constants in `internal/entity/experiment.go:10-16`):

| From         | To              | Trigger                                     |
| ------------ | --------------- | ------------------------------------------- |
| —            | `designed`      | `experiment design` / `experiment baseline` |
| `designed`   | `implemented`   | `experiment implement` (creates worktree)   |
| `implemented`| `measured`      | first `observe` call                        |
| `measured`   | `analyzed`      | `conclude` (when this is the candidate)     |
| any          | `designed`      | `experiment reset --reason` (rewinds)       |
| any          | `failed`        | set by the loop when implementation fails   |

`experiment reset` renames the abandoned branch to
`autoresearch/<exp-id>@<unix-ms>` before removing the worktree — nothing is
lost, the retry history is auditable. A baseline experiment follows the
same status progression but carries `IsBaseline=true`.

---

## Observation

A single measurement of one instrument on one experiment. Append-only;
observations are not mutated once written.

**Struct:** `internal/entity/observation.go` — `Observation`. **Pure JSON**
(no frontmatter, no Markdown body).

| Field | Type | JSON key | Notes |
| ----- | ---- | -------- | ----- |
| `ID` | `string` | `id` | `O-NNNN`. |
| `Experiment` | `string` | `experiment` | E- id. |
| `Instrument` | `string` | `instrument` | Must match a registered `Config.Instruments` key. |
| `MeasuredAt` | `time.Time` | `measured_at` | |
| `Value` | `float64` | `value` | Primary scalar. |
| `Unit` | `string` | `unit` | From the instrument definition. |
| `Samples` | `int` | `samples` | |
| `PerSample` | `[]float64` | `per_sample` | Per-run values; consumed by `analyze` for bootstrap CIs. |
| `CILow`, `CIHigh` | `*float64` | `ci_low`, `ci_high` | Optional per-observation CI (distinct from the Conclusion-level CI on the delta). |
| `CIMethod` | `string` | `ci_method` | e.g. `bootstrap-bca`. |
| `Pass` | `*bool` | `pass` | For boolean instruments (e.g. `host_test=pass`). |
| `Artifacts` | `[]Artifact` | `artifacts` | Content-addressed output files. |
| `RawArtifact`, `RawSHA` | `string` | `raw_artifact`, `raw_sha` | Legacy single-artifact fields; kept in sync by `Normalize()`. |
| `Command` | `string` | `command` | Command that produced the observation. |
| `ExitCode` | `int` | `exit_code` | |
| `Worktree` | `string` | `worktree` | Where the instrument ran. |
| `BaselineSHA` | `string` | `baseline_sha` | SHA the instrument saw in the worktree at measurement time. |
| `Author` | `string` | `author` | |
| `Aux` | `map[string]any` | `aux` | Instrument-specific extras (structured, not prose). |

**`Artifact`** (`internal/entity/observation.go:12-18`): `Name`, `SHA`,
`Path` (relative to `.research/`), `Bytes`, `Mime`. Artifacts live at
`.research/artifacts/AB/CDEF…/<filename>` (see [On-disk layout](#on-disk-layout)).

No lifecycle: observations do not have a status.

---

## Conclusion

A verdict on one hypothesis, backed by the candidate experiment, a baseline
experiment, and the statistical comparison of their observations.

**Struct:** `internal/entity/conclusion.go` — `Conclusion`.

| Field | Type | YAML key | Notes |
| ----- | ---- | -------- | ----- |
| `ID` | `string` | `id` | `C-NNNN`. |
| `Hypothesis` | `string` | `hypothesis` | H- id. |
| `Verdict` | `string` | `verdict` | `supported` / `refuted` / `inconclusive`. |
| `Observations` | `[]string` | `observations` | O- ids feeding the stat comparison. |
| `CandidateExp` | `string` | `candidate_experiment` | E- id. |
| `BaselineExp` | `string` | `baseline_experiment` | E- id (usually a baseline; may be a prior winner). |
| `Effect` | `Effect` | `effect` | Absolute baseline comparison. |
| `IncrementalExp` | `string` | `incremental_experiment` | Frontier-best experiment at conclude time. |
| `IncrementalEffect` | `*Effect` | `incremental_effect` | Delta vs. `IncrementalExp`. |
| `SecondaryChecks` | `[]ClauseCheck` | `secondary_checks` | Audit trail: one entry per goal rescuer consulted during conclude. Empty when rescue wasn't needed (primary passed) or when the goal has no rescuers. |
| `StatTest` | `string` | `stat_test` | e.g. `mann-whitney-u`. |
| `Strict` | `Strict` | `strict_check` | Firewall decision. |
| `Author` | `string` | `author` | |
| `ReviewedBy` | `string` | `reviewed_by` | Set by `conclusion accept --reviewed-by`. |
| `CreatedAt` | `time.Time` | `created_at` | |
| `Body` | `string` | — | Interpretation under `# Interpretation`. |

**Sub-structs**:

| `Effect` field | Type | Notes |
| --- | --- | --- |
| `Instrument` | `string` | Which metric the effect is on. |
| `DeltaAbs` / `DeltaFrac` | `float64` | Absolute and fractional deltas. |
| `CILowAbs` / `CIHighAbs` / `CILowFrac` / `CIHighFrac` | `float64` | Bootstrap CI bounds. |
| `PValue` | `float64` | Mann–Whitney U by default. |
| `CIMethod` | `string` | e.g. `bootstrap-bca`. |
| `NCandidate` / `NBaseline` | `int` | Sample counts feeding the comparison. |

| `Strict` field | Type | YAML key | Notes |
| --- | --- | --- | --- |
| `Passed` | `bool` | `passed` | Whether the firewall accepted the requested verdict. |
| `RequestedFrom` | `string` | `downgraded_from` | Original verdict before a strict downgrade. |
| `RescuedBy` | `string` | `rescued_by` | Instrument name of the goal rescuer whose clause check saved the verdict. Empty when the primary passed directly or when the downgrade wasn't rescued. |
| `Directional` | `bool` | `directional` | True when the hypothesis predicted with `MinEffect == 0` (direction-only). A rendering hint so a clean-CI but tiny effect is never mistaken for a quantitative win. |
| `Reasons` | `[]string` | `reasons` | Why the firewall downgraded or (on rescue) what the primary looked like before rescue fired. |

| `ClauseCheck` field | Type | YAML key | Notes |
| --- | --- | --- | --- |
| `Role` | `string` | `role` | `rescuer` today; reserved for future roles (e.g. `guardrail`). |
| `Instrument` | `string` | `instrument` | The rescuer's instrument. |
| `Direction` | `string` | `direction` | `increase` or `decrease` — copied from the goal's `Rescuer`. |
| `MinEffect` | `float64` | `min_effect` | Copied from the goal's `Rescuer`. `0` means a directional rescuer. |
| `Effect` | `*Effect` | `effect` | Computed against the same candidate/baseline pair as the primary check. `nil` when observations are missing. |
| `Passed` | `bool` | `passed` | Whether the rescuer's own strict check passed. |
| `Reasons` | `[]string` | `reasons` | Why the rescuer failed (CI crosses zero, below min_effect, no data, …). |

**Verdict dynamics**: the verdict is set by `conclude` based on the
statistical comparison. In strict mode (`Config.Mode == "strict"`), the
`CheckStrictVerdict` firewall in `internal/firewall/validators.go` can
forcibly downgrade `supported` → `inconclusive` if the CI on `delta_frac`
crosses zero in the wrong direction, if sample counts are insufficient, or
if `|delta_frac| < Hypothesis.Predicts.MinEffect`. A directional
hypothesis (`MinEffect == 0`) skips the magnitude gate; the CI-clean-side
check still applies. A critic can also call `conclusion downgrade` to flip
a decisive verdict to `inconclusive` with a reason. `conclusion appeal`
reverses a downgrade and moves the hypothesis back to `unreviewed`.

**Rescue path**: when the goal declares `Rescuers` and a positive
`NeutralBandFrac`, a failing `supported` check can be salvaged.
`CheckStrictVerdictWithContext` checks whether the primary's
`|delta_frac|` is within the band ("didn't lose"); if so, each rescuer
runs its own strict check on the same candidate/baseline pair using the
callback the conclude pipeline provides. The first rescuer to pass keeps
the verdict as `supported` with `strict.rescued_by` naming it and
`secondary_checks[]` auditing every rescuer considered. Rescue never
fires when the primary's `|delta_frac|` exceeds the band — rescue only
saves neutrals, not losses. Downstream (frontier, renderers) treat a
rescued conclusion as a first-class `supported`, but annotate it
distinctly (`supported (rescued by X)`) so readers never mistake it for
a clean primary win. The frontier's sort uses rescuers as a bounded
tiebreak when primary values are within `NeutralBandFrac`: a rescued
candidate whose rescuer wins displaces the prior best at the same
primary tier.

---

## Lesson

A distilled, supersedable claim the research loop has learned — above the
per-cycle artifacts, informing the next cycle.

**Struct:** `internal/entity/lesson.go` — `Lesson`.

| Field | Type | YAML key | Notes |
| ----- | ---- | -------- | ----- |
| `ID` | `string` | `id` | `L-NNNN`. |
| `Claim` | `string` | `claim` | One-line takeaway. |
| `Scope` | `string` | `scope` | `hypothesis` or `system`. |
| `Subjects` | `[]string` | `subjects` | H/E/C ids the lesson was extracted from. Required for `hypothesis` scope, forbidden for `system` scope. |
| `Tags` | `[]string` | `tags` | |
| `PredictedEffect` | `*PredictedEffect` | `predicted_effect` | Expected effect of future work in the same direction; only valid for `hypothesis` scope. |
| `Status` | `string` | `status` | See below. |
| `Provenance` | `*LessonProvenance` | `provenance` | `source_chain` classifies the evidence strength. |
| `SupersedesID` | `string` | `supersedes` | L- id this lesson replaces. |
| `SupersededByID` | `string` | `superseded_by` | Backref, set when another lesson supersedes this one. |
| `Author` | `string` | `author` | |
| `CreatedAt` | `time.Time` | `created_at` | |
| `Body` | `string` | — | Expected sections: Evidence, Mechanism, Scope, For-the-next-generator. |

**Sub-structs**:

| `PredictedEffect` field | Type | Notes |
| --- | --- | --- |
| `Instrument`, `Direction`, `MinEffect` | | Same shape as `Predicts`. |
| `MaxEffect` | `float64` | Optional upper bound; must be `>= MinEffect` when set. |

| `LessonProvenance` field | Value |
| --- | --- |
| `SourceChain` | `system` / `reviewed_decisive` / `unreviewed_decisive` / `inconclusive` |

**Status values** (constants in `internal/entity/lesson.go:55-67`):

| Status          | Meaning |
| --------------- | ------- |
| `active`        | Informs future iterations. |
| `provisional`   | Unverified; pending confirmation. |
| `invalidated`   | Contradicted by subsequent evidence. |
| `superseded`    | Replaced by a newer lesson (`SupersededByID`). |

`Lesson.EffectiveStatus()` returns `active` when the field is empty.

**Firewall rule**: `hypothesis add --inspired-by L-…` only accepts lessons
that are `active` AND whose source chain resolves to `system` or
`reviewed_decisive`. Lessons rooted in unreviewed decisive chains or
inconclusive chains are rejected at the CLI boundary. See
`firewall.CheckInspiredByLessonsReviewed` and `AssessLessonSourceChain`.

---

## Brief

Frozen read-only snapshot written once by `experiment implement` into the
experiment worktree root as `.autoresearch-brief.json`. Subagents inside a
worktree cannot reach back to the main `.research/` store (different tree
entirely), so they read this file for context.

**Struct:** `internal/entity/brief.go` — `Brief` (+ `BriefGoal`,
`BriefHypothesis`, `BriefExperiment`, `BriefLesson`). Pure JSON. See the
file for the field list — it mirrors the subset of the main entities an
implementer or observer needs.

The brief is never updated. If `experiment reset` rewinds an experiment,
the next `experiment implement` writes a fresh brief.

---

## Store-level types

### `State` — `.research/state.json`

**Struct:** `internal/store/state.go` — `State`. JSON-serialized.

| Field | Type | JSON key | Notes |
| ----- | ---- | -------- | ----- |
| `SchemaVersion` | `int` | `schema_version` | Current `StateSchemaVersion = 2`. |
| `CurrentGoalID` | `string` | `current_goal_id` | G- id of the active goal (empty when none). |
| `Paused` | `bool` | `paused` | See [Paused state](#paused-state). |
| `PauseReason` | `string` | `pause_reason` | |
| `PausedAt` | `*time.Time` | `paused_at` | |
| `ResearchStartedAt` | `*time.Time` | `research_started_at` | Set on `init`; used by `CheckBudgetForNewExperiment`. |
| `Counters` | `map[string]int` | `counters` | Per-kind ID counters (`G`, `H`, `E`, `O`, `C`, `L`). |
| `LastEventAt` | `*time.Time` | `last_event_at` | Updated by `AppendEvent`. |

The store is single-writer (CLI invocation). `UpdateState(fn)` reads,
mutates, writes atomically — no locking today.

### `Config` — `.research/config.yaml`

**Struct:** `internal/store/config.go` — `Config`. YAML-serialized. User-editable.

| Field | Type | YAML key | Notes |
| ----- | ---- | -------- | ----- |
| `SchemaVersion` | `int` | `schema_version` | Defaults to `1`. |
| `Build` | `CommandSpec` | `build` | Project build command. |
| `Test` | `CommandSpec` | `test` | Project test command. |
| `Worktrees` | `WorktreesConfig` | `worktrees` | Where experiment worktrees go. |
| `Instruments` | `map[string]Instrument` | `instruments` | Registered measurement tools. |
| `Budgets` | `Budgets` | `budgets` | Research-wide caps. |
| `Mode` | `string` | `mode` | `strict` by default. Controls firewall strictness at conclude time. |

| `CommandSpec` field | Type |
| --- | --- |
| `Command` | `string` |
| `WorkDir` | `string` |

| `WorktreesConfig` field | Type | Notes |
| --- | --- | --- |
| `Root` | `string` | Absolute path. Defaults to `<UserCacheDir>/autoresearch/<basename>-<8hex>/worktrees`. |

| `Instrument` field | Type | Notes |
| --- | --- | --- |
| `Cmd` | `[]string` | Command to run. |
| `Parser` | `string` | e.g. `builtin:scalar`, `builtin:perf`, `builtin:criterion`, `builtin:boolean`. |
| `Pattern` | `string` | Extraction regex for `builtin:scalar` (one capture group for a base-10 integer). Ignored by others. |
| `Unit` | `string` | |
| `MinSamples` | `int` | Enforced by `CheckObservationRequest` in strict mode. |
| `Requires` | `[]string` | `"instrument=condition"` pairs (v1 condition: `pass`). Enforced by `CheckInstrumentDependencies` at observe time. |

| `Budgets` field | Type | Notes |
| --- | --- | --- |
| `MaxExperiments` | `int` | Hard cap on new experiments (checked at design time only). |
| `MaxWallTimeH` | `int` | Hard cap from `ResearchStartedAt`. |
| `FrontierStallK` | `int` | Loop heuristic for "no progress in K experiments". |
| `StaleExperimentMinutes` | `int` | Warning threshold for in-flight experiments. |

### `Event` — `.research/events.jsonl`

**Struct:** `internal/store/events.go` — `Event`. One JSON object per line.

| Field | Type | JSON key | Notes |
| ----- | ---- | -------- | ----- |
| `Ts` | `time.Time` | `ts` | UTC; set by `AppendEvent` if zero. |
| `Kind` | `string` | `kind` | e.g. `hypothesis.add`, `experiment.implement`, `conclude`, `goal.migrated`. |
| `Actor` | `string` | `actor` | `system`, `human`, `agent:orchestrator`, `agent:observer`, etc. |
| `Subject` | `string` | `subject` | The entity ID the event is about. |
| `Data` | `json.RawMessage` | `data` | Kind-specific payload. |

Example line:

```json
{"ts":"2026-04-16T10:30:45Z","kind":"conclude","actor":"agent:orchestrator","subject":"H-0042","data":{"verdict":"supported","candidate":"E-0013","baseline":"E-0001"}}
```

Every mutating verb appends an event. `Events(limit)` returns the last
*limit* records; `log --follow` polls the file byte-offset style (no
fsnotify, see CLAUDE.md).

**What goes into `data`** is governed by the **Event payload rule** in
`CLAUDE.md`: log semantic transitions (include `from`/`to` when a
status changes), capture time-sensitive context and cross-references the
reader can't recover from the entity file, and don't dump whole structs
or high-volume data. Events are an audit log and an advisory cache
hint — they are not a replay stream.

---

## Paused state

`pause --reason <TEXT>` flips `State.Paused=true`, records the reason and
timestamp. `resume` clears them. Mutating verbs check the flag via
`openStoreLive()` and return `ErrPaused` → exit code `3` when paused;
read-only verbs (`status`, `log`, `tree`, `frontier`, `report`,
`dashboard`, `*-show`, `*-list`, `artifact *`, `conclusion show`) work
regardless. Orchestrators treat exit 3 as the signal to stop cleanly.

`Paused` is a property of `State`, not of any single entity — there is no
"paused experiment". The gate is at the CLI boundary.

---

## On-disk layout

```
.research/
├── config.yaml            (Config, YAML, user-editable)
├── state.json             (State, JSON)
├── events.jsonl           (Event log, append-only JSONL)
├── goals/
│   └── G-NNNN.md          (Goal, YAML frontmatter + MD)
├── hypotheses/
│   └── H-NNNN.md          (Hypothesis, YAML frontmatter + MD)
├── experiments/
│   └── E-NNNN.md          (Experiment, YAML frontmatter + MD)
├── observations/
│   └── O-NNNN.json        (Observation, pure JSON)
├── conclusions/
│   └── C-NNNN.md          (Conclusion, YAML frontmatter + MD)
├── lessons/
│   └── L-NNNN.md          (Lesson, YAML frontmatter + MD; dir is created lazily)
└── artifacts/
    └── ab/
        └── cdef…62hex/
            └── <filename>   (content-addressed by SHA-256)
```

Paths are defined as constants in `internal/store/store.go`. Artifacts are
sharded on the first two hex characters of the SHA-256 (`ab/`) and then
placed in a directory named by the remaining 62 characters; the final
filename is passed in by the writer (default `raw.out`). `ArtifactLocation`
resolves a full hash or an unambiguous prefix of ≥ 4 hex characters.

**Worktrees live outside `.research/`.** Default root is
`<UserCacheDir>/autoresearch/<project-basename>-<8-hex-of-path>/worktrees/`,
overridable in `config.yaml` under `worktrees.root`. This keeps duplicate
source trees out of the project's own grep/find results. Each worktree
contains a `.autoresearch-brief.json` at its root — the frozen `Brief`
snapshot written by `experiment implement`.

**The CLI is the only writer of `.research/`.** Subagents read freely but
mutate via `autoresearch <verb>`. Read-only agents may follow
`events.jsonl` to observe activity.

---

## Serialization conventions

- **YAML frontmatter + Markdown body** for Goal, Hypothesis, Experiment,
  Conclusion, Lesson. Split on `---` delimiters. Parsed via
  `entity.ParseFrontmatter`, written via `entity.WriteFrontmatter`
  (`internal/entity/markdown.go`). The body round-trips verbatim.
- **Sections inside the body** (`# Rationale`, `# Design notes`, `#
  Implementation notes`, `# Interpretation`, `# Steering`, …) are appended
  via `entity.AppendMarkdownSection` and read back with
  `entity.ExtractSection`. These are string conventions, not schema.
- **Pure JSON** for Observation (high-volume, agent-produced, no prose),
  `State`, and `Brief`.
- **Pure YAML** for Config (human-editable).
- **JSONL** for events (append-only, streamable).
- **All writes go through `store.atomicWrite`** (`internal/store/atomic.go`
  — temp file in the same directory, rename into place). No partial files
  if a write is interrupted.

---

## Cross-reference graph

Field-level edges maintained by the model. When you add a new field or
validator, check whether it introduces a new edge.

| Source        | Field                           | Target(s)                        |
| ------------- | ------------------------------- | -------------------------------- |
| Goal          | `Objective.Instrument`          | `Config.Instruments` (by name)   |
| Goal          | `Constraints[].Instrument`      | `Config.Instruments` (by name)   |
| Goal          | `Rescuers[].Instrument`         | `Config.Instruments` (by name)   |
| Goal          | `DerivedFrom`                   | Goal                             |
| Hypothesis    | `GoalID`                        | Goal                             |
| Hypothesis    | `Parent`                        | Hypothesis                       |
| Hypothesis    | `InspiredBy[]`                  | Lesson                           |
| Hypothesis    | `Predicts.Instrument`           | `Config.Instruments` (and must be in the goal) |
| Experiment    | `Hypothesis`                    | Hypothesis                       |
| Experiment    | `Baseline.Experiment`           | Experiment                       |
| Experiment    | `Instruments[]`                 | `Config.Instruments`             |
| Experiment    | `ReferencedAsBaselineBy[]`      | Conclusion                       |
| Observation   | `Experiment`                    | Experiment                       |
| Observation   | `Instrument`                    | `Config.Instruments`             |
| Observation   | `Artifacts[].SHA`               | `.research/artifacts/…`          |
| Conclusion    | `Hypothesis`                    | Hypothesis                       |
| Conclusion    | `Observations[]`                | Observation                      |
| Conclusion    | `CandidateExp` / `BaselineExp` / `IncrementalExp` | Experiment     |
| Conclusion    | `SecondaryChecks[].Instrument`  | `Config.Instruments`             |
| Conclusion    | `Strict.RescuedBy`              | `Goal.Rescuers[].Instrument`     |
| Lesson        | `Subjects[]`                    | Hypothesis / Experiment / Conclusion |
| Lesson        | `SupersedesID` / `SupersededByID` | Lesson                          |
| Lesson        | `PredictedEffect.Instrument`    | `Config.Instruments`             |
| State         | `CurrentGoalID`                 | Goal                             |

There are no foreign-key constraints at the store level. Validators in
`internal/firewall/validators.go` enforce the ones that matter at the CLI
boundary (goal instrument registered, hypothesis instrument within goal,
parent reviewed, inspired-by lessons active-and-reviewed, etc.).

---

## Schema versioning

- `State.SchemaVersion` — current **2**. `v1` was a single-goal store with
  `.research/goal.md`; `v2` is multi-goal with `.research/goals/G-NNNN.md`
  and `State.CurrentGoalID`.
- `Goal.SchemaVersion` — current **4** (`GoalSchemaVersion`). `v4` added
  `Rescuers` and `NeutralBandFrac` — pure additive. Legacy `v3` goals
  parse cleanly with empty rescuers and zero band; no forced rewrite,
  no migration event.
- `Config.SchemaVersion` — current **1** (implicit default).

One migration runs today: `internal/store/migrate.go:migrateV1ToV2`. It
triggers on `Open()` when `State.SchemaVersion < 2`, is idempotent (no-op
if `.research/goal.md` is absent), and:

1. Allocates `G-0001` for the legacy goal.
2. Writes `.research/goals/G-0001.md` with `status=active` and
   `created_at=mtime` of the old file.
3. Stamps `goal_id=G-0001` on every hypothesis missing one.
4. Bumps `State.SchemaVersion` and sets `State.CurrentGoalID`.
5. Removes the legacy `.research/goal.md`.
6. Appends a `goal.migrated` event.

Policy for future migrations: run once on `Open()`, idempotent, emit an
event on completion. `Create()` always writes at the current schema
version and skips migration.

---

## See also

- `README.md` — user-facing narrative and CLI tour.
- `CLAUDE.md` — conventions for hacking on autoresearch itself.
- `internal/firewall/validators.go` — the invariants enforced on mutation.
- `internal/store/` — canonical types and the single writer.
- `internal/entity/` — the entity types authoritatively defined.
