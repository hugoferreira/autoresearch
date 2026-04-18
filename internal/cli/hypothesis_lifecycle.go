package cli

import "github.com/bytter/autoresearch/internal/entity"

func hypothesisStatusAllowsConclude(status string) bool {
	switch status {
	case entity.StatusOpen, entity.StatusInconclusive:
		return true
	default:
		return false
	}
}

func hypothesisStatusAllowsKill(status string) bool {
	switch status {
	case entity.StatusOpen, entity.StatusInconclusive:
		return true
	default:
		return false
	}
}
