package cli

import (
	"fmt"
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
)

func formatEvidenceFailure(f entity.EvidenceFailure) string {
	label := fmt.Sprintf("%s (exit %d)", f.Name, f.ExitCode)
	if detail := strings.TrimSpace(f.Error); detail != "" {
		return label + ": " + detail
	}
	return label
}
