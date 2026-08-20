package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/agentstax/vulkan/pkg/common/diagnostic"
	"github.com/spf13/cobra"
)

func newExplainCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "explain [code]",
		Short: "Explain a Vulkan error or log-event code, offline",
		Long: "explain renders a declared error condition or log event -- problem,\n" +
			"recovery, fix, docs link -- from the code on any log line or error\n" +
			"message. With no code it lists every declared condition and event.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()
			if len(args) == 0 {
				rows := make([][2]string, 0, 40)
				for _, declared := range diagnostic.Errors() {
					rows = append(rows, [2]string{declared.Code, declared.Problem})
				}
				for _, declared := range diagnostic.Events() {
					rows = append(rows, [2]string{declared.Code, declared.Message})
				}
				slices.SortFunc(rows, func(left [2]string, right [2]string) int {
					return strings.Compare(left[0], right[0])
				})
				for _, row := range rows {
					fmt.Fprintf(w, "%s  %s\n", row[0], row[1])
				}
				return nil
			}

			code := strings.ToUpper(args[0])
			for _, declared := range diagnostic.Errors() {
				if declared.Code != code {
					continue
				}
				fix := declared.Fix
				if cliFix, ok := cliFixes[declared.Code]; ok {
					fix = cliFix
				}
				renderErrorBlock(w, declared, fix)
				return nil
			}
			for _, declared := range diagnostic.Events() {
				if declared.Code != code {
					continue
				}
				renderLogEventBlock(w, declared)
				return nil
			}
			return failOp("unrecognized error code: %q -- `vulkan explain` lists every code", args[0])
		},
	}
}
