package main

// throwaway check for WorkerSnapshots -- not a lab, delete after chunk 11a review

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/consumer"
	"github.com/agentstax/vulkan/pkg/datastore"
	metricsdatastore "github.com/agentstax/vulkan/pkg/metrics/datastore"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
)

type Message struct{ Data string }

func main() {
	ctx := context.Background()

	ds, err := datastore.NewPostgresDatastore(ctx, &datastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password", Host: "localhost", Port: 5432, Database: "example_db",
	})
	must(err)
	defer ds.Close()

	mAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	must(err)
	must(mAdmin.RegisterSystem(ctx, nil))

	const topicName = "test.workersnapshot"
	_, err = mAdmin.RegisterTopic(ctx, topicName, topic.SchemaVersion(1), &topiccontroller.TopicConfig{})
	must(err)

	metricsDsCold, err := metricsdatastore.NewMetricsDatastore(ds, nil)
	must(err)
	coldJobs, err := metricsDsCold.CronJobSnapshots(ctx)
	must(err)
	fmt.Println("cold (no scheduler running):")
	for _, j := range coldJobs {
		fmt.Printf("%-32s suspended=%-5v overdue=%-5v due_for=%s\n", j.Name, j.Suspended, j.Overdue, j.DueFor.Round(time.Millisecond))
	}

	c, err := consumer.NewConsumer[Message](ds, nil)
	must(err)
	instance, err := c.Register(ctx, "snapshot.group", topicName, topic.SchemaVersion(1))
	must(err)

	consumeCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		done <- instance.Consume(consumeCtx, func(ctx context.Context, m *Message) error { return nil })
	}()

	// let the manager claim itself and spawn/claim the chain
	time.Sleep(5 * time.Second)

	metricsDs, err := metricsdatastore.NewMetricsDatastore(ds, nil)
	must(err)
	snapshots, err := metricsDs.WorkerSnapshots(ctx)
	must(err)

	fmt.Printf("\n%-18s %-14s %-24s %-10s %6s %5s %8s %12s %12s\n",
		"WORKER", "OWNER KIND", "OWNER NAME", "STATUS", "TARGET", "LIVE", "ATTEMPTS", "OLDEST AGE", "UNCLAIMED")
	for _, s := range snapshots {
		fmt.Printf("%-18s %-14s %-24s %-10s %6d %5d %8d %12s %12s\n",
			s.Name, s.Owner.Kind(), s.Owner.Name, s.Status, s.TargetInstances, s.LiveInstances,
			s.Attempts, s.OldestInstanceAge.Round(time.Millisecond), s.UnclaimedFor.Round(time.Millisecond))
	}

	cronJobs, err := metricsDs.CronJobSnapshots(ctx)
	must(err)
	fmt.Printf("\n%-32s %-14s %-14s %-14s %-9s %-8s %12s %-20s\n", "CRON JOB", "OWNER KIND", "OWNER NAME", "SCHEDULE", "SUSPENDED", "OVERDUE", "DUE FOR", "LAST SCHEDULED")
	for _, j := range cronJobs {
		last := "never"
		if !j.LastScheduledTime.IsZero() {
			last = j.LastScheduledTime.Format("15:04:05")
		}
		fmt.Printf("%-32s %-14s %-14s %-14s %-9v %-8v %12s %-20s\n",
			j.Name, j.Owner.Kind(), j.Owner.Name, j.Schedule, j.Suspended, j.Overdue, j.DueFor.Round(time.Millisecond), last)
	}

	stop()
	<-done

	// after shutdown: everything the consumer held should read unclaimed
	snapshots, err = metricsDs.WorkerSnapshots(ctx)
	must(err)
	fmt.Println("\nafter shutdown:")
	for _, s := range snapshots {
		fmt.Printf("%-18s %-24s %-10s live=%d unclaimed_for=%s\n",
			s.Name, s.Owner.Name, s.Status, s.LiveInstances, s.UnclaimedFor.Round(time.Millisecond))
	}
}

func must(err error) {
	if err != nil {
		fmt.Println("FAILED:", err)
		os.Exit(1)
	}
}
