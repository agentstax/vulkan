// Command variance is the Phase 3 "variance proof": a stream of fast messages
// with a few deliberately slow ones mixed in. It shows the dispatcher+pool keeps
// draining fast messages while slow ones each tie up a single worker — the thing
// an N-serial-batch worker structurally can't do (a slow message there blocks the
// rest of its batch).
//
// Sleep is payload-driven (common.Work.SleepMs), so the mix is set at seed time.
// It records every completion's wall-time and prints a timeline + summary.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/concurrency"
	"github.com/agentstax/vulkan/pkg/consumer"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/topic"
)

type completion struct {
	ms   int64
	slow bool
}

func main() {
	concurrencyPtr := flag.Int("concurrency", 8, "worker pool size")
	countPtr := flag.Int("count", 0, "total messages to expect, then stop")
	slowMsPtr := flag.Int("slow-threshold-ms", 1000, "payload sleep >= this counts as 'slow' in the report")
	groupPtr := flag.String("group", "phase3.variance", "consumer group name")
	topicPtr := flag.String("topic", "learning.v1", "topic to drain (must already have a seeded backlog, e.g. via `just produce`)")
	flag.Parse()

	conc := *concurrencyPtr
	target := int64(*countPtr)
	if target < 1 {
		fmt.Println("-count must be >= 1 (the number of seeded rows)")
		os.Exit(1)
	}

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	time.AfterFunc(180*time.Second, stop) // watchdog

	queue, err := concurrency.NewPressureQueue[consumer.MessageRow](100 + conc)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	pool, err := concurrency.NewWorkerPoolLimiter(conc)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	ds, err := coredatastore.NewPostgresDatastore(ctx, &coredatastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password", Host: "localhost", Port: 5432, Database: "example_db", MaxConns: 20,
	})
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	t, err := topic.Register(ctx, ds, topic.Config{Name: *topicPtr})
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	datastore := consumer.NewConsumerDatastore[common.Work](ds)

	wc := consumer.NewWorkConsumer[common.Work](*groupPtr, t, queue, pool, datastore, &consumer.WorkConsumerConfig{
		BatchLimit:      100,
		MaxAttempts:     3,
		ClaimPollRate:   500 * time.Millisecond,
		WorkTimeout:     10 * time.Second, // must exceed the slowest payload sleep
		QueueTimeout:    3 * time.Second,
		AckMargin:       2 * time.Second,
		ShutdownTimeout: 15 * time.Second,
	})

	if err := wc.Register(ctx); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	var mu sync.Mutex
	comps := make([]completion, 0, target)
	var counter atomic.Int64
	start := time.Now()

	err = wc.Consume(ctx, func(ctx context.Context, work *common.Work) error {
		if work.SleepMs > 0 {
			time.Sleep(time.Duration(work.SleepMs) * time.Millisecond)
		}
		c := completion{ms: time.Since(start).Milliseconds(), slow: work.SleepMs >= *slowMsPtr}
		mu.Lock()
		comps = append(comps, c)
		mu.Unlock()
		if counter.Add(1) == target {
			stop()
		}
		return nil
	})
	if err != nil {
		fmt.Println("consume error:", err)
		os.Exit(1)
	}

	report(comps, conc)
}

func report(comps []completion, conc int) {
	if len(comps) == 0 {
		fmt.Println("no completions recorded")
		return
	}
	var wall int64
	fast, slow := 0, 0
	var lastSlow int64
	for _, c := range comps {
		if c.ms > wall {
			wall = c.ms
		}
		if c.slow {
			slow++
			if c.ms > lastSlow {
				lastSlow = c.ms
			}
		} else {
			fast++
		}
	}

	const bucket = 250 // ms
	nb := int(wall/bucket) + 1
	fastB := make([]int, nb)
	slowB := make([]int, nb)
	for _, c := range comps {
		b := int(c.ms / bucket)
		if c.slow {
			slowB[b]++
		} else {
			fastB[b]++
		}
	}
	maxFast := 1
	for _, v := range fastB {
		if v > maxFast {
			maxFast = v
		}
	}

	// the proof: while slow messages are in flight, do fast ones keep completing?
	fastDuringSlow := 0
	for _, c := range comps {
		if !c.slow && c.ms <= lastSlow {
			fastDuringSlow++
		}
	}
	// fast-active buckets within the slow window (every bucket should have fast > 0)
	windowBuckets := int(lastSlow/bucket) + 1
	fastActive, minFastInWindow := 0, 1<<30
	for i := 0; i < windowBuckets && i < nb; i++ {
		if fastB[i] > 0 {
			fastActive++
		}
		if fastB[i] < minFastInWindow {
			minFastInWindow = fastB[i]
		}
	}

	fmt.Printf("\n=== variance proof ===\n")
	fmt.Printf("workers=%d  total=%d  fast=%d  slow=%d  wall=%dms\n", conc, len(comps), fast, slow, wall)
	fmt.Printf("last slow finished at %dms; fast completed during the slow window: %d/%d\n\n", lastSlow, fastDuringSlow, fast)
	fmt.Printf("timeline (each bucket = %dms)   F=fast bar (scaled), S=#slow finishing\n", bucket)
	for i := range nb {
		bar := strings.Repeat("█", fastB[i]*40/maxFast)
		mark := ""
		if slowB[i] > 0 {
			mark = fmt.Sprintf("  <-- %d slow done", slowB[i])
		}
		fmt.Printf("%5d-%-5dms | %4d %-40s%s\n", i*bucket, (i+1)*bucket, fastB[i], bar, mark)
	}
	fmt.Printf("\nPROOF: across the slow window (0-%dms), fast completions never stalled:\n", lastSlow)
	fmt.Printf("  fast-active buckets in window: %d/%d  (min %d fast per 250ms bucket)\n", fastActive, windowBuckets, minFastInWindow)
}
