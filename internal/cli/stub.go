package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

func stub(name string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		out := cmd.OutOrStdout()
		if globalJSON {
			payload := map[string]any{
				"status":  "stub",
				"command": name,
				"message": "not yet implemented — M1 skeleton",
			}
			return json.NewEncoder(out).Encode(payload)
		}
		fmt.Fprintf(out, "stub: %s (not yet implemented — M1 skeleton)\n", name)
		return nil
	}
}
