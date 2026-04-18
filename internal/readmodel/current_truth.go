package readmodel

import "github.com/bytter/autoresearch/internal/entity"

// SupportedConclusionCountsForReadSurface reports whether a historical
// supported conclusion should still be used as the default supported winner on
// read surfaces. This includes current accepted/pending truth and a legacy
// killed-state back-compat exception.
func SupportedConclusionCountsForReadSurface(status string) bool {
	switch status {
	case "", entity.StatusSupported, entity.StatusUnreviewed:
		return true
	case entity.StatusKilled:
		// Back-compat: older stores may still contain supported->killed
		// histories. Preserve those accepted wins on read-only historical
		// surfaces even though new lifecycle guards stop producing them.
		return true
	default:
		return false
	}
}
