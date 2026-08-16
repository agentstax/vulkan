package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/spf13/cobra"
)

func newGroupConfigGetCmd(g *globalFlags) *cobra.Command {
	var schemaVersion int64

	cmd := &cobra.Command{
		Use:   "get <topic> <group> [key]",
		Short: "Show the group's config",
		Long: `Show each config key the group's consumer kinds declare, and the value it
runs with. Pass a key to show just that key; message shows one line per
field.`,
		Example: `  vulkan group config get orders billing
  vulkan group config get orders billing claim_poll_rate
  vulkan group config get orders billing message`,
		Args: cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			topicName, groupName := args[0], args[1]
			key := ""
			if len(args) == 3 {
				key = args[2]
			}
			out := cmd.OutOrStdout()

			mAdmin, _, closeAdmin, err := openAdmin(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeAdmin()

			workers, err := mAdmin.GetGroup(ctx, topicName, topic.SchemaVersion(schemaVersion), groupName)
			if err != nil {
				return groupError(topicName, groupName, err)
			}

			lines := groupConfigLines(workers)
			if len(lines) == 0 {
				fmt.Fprintf(out, "%s consumer group %q on topic %q\n", glyphOK(), groupName, topicName)
				fmt.Fprintln(out, "  (no config -- the group has no consumer worker rows yet; they appear at the group's first Consume)")
				return nil
			}
			if key != "" {
				lines = filterGroupConfigLines(lines, key)
				if len(lines) == 0 {
					return failOp("no consumer kind of group %q declares config key %q", groupName, key)
				}
			}

			fmt.Fprintf(out, "%s consumer group %q on topic %q\n", glyphOK(), groupName, topicName)
			printGroupConfigLines(out, lines)
			return nil
		},
	}

	f := cmd.Flags()
	f.Int64Var(&schemaVersion, "schema-version", 1, "which registered version of the topic the group belongs to")
	return cmd
}

// groupConfigLine is one config key on one worker row, ready to print.
type groupConfigLine struct {
	key    string
	worker string
	value  string
}

// groupConfigLines flattens the rows' metadata into print lines: one per key,
// and one per message field so the KEY column names the field itself.
func groupConfigLines(workers []*worker.Worker) []groupConfigLine {
	var lines []groupConfigLine
	for _, row := range workers {
		metadata, ok := row.Metadata.(map[string]any)
		if !ok {
			continue
		}
		for key, value := range metadata {
			if key == "message" {
				lines = append(lines, messageConfigLines(row.Name, value)...)
				continue
			}
			lines = append(lines, groupConfigLine{
				key:    key,
				worker: row.Name,
				value:  formatMetadataValue(key, value),
			})
		}
	}
	slices.SortFunc(lines, func(a, b groupConfigLine) int {
		if c := strings.Compare(a.key, b.key); c != 0 {
			return c
		}
		return strings.Compare(a.worker, b.worker)
	})
	return lines
}

// messageConfigLines is one row's message document expanded to a line per
// field. A document that doesn't decode prints as raw JSON rather than
// dropping out of the table.
func messageConfigLines(workerName string, document any) []groupConfigLine {
	options, err := decodeMessageOptions(document)
	if err != nil {
		return []groupConfigLine{{
			key:    "message",
			worker: workerName,
			value:  formatMetadataValue("message", document),
		}}
	}

	var lines []groupConfigLine
	for _, field := range messageFieldKeys {
		lines = append(lines, groupConfigLine{
			key:    field.path,
			worker: workerName,
			value:  field.read(options),
		})
	}
	return lines
}

// filterGroupConfigLines keeps one key's lines -- message matches all its
// fields.
func filterGroupConfigLines(lines []groupConfigLine, key string) []groupConfigLine {
	var kept []groupConfigLine
	for _, line := range lines {
		if line.key == key || strings.HasPrefix(line.key, key+".") {
			kept = append(kept, line)
		}
	}
	return kept
}

func printGroupConfigLines(w io.Writer, lines []groupConfigLine) {
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "  KEY\tWORKER\tVALUE")
	for _, line := range lines {
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", line.key, line.worker, cellOrDash(line.value))
	}
	tw.Flush()
}

// formatMetadataValue renders one metadata value for the table -- duration
// keys arrive from JSONB as float64 nanoseconds.
func formatMetadataValue(key string, value any) string {
	if value == nil {
		return ""
	}
	switch key {
	case "claim_poll_rate", "exception_initial_backoff", "poll_rate", "repeat_interval":
		if ns, ok := value.(float64); ok {
			return time.Duration(int64(ns)).String()
		}
	}
	switch v := value.(type) {
	case string:
		return v
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(raw)
}

func cellOrDash(cell string) string {
	if cell == "" {
		return "-"
	}
	return cell
}

// durationCell renders a decoded duration field; zero means absent.
func durationCell(duration time.Duration) string {
	if duration == 0 {
		return ""
	}
	return duration.String()
}

// intCell renders a decoded int field; zero means absent.
func intCell(value int) string {
	if value == 0 {
		return ""
	}
	return strconv.Itoa(value)
}
