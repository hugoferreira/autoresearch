package cli

import (
	"github.com/bytter/autoresearch/internal/integration"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Claude settings JSON output", func() {
	It("reports added allow and deny permissions without an ambiguous added field", func() {
		allow := integration.AutoresearchAllowEntry
		deny := integration.ClaudeHarnessToolResultsDenyEntry

		payload := claudeSettingsResultToMap(integration.ClaudeSettingsResult{
			Path:             "/tmp/project/.claude/settings.json",
			Updated:          true,
			AddedPermissions: []string{allow, deny},
			AddedAllow:       []string{allow},
			AddedDeny:        []string{deny},
		})

		Expect(payload).NotTo(HaveKey("added"))
		Expect(payload).To(HaveKeyWithValue("added_permissions", []string{allow, deny}))
		Expect(payload).To(HaveKeyWithValue("added_allow", []string{allow}))
		Expect(payload).To(HaveKeyWithValue("added_deny", []string{deny}))
	})
})
