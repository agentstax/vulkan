package cron

import (
	"context"
	"testing"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
)

func TestNewCronJobRejects(t *testing.T) {
	type args struct {
		id, systemId, topicId, consumerGroupId int64
		name, schedule                         string
		concurrency                            common.ConcurrencyPolicy
		timeout                                time.Duration
		next                                   time.Time
	}
	valid := args{id: 1, name: "j", schedule: "@hourly", concurrency: common.ConcurrencyAllow, timeout: time.Minute, next: time.Now()}
	build := func(a args) error {
		_, err := NewCronJob(a.id, a.systemId, a.topicId, a.consumerGroupId, a.name, a.schedule,
			a.concurrency, a.timeout, false, nil, nil, a.next, nil)
		return err
	}

	if err := build(valid); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		mut  func(a *args)
	}{
		{name: "zero id", mut: func(a *args) { a.id = 0 }},
		{name: "negative owner id", mut: func(a *args) { a.systemId = -1 }},
		{name: "two owners set", mut: func(a *args) { a.systemId, a.topicId = 1, 1 }},
		{name: "empty name", mut: func(a *args) { a.name = "" }},
		{name: "empty schedule", mut: func(a *args) { a.schedule = "" }},
		{name: "bad concurrency", mut: func(a *args) { a.concurrency = "forbid" }},
		{name: "zero timeout", mut: func(a *args) { a.timeout = 0 }},
		{name: "zero next", mut: func(a *args) { a.next = time.Time{} }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := valid
			c.mut(&a)
			if err := build(a); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

// The guards run before any DB access, so a datastore with no pool exercises
// every rejection; accepted registrations are covered by the DB-backed labs.
func TestRegisterCronJobRejects(t *testing.T) {
	d, err := NewCronJobDatastore(nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg := *(&Config{}).WithDefaults()
	hourly, err := ParseSchedule("@hourly")
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name     string
		jobName  string
		schedule *Schedule
		cfg      Config
	}{
		{name: "star in name is the binding wildcard", jobName: "j*", schedule: hourly, cfg: cfg},
		{name: "uppercase name", jobName: "Job", schedule: hourly, cfg: cfg},
		{name: "empty name", jobName: "", schedule: hourly, cfg: cfg},
		{name: "nil schedule", jobName: "j", schedule: nil, cfg: cfg},
		{name: "timeout exceeds min rate", jobName: "j", schedule: hourly, cfg: *(&Config{Timeout: 2 * time.Hour}).WithDefaults()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := d.registerCronJob(context.Background(), c.jobName, c.schedule, nil, c.cfg); err == nil {
				t.Error("expected an error")
			}
		})
	}
}
