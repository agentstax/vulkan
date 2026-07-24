package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/spf13/cobra"
)

func newMigrateSystemCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "system",
		Short: "Migrate the shared control-plane schema",
	}
	cmd.AddCommand(newDirectionCmd(g, scopeSystem, dirUp))
	cmd.AddCommand(newDirectionCmd(g, scopeSystem, dirDown))
	return cmd
}

func newMigrateTopicsCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "topics",
		Short: "Migrate every registered topic's schema",
	}
	cmd.AddCommand(newDirectionCmd(g, scopeTopics, dirUp))
	cmd.AddCommand(newDirectionCmd(g, scopeTopics, dirDown))
	return cmd
}

func newMigrateTopicCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "topic",
		Short: "Migrate a single topic's schema, by name",
	}
	cmd.AddCommand(newDirectionCmd(g, scopeTopic, dirUp))
	cmd.AddCommand(newDirectionCmd(g, scopeTopic, dirDown))
	return cmd
}

// newDirectionCmd builds one up/down leaf. All six leaves (three scopes x two
// directions) share this body -- they differ only in the scope they resolve and
// the direction they guard. The topic scope alone takes a <name> positional.
func newDirectionCmd(g *globalFlags, s scope, dir direction) *cobra.Command {
	var to int64

	use := dir.verb()
	args := cobra.NoArgs
	if s == scopeTopic {
		use = dir.verb() + " <name>"
		args = requireMigrateTopicName(dir)
	}

	cmd := &cobra.Command{
		Use:   use,
		Short: fmt.Sprintf("Migrate the %s schema %s to --to N", scopeNoun(s), directionWord(dir)),
		Args:  args,
		RunE: func(cmd *cobra.Command, cmdArgs []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			// --to is mandatory: no implicit "to latest" on up, no implicit
			// "one step back" on down. The message is direction-specific.
			if !cmd.Flags().Changed("to") {
				return errToRequired(s, dir)
			}
			if ceiling := s.ceiling(); to < 1 || to > ceiling {
				return failUsage("--to %d is out of range [1, %d] for this binary -- run `vulkan migrate versions` to see what's available", to, ceiling)
			}

			name := ""
			if s == scopeTopic {
				name = cmdArgs[0]
			}

			mAdmin, ds, closeAdmin, err := openAdmin(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeAdmin()

			targets, err := gatherTargets(ctx, mAdmin, ds.Pool, s, name)
			if err != nil {
				return err
			}
			if s == scopeTopics && len(targets) == 0 {
				fmt.Fprintln(out, "no topics registered")
				return nil
			}

			moving, err := guardDirection(targets, dir, to)
			if err != nil {
				return err
			}
			if moving == 0 {
				printMigrateNoop(out, s, targets, to)
				return nil
			}

			// Fast pre-flight, not a guarantee -- see migrate.IsLocked. Catches the
			// common case (another migrate already running) before committing to a
			// call that would otherwise block silently until that one finishes.
			locked, err := migrate.IsLocked(ctx, ds.Pool)
			if err != nil {
				return translateAdminError(err)
			}
			if locked {
				return failOp("another migration is already in progress (advisory lock held) -- wait for it to finish, or confirm no other migrate process is actually running before retrying")
			}

			if err := runScopeMigrate(ctx, mAdmin, s, name, to); err != nil {
				return migrateError(err)
			}
			printMigrateResult(out, s, dir, targets, to, moving)
			return nil
		},
	}

	cmd.Flags().Int64Var(&to, "to", 0, "target schema version (required)")
	return cmd
}

// requireMigrateTopicName is the Args rule for `migrate topic up|down <name>` --
// its own validator, not the topic-command one, so the usage line names the right
// path (`vulkan migrate topic ...`, not `vulkan topic ...`).
func requireMigrateTopicName(dir direction) cobra.PositionalArgs {
	verb := dir.verb()
	return func(_ *cobra.Command, args []string) error {
		if len(args) < 1 {
			return failUsage("%s requires a topic name\nusage: vulkan migrate topic %s <name> --to N", verb, verb)
		}
		if len(args) > 1 {
			return failUsage("migrate topic %s takes exactly one topic name", verb)
		}
		return nil
	}
}

// errToRequired is the direction-specific teaching error for a missing --to.
func errToRequired(s scope, dir direction) error {
	if dir == dirDown {
		return failUsage("--to is required for %s down -- downgrades name an explicit target, there's no implicit \"down one step\"", scopeNoun(s))
	}
	return failUsage("--to is required (e.g. --to %d) -- run `vulkan migrate versions` to see what's available", s.ceiling())
}

func runScopeMigrate(ctx context.Context, mAdmin *admin.MessageAdmin, s scope, name string, to int64) error {
	switch s {
	case scopeSystem:
		return mAdmin.MigrateSystem(ctx, to)
	case scopeTopic:
		return mAdmin.MigrateTopic(ctx, name, to)
	default:
		return mAdmin.MigrateTopics(ctx, to)
	}
}

func printMigrateNoop(w io.Writer, s scope, targets []migrateTarget, to int64) {
	if s == scopeTopics {
		fmt.Fprintf(w, "%s all topics already at version %d, nothing to do\n", glyphOK(), to)
		return
	}
	fmt.Fprintf(w, "%s %s already at version %d, nothing to do\n", glyphOK(), singleLabel(s, targets), to)
}

func printMigrateResult(w io.Writer, s scope, dir direction, targets []migrateTarget, to int64, moving int) {
	if s == scopeTopics {
		fmt.Fprintf(w, "%s migrated %s %s to version %d\n", glyphOK(), pluralize(moving, "topic"), dir.verb(), to)
		return
	}
	fmt.Fprintf(w, "%s %s migrated %s to version %d\n", glyphOK(), singleLabel(s, targets), dir.verb(), to)
}

func singleLabel(s scope, targets []migrateTarget) string {
	if s == scopeTopic {
		return fmt.Sprintf("topic %q", targets[0].label)
	}
	return "system"
}

func scopeNoun(s scope) string {
	switch s {
	case scopeSystem:
		return "system"
	case scopeTopic:
		return "topic"
	default:
		return "topics"
	}
}

// directionWord is the human phrasing for a Short line -- "forward"/"back",
// where verb() gives the command word "up"/"down".
func directionWord(dir direction) string {
	if dir == dirDown {
		return "back"
	}
	return "forward"
}
