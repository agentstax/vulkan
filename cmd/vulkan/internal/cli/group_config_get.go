package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/spf13/cobra"
)

func newGroupConfigGetCmd(g *globalFlags) *cobra.Command {
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

			client, closeClient, err := openClient(ctx, g.databaseURL, g.schema, slog.LevelError)
			if err != nil {
				return err
			}
			defer closeClient()

			workers, err := client.Topic(topicName).Group(groupName).Workers(ctx)
			if err != nil {
				return groupError(topicName, groupName, err)
			}

			lines := groupConfigLines(workers)
			if key != "" && len(lines) > 0 {
				lines = filterGroupConfigLines(lines, key)
				if len(lines) == 0 {
					return failOp("no consumer kind of group %q declares config key %q", groupName, key)
				}
			}

			if g.jsonOutput() {
				writeJSON(out, toGroupConfigDocument(topicName, groupName, lines))
				return nil
			}

			if len(lines) == 0 {
				fmt.Fprintf(out, "%s consumer group %q on topic %q\n", glyphOK(), groupName, topicName)
				fmt.Fprintln(out, "  (no config -- the group has no consumer worker rows yet; they appear at the group's first Consume)")
				return nil
			}

			fmt.Fprintf(out, "%s consumer group %q on topic %q\n", glyphOK(), groupName, topicName)
			printGroupConfigLines(out, lines)
			return nil
		},
	}

	return cmd
}

// groupConfigLine is one config key on one worker row, ready to print.
type groupConfigLine struct {
	key    string
	worker string
	value  string
}

// groupConfigDocument is group config get's json result: each declared key
// with the worker row it came from, as the table renders them.
type groupConfigDocument struct {
	Topic string                    `json:"topic"`
	Group string                    `json:"group"`
	Keys  []groupConfigLineDocument `json:"keys"`
}

type groupConfigLineDocument struct {
	Key    string `json:"key"`
	Worker string `json:"worker"`
	Value  string `json:"value"`
}

func toGroupConfigDocument(topicName string, groupName string, lines []groupConfigLine) groupConfigDocument {
	keys := make([]groupConfigLineDocument, 0, len(lines))
	for _, line := range lines {
		keys = append(keys, groupConfigLineDocument{Key: line.key, Worker: line.worker, Value: line.value})
	}
	return groupConfigDocument{Topic: topicName, Group: groupName, Keys: keys}
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

// formatMetadataValue renders one metadata value for the table -- a duration
// key arrives from JSONB as float64 nanoseconds, indistinguishable from a
// plain count, so the key's name is what tells them apart.
func formatMetadataValue(key string, value any) string {
	if value == nil {
		return ""
	}
	if nanoseconds, ok := value.(float64); ok && isDurationKey(key) {
		return time.Duration(int64(nanoseconds)).String()
	}
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(raw)
}

// durationKeySuffixes are how every worker kind names a time.Duration field:
// poll_rate, repeat_interval, exception_initial_backoff. A kind naming one
// some other way prints its raw nanoseconds until the name joins this list.
var durationKeySuffixes = []string{"_rate", "_interval", "_backoff", "_ttl", "_timeout", "_delay", "_margin"}

func isDurationKey(key string) bool {
	for _, suffix := range durationKeySuffixes {
		if strings.HasSuffix(key, suffix) {
			return true
		}
	}
	return false
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
