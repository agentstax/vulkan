package cli

import (
	"errors"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/agentstax/vulkan/pkg/migrate"
	migratecontroller "github.com/agentstax/vulkan/pkg/migrate/controller"
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

			client, ds, closeClient, err := openClient(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeClient()

			controller, err := migratecontroller.NewController(ds, nil)
			if err != nil {
				return err
			}

			sysOwner, err := controller.SystemOwner(ctx)
			if err != nil {
				if errors.Is(err, migrate.ErrNotRegistered) {
					return migrateStatusNotInitialized(out, g)
				}
				return translateAdminError(err)
			}
			sysCurrent, err := controller.SystemVersion(ctx, sysOwner.SystemId)
			if err != nil {
				if errors.Is(err, migrate.ErrNotRegistered) {
					return migrateStatusNotInitialized(out, g)
				}
				return translateAdminError(err)
			}

			topics, err := client.ListTopics(ctx)
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
				current, err := controller.TopicVersion(ctx, t.Id)
				if err != nil {
					return translateAdminError(err)
				}
				rows = append(rows, row{name: t.Name, current: current, available: topicAvail})
			}

			if g.jsonOutput() {
				document := migrateStatusDocument{
					Initialized:     true,
					SystemAvailable: sysAvail,
					TopicAvailable:  topicAvail,
					Schemas:         make([]migrateSchemaDocument, 0, len(rows)),
				}
				for _, r := range rows {
					document.Schemas = append(document.Schemas, migrateSchemaDocument{
						Schema:    r.name,
						Current:   r.current,
						Available: r.available,
						Behind:    r.current < r.available,
					})
				}
				writeJSON(out, document)
				return nil
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

// migrateStatusDocument is migrate status's json result. Initialized false
// means the control-plane schema was never created; schemas is then empty.
type migrateStatusDocument struct {
	Initialized     bool                    `json:"initialized"`
	SystemAvailable int64                   `json:"system_available"`
	TopicAvailable  int64                   `json:"topic_available"`
	Schemas         []migrateSchemaDocument `json:"schemas"`
}

// migrateSchemaDocument is one schema row: the system schema or one
// registered topic.
type migrateSchemaDocument struct {
	Schema    string `json:"schema"`
	Current   int64  `json:"current"`
	Available int64  `json:"available"`
	Behind    bool   `json:"behind"`
}

// migrateStatusNotInitialized is the shared never-initialized result: still
// exit 0 -- an uninitialized database is an answer, not a failure.
func migrateStatusNotInitialized(w io.Writer, g *globalFlags) error {
	if g.jsonOutput() {
		writeJSON(w, migrateStatusDocument{
			SystemAvailable: availableSystemVersion(),
			TopicAvailable:  availableTopicVersion(),
			Schemas:         make([]migrateSchemaDocument, 0),
		})
		return nil
	}
	fmt.Fprintln(w, "system schema not initialized -- run `vulkan migrate init`")
	return nil
}
