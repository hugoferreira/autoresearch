package instrument

import "github.com/bytter/autoresearch/internal/store"

const (
	SamplePlanRequested     = "requested"
	SamplePlanMinSamples    = "min_samples"
	SamplePlanParserDefault = "parser_default"
)

// SamplePlan describes the sample target a caller should aim for before
// deciding whether another observation run is needed.
type SamplePlan struct {
	Target      int
	Source      string
	MultiSample bool
}

// PlanSamples resolves the effective sample target for an instrument.
//
// Multi-sample parsers (timing, scalar) honor explicit requests, then
// instrument.MinSamples, then their parser defaults. One-shot parsers still
// honor explicit requests and MinSamples as a target across observations, but
// a single execution contributes only one sample.
func PlanSamples(inst store.Instrument, requested int) SamplePlan {
	plan := SamplePlan{
		Target:      1,
		Source:      SamplePlanParserDefault,
		MultiSample: parserSupportsMultipleSamples(inst.Parser),
	}
	switch {
	case requested > 0:
		plan.Target = requested
		plan.Source = SamplePlanRequested
	case inst.MinSamples > 0:
		plan.Target = inst.MinSamples
		plan.Source = SamplePlanMinSamples
	case plan.MultiSample:
		plan.Target = parserDefaultSamples(inst.Parser)
	}
	return plan
}

func parserSupportsMultipleSamples(parser string) bool {
	switch parser {
	case "builtin:timing", "builtin:scalar":
		return true
	default:
		return false
	}
}

func parserDefaultSamples(parser string) int {
	switch parser {
	case "builtin:timing":
		return 5
	case "builtin:scalar":
		return 3
	default:
		return 1
	}
}
