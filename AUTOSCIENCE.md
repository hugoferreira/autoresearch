# Autoscience: Scaffolding Rigorous Science for Autonomous Agents

*Hugo Sereno Ferreira — Arm, Cambridge, UK*
*AgenticOS Blog Series · 2026*

> *"The first principle is that you must not fool yourself — and you are the easiest person to fool."* — Richard P. Feynman

Over the last months I've been building something that was, in hindsight, a research loop. An LLM proposed optimizations for hot inner loops for a Computer Algebra System (CAS) that I was implementing on a Cortex-M4[^1], applied them on a branch, timed the result, and wrote up a plausible story about why the numbers moved. It produced a lot of numbers. It produced a lot of stories as well. It was, to a first approximation, pure cargo cult though: every surface element of science was present, but not a lot of the underlying discipline. The model was eight percent of the way through what it would have called a research program, with sixteen concluded "wins" stacked on top of each other, when I realized the baseline had drifted twice and one of the "20% speedups" was measured against a stale binary due to a poor `Makefile` and Clang interaction. Everything passed the laugh test; not so much the scrutiny test.

In the previous essay[^unix] (*The Unix Prior*) I argued that agents already speak shell; that fifty years of Thompson-and-Ritchie-shaped training data gave us a composition substrate for free, and that our job was mostly to pave the path people were already walking on. This one is the sequel, applied to a different problem: not *how does an agent compose work*, but *how does an agent produce knowledge we can trust?*

The answer, it turns out, is the same kind of answer. There is already a substrate; its name is the *scientific method*. And models already know it too -- they can (probably) recite Popper in five languages -- but knowing it and *performing* it are different things. The harness that makes performance auditable is what this essay is about.

## The Next Frontier Isn't Code — It's Science

The last three years of agent progress lived inside a single problem: *given this ticket, produce a diff*. It's a narrow, mostly-solved problem now[^2]. Claude Code, Codex, Cursor — the frontier labs are racing on a benchmark where the task is bounded, the evaluator is a test suite, and the failure mode is "the change doesn't compile." This is fine, useful, and enormously valuable. It is also almost done.

The next thing is harder and less bounded. *Given this system, figure out whether X improves Y, and prove it.* There is no test suite. There is no ticket. The evaluator — the person who reads the conclusion — has to be able to audit the evidence back to first principles, because the consequence of a wrong "supported" claim is not a failed CI run but a bad architectural decision made weeks later by someone who trusted the result.

This is science. Not the capital-S, Nature-paper, decade-long kind. The lowercase kind — the kind practiced by any engineer trying to decide if the new allocator is actually faster, or any data scientist trying to decide if the new retrieval layer actually helps. The difference from feature delivery is small in shape and enormous in consequence: the answer is a *number with a confidence interval*, not a green check.

In *The Token Economy* I argued *reduce with programs, reason with models*. For science, the pattern flips: *reason with models, but commit to evidence with programs.* The model is allowed to speculate. It is not allowed to be the thing that converts speculation into evidence, because it is, with high reliability, the easiest person in the room to fool.

## A Taxonomy of Autonomy in Science

Before talking about any particular harness, it helps to have a shared frame. Borrowing shamelessly from SAE's self-driving levels, here is possible ladder:

* **L0: Assistant.** The human runs every experiment. The LLM summarizes papers, drafts text, explains code. No decisions delegated.
* **L1: Co-author.** The LLM proposes hypotheses. The human designs and runs every experiment. The LLM helps write up the result. This is most of "using ChatGPT for research" today.
* **L2: Driver.** The LLM proposes *and executes* experiments inside a sandbox. The human reviews every conclusion before it's accepted as knowledge. The human is still the epistemic authority.
* **L3: Autonomous under harness.** The LLM runs a closed loop — hypothesis, experiment, observation, conclusion, lesson — under structural constraints that force falsifiability, isolation, and review. The human reviews *gated decisive results* and steers by conversation, not by editing state. Much of the work happens while the human is asleep.
* **L4: Autonomous without harness.** The LLM sets its own goals, picks its own targets, decides what counts as evidence. We are not there. We probably shouldn't be there yet, for the same reason we don't let L4 cars decide their own destinations.

`autoresearch` — the CLI this essay is about — deliberately targets **L3**. The orchestrator subagent runs *one cycle* and yields.[^yield] The gate reviewer is dispatched independently, with no orchestrator context, and re-checks stats and mechanism from the raw artifacts.[^independence] The human steers through the main agent session, in natural language. The dashboard is strictly read-only — a window, not a steering wheel.

The L3/L4 boundary is, I think, where the interesting engineering lives. L4 is a research problem for the alignment community. L3 is something we can build today, and the question is how.

## The Shape of the Problem

Andrej Karpathy recently published a small repo, also called `autoresearch`[^karpathy], framed — with his usual deadpan — as a dispatch from the year the research labs become *"autonomous swarms of AI agents running across compute cluster megastructures in the skies."* Under the hyperbole, the idea is crisp:

> *"Give an AI agent a small but real LLM training setup and let it experiment autonomously overnight. It modifies the code, trains for 5 minutes, checks if the result improved, keeps or discards, and repeats. You wake up in the morning to a log of experiments and (hopefully) a better model. […] You're not touching any of the Python files like you normally would as a researcher. Instead, you are programming the program.md Markdown files that provide context to the AI agents and set up your autonomous research org."*

Three things in that paragraph are genuinely good, and worth saying out loud before any contrast. First, the framing *you are programming the program.md*: the research loop is configured in prose, not in code. That is exactly the right level of abstraction, and it is also — not coincidentally — where this essay's harness lives. Second, the loop shape — *modify, train, check, keep-or-discard, repeat* — is the correct shape. It is the same five-beat cycle I spend the rest of this essay defending. Third, the whole thing is a hundred lines of Python that you can read over coffee; it is a first draft, it knows it is a first draft, and the commit history does not apologize for what it isn't.

And yet: "checks if the result improved" is where the story ends in the minimal version, and where it has to start in any version that claims to produce *knowledge* rather than artifacts. *Improved* compared to what baseline? Measured with how many samples? With what confidence interval? Surviving what independent review? In Karpathy's repo, the answer is "whatever the agent decides," which is fine for a demo and fatal for anything downstream of the demo. Programming the research org in `program.md` is a beautiful idea, and an unchecked one; `program.md` is also where an overconfident agent will quietly relax the definition of "improved" until everything is an improvement.

The question then is the obvious follow-up to Karpathy's setup: *what would you have to add to a five-minute-overnight loop to earn the word "science"?* Not in the performative sense — in the much smaller sense that the log you wake up to should survive an honest second reading. To avoid the name collision, and because the concept is broader than any one implementation, I'll call the *idea* **Autoscience** from here on. The codebase keeps its name.

## What "Good Science" Requires of an Autonomous Loop

If you strip the institutional scaffolding (peer review, grants, journals) and ask what's actually *load-bearing* about science as an epistemic practice, you get a short list:

1. **Falsifiability.** A claim worth making is a claim that could, in principle, lose. "Unrolling the loop will make it faster" is a hypothesis. "Unrolling the loop is interesting" is not.
2. **Isolation.** Every attempt runs against a clean, known baseline. Otherwise you're measuring the interaction of your change with someone else's change, and calling the sum your result.
3. **Measurement, not narration.** Evidence is a number from an instrument, not a paragraph describing what probably happened.
4. **Dual baselines.** "Better than the original" and "better than the current best" are *different questions* and both matter. Collapsing them is how you get the thirtieth 5%-speedup that is, cumulatively, a 2% regression.
5. **Statistical honesty.** Point estimates lie. Confidence intervals occasionally tell the truth. If the CI on your effect crosses zero, your effect is not *small* — it is *not established*.
6. **Independent review.** The person who judges cannot be the person who authored. This is the oldest trick in epistemics and the one agent systems most reliably forget.
7. **Audit trail.** Every decision reconstructible from durable state, after the fact, by someone who wasn't there. Without this, you don't have science; you have vibes.
8. **Reusable lessons.** A loop that doesn't learn from its own failures is a random walk.

None of these require an LLM. They would be requirements for a human-run optimization project, a biology lab, or a policy A/B test. The question is what they look like when the *actor* is an autonomous agent. It turns out the answer is: *exactly the same*, but the scaffold has to be machine-readable instead of living in the senior scientist's head.

## Enter Autoscience

`autoresearch` (the implementation) is built around six nouns. The nouns are the grammar; each one exists to force a discipline the model would otherwise skip:

| Concept       | Forces                                  |
| ---           | ---                                     |
| **Goal**      | The optimization contract is explicit: objective instrument + direction, constraints, optional threshold. |
| **Hypothesis**| A claim is falsifiable before code is touched: it predicts an instrument, a direction, and (optionally) a minimum effect size. |
| **Experiment**| Every attempt runs in its own `git worktree` from the recorded baseline. What we changed is separate from what we learned. |
| **Observation**| A measurement is bound to a specific instrument, command, candidate ref, samples, and content-addressed artifact. |
| **Conclusion**| A verdict is derived from observations and gated by a strict firewall. "Supported" is automatically downgraded to "inconclusive" if the CI crosses zero in the wrong direction or the observed effect misses `min_effect`.[^strict] |
| **Lesson**    | A reusable takeaway, provenance-tracked so only reviewed-decisive lessons can inspire future hypotheses. Failure compounds. |

The README puts it crisply: `autoresearch` *deliberately separates intent, change, evidence, and judgment.*[^sep] Those four words are the whole thesis of this section. An LLM will happily collapse them — propose a change, "observe" the result by narrating what it expects, and judge its own work in the same breath. The harness refuses each collapse, one noun at a time.

There is a seventh primitive — the **Frontier** — which is not an entity but a *derived view*: the best supported conclusions for the current goal, annotated with current loop actionability (a "dead" row can still represent a historically important win). It is what tells the orchestrator *we are no longer improving* and what signals the goal can stop. Without it, autonomous loops don't know how to finish.

## The Loop, End to End

Here is one cycle, with each arrow annotated by which requirement from §4 it enforces:

```
Goal                                                  (§4.1 falsifiability scoping)
  ↓  one active at a time; objective + constraints explicit
Hypothesis                                            (§4.1 falsifiability)
  ↓  declares predicts.instrument, direction, min_effect
Experiment (in its own git worktree)                  (§4.2 isolation)
  ↓  branches from the recorded baseline SHA
Implement (delegated to a coder sub-subagent)         (§4.2 isolation preserved)
  ↓  pure coder; reads .autoresearch-brief.json; returns commit SHA
Observe, per instrument, in dependency order          (§4.3 measurement)
  ↓  e.g. host_test must pass before host_timing runs
Analyze                                               (§4.4 dual baselines, §4.5 stats)
  ↓  percentile bootstrap on fractional delta, Mann–Whitney U
Conclude                                              (§4.5 statistical honesty)
  ↓  strict firewall may downgrade supported → inconclusive
Lesson                                                (§4.8 reusable lessons)
  ↓  provenance-tracked; system / reviewed-decisive only are citable
Gate Reviewer (independent, no orchestrator context)  (§4.6 independent review)
  ↓  recomputes stats from raw artifacts; accepts or downgrades
Frontier updates; stalled_for increments              (§4.7 audit trail)
  ↓  dashboard reflects; no new event needed for derived state
Next cycle (or goal completes, or budget exhausted)
```

Two things about this loop are worth staring at. First, the downgrade path is *automatic*, not negotiable. The `conclude` verb, in strict mode, will refuse to leave a verdict of "supported" in place if the CI on `delta_frac` crosses zero in the wrong direction, or if `|delta_frac| < hypothesis.min_effect`. There is no flag to disable this. There is a rescuer path — a goal may declare that a primary-objective neutral result can be rescued by a secondary instrument's strict win — but rescue fires only when the primary *didn't lose*.[^rescue] The model can beg; the harness does not care.

Second, the orchestrator explicitly *does not* dispatch the gate reviewer. When a cycle produces a decisive verdict (supported or refuted), the orchestrator yields to the main session. The main session — or the human, via the main agent — is what invites the reviewer in. This is the smallest possible structural defense against critic-author collusion, and it is surprisingly effective.

## Design: Everything Is a File, and the CLI Is the Only Writer

Two architectural invariants do all the load-bearing:

### Research is plain files

Markdown with YAML frontmatter for entities, JSONL for the event log, content-addressed blobs for artifacts. Nothing proprietary, nothing binary, nothing you need a tool to read.

```
.research/
  config.yaml          # build/test cmds, instruments, budgets
  state.json           # pause flag, counters, current_goal_id
  events.jsonl         # append-only semantic-transition log
  goals/G-NNNN.md      # objective + constraints + steering
  hypotheses/H-NNNN.md
  experiments/E-NNNN.md
  observations/O-NNNN.md
  conclusions/C-NNNN.md
  lessons/L-NNNN.md
  artifacts/<sha256>/…
```

Anything a human reads, `grep` reads. Anything `grep` reads, an agent reads. There is no reader/writer asymmetry to exploit, no schema migration to coordinate, no binary format to version. Git diffs of `.research/` are legible to code review even though the directory itself is gitignored by default (agents should not commit their research state into the target project's history).

### The CLI is the only writer

Every mutation goes through `autoresearch <verb>`. Subagents never edit `.research/` directly — not because we don't trust them with Edit tools, but because *the single-writer invariant is what makes everything else work*. Atomic rename on every write. Append-only event log with semantic `from → to` transitions. A pause gate that mutating verbs honor and read verbs ignore. A budget gate that refuses new experiments when `max_experiments` is exhausted. A firewall that runs *before* state is touched, not after. All of these are enforceable because there is exactly one process that mutates files, and everyone else talks to it.

This may sound like bureaucracy, but it is the opposite. A subagent with Edit access to `.research/` has to be taught not to take shortcuts; a subagent with only `Bash(autoresearch ...)` access *cannot* take shortcuts. The discipline is load-bearing, not decorative (*cf.* The Map Is the Vulnerability[^4]).

## The Unix Prior, Revisited for Science

In *The Unix Prior* I argued that Unix is a composition substrate that LLMs absorbed the way a musician absorbs scales — by sheer corpus exposure — and that this is why building AgenticOS on top of filesystem and shell utilities is, absurd as it sounds, the cheapest interface in tokens per unit of agent competence. *Don't teach models your API. Use the one they already know.* Autoscience is what happens when you apply the same bet to scientific workflow:

- The agent does not need a science-DSL schema preloaded into its system prompt. It needs a CLI with discoverable verbs, `--help`, and `--json`. The model already knows how to read `autoresearch hypothesis add --claim "..." --predicts-instrument host_timing --predicts-direction decrease --predicts-min-effect 0.05`. This is shell. It has seen a billion examples.
- `.research/` is a filesystem tree with markdown entities and JSONL events. The model already knows `grep`, `jq`, `cat`, `tail -f`. When it wants to check whether an observation exists for an experiment, it doesn't call a bespoke tool; it runs `ls .research/observations | head`.
- Experiment isolation is literal `git worktree`. The model already knows Git at a fluency most of us would pay to have. We didn't invent an isolation primitive; we used the one that ships with the operating system.
- The orchestrator/reviewer split is two subagents with textual prompts in `.claude/agents/`. No orchestration DSL, no state machine library, no graph framework. The two prompts are under 800 lines combined.

Contrast this with a hypothetical "ScienceMCP" server exposing typed tools — `create_hypothesis`, `record_observation`, `compute_confidence_interval`. Every subagent session would pay thousands of tokens of schema description before any work started. Every version bump would invalidate the in-context examples. Every edge case would grow the spec. We would be paying the substrate tax on every invocation, forever.

What the agent sees when it opens Claude Code on a project with `autoresearch` installed is, instead, (1) a markdown file (`.claude/autoresearch.md`) describing the CLI in prose, (2) two subagent prompts (`research-orchestrator.md`, `research-gate-reviewer.md`) describing the loop in prose, and (3) a CLI binary on `$PATH`. That is the entire interface. It is also the entire training prior — the model already knows how to read CLI help, already knows how to parse `--json` output with `jq`, already knows how to check exit codes. We are not teaching it anything new about *shape*. We are only specifying *vocabulary*.

The deeper move, the one I want to land, is that *the scientific method is itself a substrate with a prior*. Models have read enough papers, enough textbooks, enough philosophy-of-science essays, that the shape of "form a falsifiable hypothesis, isolate the variable, measure with a calibrated instrument, submit to independent review" is already compressed into their weights. They know how to do science in the same way they know how to write bash — by absorption, not instruction. What they lack is *discipline under pressure*: the tendency, when it would be convenient, to cut the corner. The harness is what restores the discipline: it is the venerable reviewer number two.

## Failure Modes the Harness Neutralizes

Each row is a failure I have personally watched an LLM loop commit as the development of `autoresearch` unfolded, with the specific countermeasure found in this codebase:

| Failure | How the naive loop falls into it | What this design does about it |
| --- | --- | --- |
| **Confabulated results** | Agent "observes" by narrating the numbers it expected. | Only the `observe` verb writes observations; each one records the exact command, per-sample data, and a content-addressed artifact. No observation exists without an artifact behind it. |
| **Goodharting** | A "20% speedup" turns out to have deleted the test. | Instrument dependency gate: timing cannot run until `host_test=pass`. Gate reviewer has an explicit goodharting checklist — "did this optimize the metric without genuinely improving the system?" |
| **p-hacking / cherry-picking** | Agent reruns until a favourable sample appears. | `observe` is idempotent on `(experiment, candidate-ref)`; samples are reused or topped up, not regenerated. Seeded PRNG makes the bootstrap CI reproducible across runs. Strict gate downgrades "supported" when the CI wanders. |
| **Moving baselines** | Agent compares to whatever's convenient this week. | Baseline is a first-class experiment, pinned to a SHA. Every conclusion carries both *absolute* (vs. goal baseline) and *incremental* (vs. frontier best) deltas. `--baseline-experiment` overrides are logged. |
| **Scope drift** | Agent "improves" a tangential metric and calls it a win. | A hypothesis's predicted instrument must equal the goal objective or one of the declared constraints. The firewall refuses the hypothesis otherwise. |
| **Critic-author collusion** | The context that wrote the conclusion also judges it. | Orchestrator yields on decisive; the gate reviewer is dispatched with no orchestrator context and recomputes the statistics from raw artifacts. Acceptance is a separate verb issued by a separate actor. |
| **State races** | Two subagents clobber each other's edits. | Single-writer CLI + atomic rename + append-only event log. Subagents don't have Edit access to `.research/`; they have `Bash(autoresearch *)` and nothing else. |
| **Untracked regressions** | Yesterday's win silently breaks today's test. | Every conclusion pins the exact `candidate-ref` and cites its observations by ID. Artifacts are content-addressed by SHA-256; if the evidence changes, the reference breaks loudly. |
| **Mechanism hand-waving** | "Unrolling made it faster" with no supporting evidence. | The gate reviewer must confirm every mechanism claim is either directly visible in the diff or present in a cited evidence artifact. Downgrade on failure to substantiate. |
| **Loops that won't stop** | Agent keeps grinding a dead direction. | Frontier tracks `stalled_for`; budget gate refuses new experiments past a hard cap; the rescuer path refuses to rescue outright losses. Stalling is a first-class signal, not a vibe. |

None of these countermeasures is an AI-alignment innovation. They are all *boring* engineering: sentinel files, atomic writes, dependency graphs, seeded PRNGs, the distinction between a CI and a point estimate. The reason they work is that the LLM cannot route around them without *explicitly* asking for or trying to write directly to disk: both of those actions leave fingerprints that can be checked in the audit log.

## Lessons, Generalization, and Next Frontiers

The specific verbs in `autoresearch` are code-shaped, but the scaffold is substrate-agnostic. The five primitives — *goal, hypothesis, experiment (isolated), observation (instrumented), conclusion (gated)* — plus the meta-rule *single-writer CLI over plain-file state* generalize anywhere three conditions hold:

1. "Better" can be named as a number.
2. Attempts can be isolated from each other.
3. A human can specify, up front, what would count as success.

**Wet-lab biology.** An experiment worktree becomes a plate or a run; instruments become assays; conclusions are still gated by CI; the gate reviewer is a second technician who hasn't seen the primary's notes. Hard part: baselines drift with reagent lots, so `Baseline` needs a freshness policy — probably an event that invalidates cached baseline observations when the lot changes. The meta-rule still works; a single experiment-management CLI writes the lab notebook, and technicians talk to it rather than editing the spreadsheet directly.

**Materials / catalysis.** Same shape as biology, but budget becomes much more aggressive — each run is expensive enough that `max_experiments` is the dominant constraint. The frontier-stall signal becomes the primary stopping rule, not an advisory.

**Applied ML research.** The original Karpathy's fit. The loss function *is* the instrumentation layer; worktrees become branches; the firewall is the missing piece in most ML-RnD shops I've seen, and it would mostly translate one-to-one. The seed-fishing failure mode in §9 is specifically an ML failure mode, and the seeded-PRNG-plus-idempotent-observe pattern neutralizes it.

**Policy / product A/B experimentation.** Harder. Instruments are noisy, baselines move (the user population drifts), and the human review gate has to be more aggressive because the feedback loop is too slow for frontier-stall to matter. The scaffold still helps; the firewall's automatic downgrade on wrong-direction CIs is exactly the defense against the "positive result from a too-small sample" pattern that dominates product experimentation write-ups.

**Honest limits.** The scaffold breaks when "better" can't be named as a number — which is most creative work, and a great deal of research in fields where the contribution is conceptual rather than empirical. It also breaks when isolation is genuinely impossible (online systems with deep feedback loops). And it adds overhead that's not worth paying when the experiment feedback loop is faster than the scaffold — if you can run a trial in two seconds and eyeball the answer, the discipline costs more than it buys.

What I am more interested in than any one port is the pattern: *a harness is something you design so the cheapest path for the agent is the scientifically honest path.* The agent isn't being forced to behave. It is being given an environment where the forbidden moves are *harder than the permitted moves*, because the permitted moves are expressed in a language (shell, files, Git) it already fluently speaks, and the forbidden moves require fighting the substrate. That is a much more robust defense than a prompt saying "please be rigorous." I have shipped both. Only one of them sleeps through the night.

## Epilogue

There is a moment, somewhere around the second week of running `autoresearch` on a real project, when the loop produces a beautifully-framed, well-measured, mechanism-supported hypothesis — and the strict firewall downgrades it from "supported" to "inconclusive" because the CI on the fractional delta clips zero by three basis points. And the agent, unable to route around it, writes a lesson saying *the direction is probably right, but the effect is below the threshold we set; next cycle should widen `min_effect` or tighten the baseline*. That lesson then inspires the next hypothesis, which opens in a clean worktree, and the loop keeps moving.

That is the moment the scaffold earns its keep. Not the moments it catches fraud — those are rare and dramatic. The moments it catches *honest overconfidence*. The agent was not lying. The agent was doing what every human researcher does when the numbers are close: squinting in a direction that favors the story. The harness does not squint. It does not care about the story. It cares about the CI.

In the *Unix Prior* essay I closed on the observation that the training data chose a winner for system composition, and it would be strange to bet against it. The scientific method is the same kind of winner — not because it's elegant (it isn't, particularly), but because it's the only thing we've found that survives contact with a clever author trying to fool themselves. Autonomous agents are, among other things, extraordinarily clever authors. They inherit more rigor when we make them walk the path that already exists: hypothesis, controlled comparison, independent review, CI, lesson, next.

We didn't plan for models to learn shell. They just did. We didn't plan for them to know the shape of science. They just do. Our job is to pave the path — to build the boring scaffolding, atomic writes and content-addressed artifacts and firewall gates and independent-reviewer splits, that lets the fluency express itself as discipline rather than as confident nonsense. And then get out of the way.

The twelve-step MCP pipeline became a three-command shell pipeline because the agent already knew how.[^unix-close] The sixteen confident "wins" became three supported conclusions and thirteen honest inconclusives because the firewall already knew how. In both cases, we didn't teach the model anything new. We just stopped pretending we had to.

---

[^1]: Because I have weird hobbies...

[^2]: Lot's of reasonable people would disagree with me. Personally, having spent the last 37 years programming in all sorts of languages, I made peace with the fact and moved on.

[^4]: Ferreira, H. S., *The Map Is the Vulnerability: Why Your Agent's Permission System Leaks Topology*, AgenticOS Blog Series, 2026.

[^unix]: Ferreira, H. S., *The Unix Prior: Why Agents Already Speak Shell*, AgenticOS Blog Series, 2026. Read this first if you haven't; much of §7–8 of this essay depends on its "don't teach models your API; use the one they already know" framing.

[^yield]: From the installed subagent contract: *"After concluding with `supported` or `refuted`, the hypothesis enters `unreviewed` status. Do not start another cycle. Yield to the main session with an explicit handoff. Do **not** dispatch `research-gate-reviewer` yourself."* (`internal/integration/agents/research-orchestrator.md`). The orchestrator is non-recursive by construction; one cycle per invocation.

[^independence]: *"You have no context from the orchestrator — you build your assessment from the raw data. That independence is the whole point of your role."* (`internal/integration/agents/research-gate-reviewer.md`). The reviewer recomputes the statistics from the stored artifacts before deciding, and has an explicit duty to downgrade when a mechanism claim is supported by neither the diff nor a cited artifact.

[^karpathy]: Karpathy, A., *autoresearch*, `github.com/karpathy/autoresearch`, March 2026. Quoted blocks are from the project README. I mean this genuinely: the repo is a good minimal demonstration of a think-try-observe-think loop, and you can learn more from reading its hundred lines than from reading this essay. The name collision is accidental and unfortunate on both sides.

[^strict]: `CheckStrictVerdict` in `internal/firewall/validators.go`. Two conditions: the 95% bootstrap CI must lie entirely on the predicted side of zero, and `|delta_frac|` must be at least `hypothesis.min_effect`. Directional hypotheses (`min_effect=0`) skip only the magnitude check. The downgrade reason is recorded in `Conclusion.strict` for the audit trail.

[^sep]: *"`autoresearch` deliberately separates intent, change, evidence, and judgment."* — project `README.md`, §Core concepts. If one sentence captures the thesis of the implementation, it is that one.

[^rescue]: Goals may declare `rescuers[]` and a `neutral_band_frac`. When the primary strict check fails and `|delta_frac|` is within the neutral band — i.e. the primary *didn't lose* — each rescuer runs its own strict check on the same candidate/baseline pair. The first to pass rescues the verdict. Rescue never fires on a regression. See `CheckStrictVerdictWithContext` in `internal/firewall/`.

[^unix-close]: *"That twelve-step MCP orchestration I was debugging last month? I rewrote it as a three-command pipeline. It took two minutes: the agent already knew how."* — *The Unix Prior*, §6.
