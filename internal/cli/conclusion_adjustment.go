package cli

import (
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
)

const (
	conclusionReasonCriticDowngradePrefix = "critic downgrade: "
	conclusionReasonWithdrawalPrefix      = "withdrawal: "
)

const (
	conclusionAdjustmentNone = ""
	conclusionAdjustmentFire = "firewall_downgrade"
	conclusionAdjustmentCrit = "critic_downgrade"
	conclusionAdjustmentWith = "withdrawn"
)

func conclusionAdjustmentKind(c *entity.Conclusion) string {
	if c == nil || c.Strict.RequestedFrom == "" {
		return conclusionAdjustmentNone
	}
	switch {
	case conclusionReasonHasPrefix(c.Strict.Reasons, conclusionReasonWithdrawalPrefix):
		return conclusionAdjustmentWith
	case conclusionReasonHasPrefix(c.Strict.Reasons, conclusionReasonCriticDowngradePrefix):
		return conclusionAdjustmentCrit
	default:
		return conclusionAdjustmentFire
	}
}

func conclusionAdjustmentSummary(c *entity.Conclusion) string {
	if c == nil || c.Strict.RequestedFrom == "" {
		return ""
	}
	switch conclusionAdjustmentKind(c) {
	case conclusionAdjustmentWith:
		return "withdrawn from " + c.Strict.RequestedFrom
	default:
		return "downgraded from " + c.Strict.RequestedFrom
	}
}

func conclusionReasonHasPrefix(reasons []string, prefix string) bool {
	for _, r := range reasons {
		if strings.HasPrefix(r, prefix) {
			return true
		}
	}
	return false
}
