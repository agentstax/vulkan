package cli

import (
	"fmt"
	"strings"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/spf13/cobra"
)

func newExplainCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "explain [code]",
		Short: "Explain a Vulkan error code, offline",
		Long: "explain renders a declared error condition -- problem, recovery, fix,\n" +
			"docs link -- from the code on any log line or error message. With no\n" +
			"code it lists every declared condition.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()
			if len(args) == 0 {
				for _, declared := range common.Errors() {
					fmt.Fprintf(w, "%s  %s\n", declared.Code, declared.Problem)
				}
				return nil
			}

			code := strings.ToUpper(args[0])
			for _, declared := range common.Errors() {
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
			return failOp("unrecognized error code: %q -- `vulkan explain` lists every code", args[0])
		},
	}
}
