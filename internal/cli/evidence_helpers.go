package cli

import (
	"fmt"
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
)

func formatEvidenceFailure(f entity.EvidenceFailure) string {
	label := fmt.Sprintf("%s (exit %d)", f.Name, f.ExitCode)
	if detail := strings.TrimSpace(f.Error); detail != "" {
		return label + ": " + detail
	}
	return label
}

func buildConclusionShowJSON(s *store.Store, c *entity.Conclusion) conclusionShowJSON {
	out := conclusionShowJSON{Conclusion: c}
	joinConclusionObservationEvidence(&out, s, c.Observations)
	return out
}

func joinConclusionObservationEvidence(out *conclusionShowJSON, s *store.Store, ids []string) {
	capHint := len(ids)
	for _, id := range ids {
		obs, err := s.ReadObservation(id)
		if err != nil {
			ensureObservationReadIssues(out, capHint)[id] = err.Error()
			continue
		}
		ensureObservationArtifacts(out, capHint)[id] = cloneArtifacts(obs.Artifacts)
		if len(obs.EvidenceFailures) > 0 {
			ensureObservationEvidenceFailures(out, capHint)[id] = cloneEvidenceFailures(obs.EvidenceFailures)
		}
	}
}

func ensureObservationArtifacts(out *conclusionShowJSON, capHint int) map[string][]entity.Artifact {
	if out.ObservationArtifacts == nil {
		out.ObservationArtifacts = make(map[string][]entity.Artifact, capHint)
	}
	return out.ObservationArtifacts
}

func ensureObservationEvidenceFailures(out *conclusionShowJSON, capHint int) map[string][]entity.EvidenceFailure {
	if out.ObservationEvidenceFailures == nil {
		out.ObservationEvidenceFailures = make(map[string][]entity.EvidenceFailure, capHint)
	}
	return out.ObservationEvidenceFailures
}

func ensureObservationReadIssues(out *conclusionShowJSON, capHint int) map[string]string {
	if out.ObservationReadIssues == nil {
		out.ObservationReadIssues = make(map[string]string, capHint)
	}
	return out.ObservationReadIssues
}

func cloneArtifacts(in []entity.Artifact) []entity.Artifact {
	out := make([]entity.Artifact, len(in))
	copy(out, in)
	return out
}

func cloneEvidenceFailures(in []entity.EvidenceFailure) []entity.EvidenceFailure {
	out := make([]entity.EvidenceFailure, len(in))
	copy(out, in)
	return out
}
