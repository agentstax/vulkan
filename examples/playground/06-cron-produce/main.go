// Scenario 06 -- a cron-scheduled job.
//
// Nightly invoice run: register the schedule once, consume its requests.
//
// Concepts held before domain code (12): the consume set from scenario 03,
// plus MessageAdmin (needed here, for RegisterCronJob), cron.ParseSchedule,
// CronJobConfig, cron.JobRequest, cron.TopicName, the job name doubling
// as the routing key / binding, and the fact that the scheduler worker
// runs only under a consumer or `vulkan manager run`.
//
// Traps hit:
//   - The job's requests land on a system topic (cron.TopicName) and the
//     binding is the job name: a user must know the topic constant and
//     that name == routing key to consume their own job.
//   - The job's payload is `any` at registration but arrives as
//     json.RawMessage on JobRequest.Payload -- the user unmarshals by hand
//     with no type linking the two ends.
//   - Nothing produces if no consumer or manager is running; a
//     register-and-exit program registers a job that never fires.
//   - A one-off "run this in 10 minutes" is not this feature; it is
//     queue-native delayed delivery (future roadmap).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumer"
	"github.com/agentstax/vulkan/pkg/cron"
	"github.com/agentstax/vulkan/pkg/datastore"
)

type InvoiceRun struct {
	Region string `json:"region"`
}

func main() {
	if err := run(); err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := common.LifecycleContext(nil)
	defer stop()

	ds, err := datastore.NewPostgresDatastore(ctx, "example_user", "localhost", "example_db",
		&datastore.PostgresConnectionConfig{Pass: "example_password"})
	if err != nil {
		return err
	}
	defer ds.Close()

	messageAdmin, err := admin.NewMessageAdmin(ds, nil)
	if err != nil {
		return err
	}
	if err := messageAdmin.RegisterSystem(ctx, nil); err != nil {
		return err
	}

	schedule, err := cron.ParseSchedule("0 2 * * *")
	if err != nil {
		return err
	}
	_, err = messageAdmin.RegisterCronJob(ctx, "invoices.nightly", schedule, InvoiceRun{Region: "eu"}, nil)
	if err != nil {
		return err
	}

	jobConsumer, err := consumer.NewConsumer[cron.JobRequest](ds, nil)
	if err != nil {
		return err
	}
	invoices, err := jobConsumer.Register(ctx, "invoice-runner", cron.TopicName, []string{"invoices.nightly"})
	if err != nil {
		return err
	}

	return invoices.Consume(ctx, func(ctx context.Context, request *cron.JobRequest) error {
		var run InvoiceRun
		if err := json.Unmarshal(request.Payload, &run); err != nil {
			return err
		}
		fmt.Printf("invoicing %s for %s\n", run.Region, request.ScheduledAt.Format("2006-01-02"))
		return nil
	})
}
