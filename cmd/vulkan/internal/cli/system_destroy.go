package cli

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/common/logging"
	metricscontroller "github.com/agentstax/vulkan/pkg/metrics/controller"
	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/agentstax/vulkan/pkg/system"
	"github.com/spf13/cobra"
)

func newSystemDestroyCmd(g *globalFlags) *cobra.Command {
	var (
		force bool
		yes   bool
	)

	cmd := &cobra.Command{
		Use:   "destroy",
		Short: "Permanently delete the system and everything registered on it",
		Long: `Permanently delete everything RegisterSystem created: every topic and its
messages, the system topics, schedules, consumer groups, workers, and the
shared control-plane tables themselves. The database returns to its
pre-register state.

Refused while a manager or consumer still runs, or while non-system topics
are still registered; --force overrides both (running processes fail once
their tables vanish, and user topics are destroyed along with their
messages).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			// the confirmation prompt would pollute the json document stream
			if g.jsonOutput() && !yes {
				return failUsage("refusing to destroy the system without confirmation -- pass --yes with --output json")
			}

			mAdmin, ds, closeAdmin, err := openAdmin(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeAdmin()

			// Check order matters: a doomed call must never waste a prompt.
			// 1. registered?
			if _, err := mAdmin.GetSystem(ctx); err != nil {
				if errors.Is(err, migrate.ErrNotRegistered) {
					return failOp("system is not registered -- nothing to destroy")
				}
				return translateAdminError(err)
			}

			// 2. the guards, pre-flighted so --force is asked for before the
			// prompt. MessageAdmin doesn't expose worker snapshots, so build a
			// metrics controller over the same pool (public API, no pkg change).
			topics, err := mAdmin.ListTopics(ctx)
			if err != nil {
				return translateAdminError(err)
			}
			var userTopics []string
			for _, found := range topics {
				if !strings.HasPrefix(found.Name, common.SystemTopicPrefix) {
					userTopics = append(userTopics, found.Name)
				}
			}
			metricsController, err := metricscontroller.NewMetricsController(ds, &metricscontroller.ControllerConfig{
				Logger: logging.NewDefaultLogger(os.Stderr, slog.LevelError),
			})
			if err != nil {
				return failOp("could not check for live workers: %v", err)
			}
			workers, err := metricsController.WorkerSnapshots(ctx)
			if err != nil {
				return translateAdminError(err)
			}
			var liveWorkers []string
			for _, snapshot := range workers {
				if snapshot.LiveInstances > 0 {
					liveWorkers = append(liveWorkers, snapshot.Name)
				}
			}
			if !force {
				if len(liveWorkers) > 0 {
					return errSystemLive(liveWorkers)
				}
				if len(userTopics) > 0 {
					return errTopicsRegistered(userTopics)
				}
			}

			// 3. confirm, unless --yes. The phrase is the database's name --
			// the system has no name of its own, and typing it proves the
			// operator knows which database they are pointed at.
			var databaseName string
			if err := ds.Pool.QueryRow(ctx, `SELECT current_database();`).Scan(&databaseName); err != nil {
				return failOp("could not read the database name: %v", err)
			}
			if !yes {
				if !stdinIsTTY() {
					return failUsage("refusing to destroy the system without confirmation -- pass --yes in non-interactive contexts (e.g. CI)")
				}
				if len(liveWorkers) > 0 { // implies --force by the gate above
					fmt.Fprintf(out, "%s workers still run (%s) -- --force destroys the schema out from under them.\n", glyphWarn(), strings.Join(liveWorkers, ", "))
				}
				if len(userTopics) > 0 { // implies --force by the gate above
					fmt.Fprintf(out, "%s topics still registered (%s) -- --force destroys them and every message they hold.\n", glyphWarn(), strings.Join(userTopics, ", "))
				}
				fmt.Fprintf(out, "This will PERMANENTLY delete the system in database %q: all %d topics, every message, and the control-plane tables.\n", databaseName, len(topics))
				fmt.Fprintln(out, "This cannot be undone.")
				fmt.Fprintln(out)
				fmt.Fprint(out, "Type the database name to confirm: ")

				typed, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				if strings.TrimSpace(typed) != databaseName {
					// No retry loop -- a piped wrong answer gets one shot, then out.
					fmt.Fprintln(out, "aborted: input did not match database name")
					return failPrinted()
				}
			}

			// 4. destroy.
			if !g.jsonOutput() {
				fmt.Fprintf(out, "destroying the system in %q... ", databaseName)
			}
			if err := mAdmin.DestroySystem(ctx, admin.DestroyOptions{Force: force}); err != nil {
				if !g.jsonOutput() {
					fmt.Fprintln(out) // end the dangling "destroying..." line
				}
				return systemDestroyError(err)
			}

			if g.jsonOutput() {
				writeJSON(out, systemDestroyedDocument{Database: databaseName, Destroyed: true})
				return nil
			}
			fmt.Fprintln(out, "done")
			fmt.Fprintf(out, "%s system destroyed -- database %q returned to its pre-register state\n", glyphOK(), databaseName)
			return nil
		},
	}

	f := cmd.Flags()
	f.BoolVar(&force, "force", false, "destroy even while workers run or topics are still registered")
	f.BoolVarP(&yes, "yes", "y", false, "skip the interactive confirmation (for non-interactive/CI use)")
	return cmd
}

// systemDestroyedDocument is system destroy's json result: the database is
// back to its pre-register state.
type systemDestroyedDocument struct {
	Database  string `json:"database"`
	Destroyed bool   `json:"destroyed"`
}

func errSystemLive(workers []string) error {
	return failOp("workers still run (%s) -- stop running managers and consumers, or pass --force to destroy anyway", strings.Join(workers, ", "))
}

func errTopicsRegistered(topics []string) error {
	return failOp("topics still registered (%s) -- destroy them first, or pass --force to destroy them and their messages", strings.Join(topics, ", "))
}

// systemDestroyError maps a DestroySystem failure to CLI output. Most cases
// are caught in pre-flight; these are the narrow races (a worker starts, or a
// topic registers, between our checks and the destroy) plus anything unexpected.
func systemDestroyError(err error) error {
	switch {
	case errors.Is(err, system.ErrSystemLive), errors.Is(err, system.ErrTopicsRegistered):
		return failOp("%s", err.Error())
	default:
		return translateAdminError(err)
	}
}
