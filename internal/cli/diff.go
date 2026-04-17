package cli

import (
	"fmt"
	"os/exec"
	"strings"
)

// runDiff executes the diff binary on two files and returns the unified-diff
// output split into lines. An empty result (no differences) is returned as
// nil, not a single empty string. Callers layer on truncation or colorizing.
func runDiff(diffBin, absA, absB string, contextLines int) ([]string, error) {
	cmd := exec.Command(diffBin, fmt.Sprintf("-U%d", contextLines), absA, absB)
	out, _ := cmd.CombinedOutput()
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil, nil
	}
	return lines, nil
}
