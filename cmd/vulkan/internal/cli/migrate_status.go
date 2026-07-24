package cli

import (
	"errors"
	"fmt"
	"text/tabwriter"

	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/spf13/cobra"
)

func newMigrateStatusCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Compare each schema's current version against what this binary offers",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			mAdmin, ds, closeAdmin, err := openAdmin(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeAdmin()

			sysCurrent, err := migrate.Version(ctx, ds.Pool, migrate.EntitySystem, systemEntityID)
			if err != nil {
				if errors.Is(err, migrate.ErrNotRegistered) {
					fmt.Fprintln(out, "system schema not initialized -- run `vulkan migrate init`")
					return nil
				}
				return translateAdminError(err)
			}

			topics, err := mAdmin.ListTopics(ctx)
			if err != nil {
				return translateAdminError(err)
			}

			sysAvail := availableSystemVersion()
			topicAvail := availableTopicVersion()

			// Read every current version up front so the behind-summary can be
			// computed before anything prints.
			type row struct {
				name      string
				current   int64
				available int64
			}
			rows := []row{{name: "system", current: sysCurrent, available: sysAvail}}
			for _, t := range topics {
				current, err := migrate.Version(ctx, ds.Pool, migrate.EntityTopic, t.Id)
				if err != nil {
					return translateAdminError(err)
				}
				rows = append(rows, row{name: t.Name, current: current, available: topicAvail})
			}

			fmt.Fprintf(out, "latest available: system %d, topic %d\n\n", sysAvail, topicAvail)

			tw := tabwriter.NewWriter(out, 0, 0, 3, ' ', 0)
			fmt.Fprintln(tw, "SCHEMA\tCURRENT\tAVAILABLE")
			for _, r := range rows {
				fmt.Fprintf(tw, "%s\t%d\t%d\n", r.name, r.current, r.available)
			}
			tw.Flush()

			// A binary older than the DB (current > available) is fine, not behind
			// -- only current < available is actionable.
			systemBehind := sysCurrent < sysAvail
			topicsBehind := 0
			for _, r := range rows[1:] {
				if r.current < r.available {
					topicsBehind++
				}
			}
			if systemBehind || topicsBehind > 0 {
				fmt.Fprintln(out)
			}
			if systemBehind {
				fmt.Fprintf(out, "system behind (%d < %d) -- run `vulkan migrate system up --to %d`\n", sysCurrent, sysAvail, sysAvail)
			}
			if topicsBehind > 0 {
				fmt.Fprintf(out, "%s behind -- run `vulkan migrate topics up --to %d`\n", pluralize(topicsBehind, "topic"), topicAvail)
			}
			return nil
		},
	}
}
