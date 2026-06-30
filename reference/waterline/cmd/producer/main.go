// Command producer appends events to the log. It mirrors the LEARNING_PLAN
// harness knobs (--count) and adds the routing/partition attributes for Phases
// 7-8 (--topic, --routing-key, --partition-key, --headers) plus --tombstone for
// the Phase 9 compaction lab. Use --migrate once to (re)create the schema.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/agentstax/vulkan/reference/waterline"
)

func dsn() string {
	if v := os.Getenv("WATERLINE_DSN"); v != "" {
		return v
	}
	return "postgres://bench:bench@localhost:5433/bench"
}

func main() {
	count := flag.Int("count", 1, "number of events to append")
	migrate := flag.Bool("migrate", false, "(re)create the schema first (destructive)")
	topic := flag.String("topic", "", "event topic (optional)")
	routingKey := flag.String("routing-key", "", "routing key for topic bindings, e.g. orders.eu.created")
	partitionKey := flag.String("partition-key", "", "partition key for FIFO ordering (Phase 8)")
	headers := flag.String("headers", "", "headers JSON object for header bindings, e.g. {\"region\":\"eu\"}")
	tombstone := flag.Bool("tombstone", false, "append a NULL-payload tombstone (Phase 9 compaction)")
	flag.Parse()

	ctx := context.Background()
	l, err := waterline.New(ctx, dsn())
	if err != nil {
		fmt.Fprintln(os.Stderr, "connect:", err)
		os.Exit(1)
	}
	defer l.Close()

	if *migrate {
		if err := l.Migrate(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "migrate:", err)
			os.Exit(1)
		}
		fmt.Println("schema (re)created")
	}

	for i := range *count {
		spec := waterline.AppendSpec{
			Topic:        optStr(*topic),
			RoutingKey:   optStr(*routingKey),
			PartitionKey: optStr(*partitionKey),
			Headers:      *headers,
			Payload:      []byte(fmt.Sprintf(`{"seq":%d}`, i)),
		}
		if *tombstone {
			spec.Payload = nil
		}
		off, err := l.Append(ctx, spec)
		if err != nil {
			fmt.Fprintln(os.Stderr, "append:", err)
			os.Exit(1)
		}
		fmt.Printf("appended offset=%d\n", off)
	}
}

func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
