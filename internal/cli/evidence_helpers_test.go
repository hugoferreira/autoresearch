package cli

import (
	"github.com/bytter/autoresearch/internal/entity"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	testEvidenceName          = "mechanism"
	testEvidenceSpawnTraceErr = `spawn "sh -c echo trace": exec: "sh": executable file not found in $PATH`
)

var _ = Describe("formatEvidenceFailure", func() {
	DescribeTable("formats exit codes and spawn errors",
		func(in entity.EvidenceFailure, want string) {
			Expect(formatEvidenceFailure(in)).To(Equal(want))
		},
		Entry("exit only",
			entity.EvidenceFailure{Name: testEvidenceName, ExitCode: 7},
			testEvidenceName+" (exit 7)",
		),
		Entry("spawn error omits meaningless zero exit",
			entity.EvidenceFailure{Name: testEvidenceName, Error: testEvidenceSpawnTraceErr},
			testEvidenceName+": "+testEvidenceSpawnTraceErr,
		),
		Entry("exit and error",
			entity.EvidenceFailure{Name: testEvidenceName, ExitCode: 7, Error: "trace command failed"},
			testEvidenceName+" (exit 7): trace command failed",
		),
	)
})
