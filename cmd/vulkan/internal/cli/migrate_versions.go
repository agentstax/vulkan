package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func newMigrateVersionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "versions",
		Short: "List the schema versions this binary knows how to reach",
		Long: "List every schema version compiled into THIS binary, per scope. The step\n" +
			"registry is the source of truth here -- nothing is read from a database.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			printScopeVersions(out, "system schema versions (this binary):", availableSystemVersion())
			fmt.Fprintln(out)
			printScopeVersions(out, "topic schema versions (this binary):", availableTopicVersion())
			return nil
		},
	}
}

// printScopeVersions lists v1 (the baseline) through the compiled ceiling. Steps
// carry no description in the registry, so the number is all there is to show;
// v1 is annotated because it's created by `migrate init`, not a versioned step.
func printScopeVersions(w io.Writer, title string, ceiling int64) {
	fmt.Fprintln(w, title)
	for v := int64(1); v <= ceiling; v++ {
		if v == 1 {
			fmt.Fprintf(w, "  %d  baseline\n", v)
			continue
		}
		fmt.Fprintf(w, "  %d\n", v)
	}
	if ceiling == 1 {
		fmt.Fprintln(w, "  (no versioned steps compiled in yet)")
	}
}
