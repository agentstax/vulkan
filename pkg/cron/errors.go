package cron

import "github.com/agentstax/vulkan/pkg/common"

// ErrCronJobNotFound means the named cron job has no row.
var ErrCronJobNotFound = common.NewError("VK0013", common.Permanent,
	"cron job not found",
	"register it with MessageAdmin.RegisterCronJob first")

// ErrDeclarationInterrupted means the cron job row was deleted between the
// declaration's insert attempt and its update; an unchanged retry re-creates
// the row, so DatastoreRetry heals the race.
var ErrDeclarationInterrupted = common.NewError("VK0025", common.Transient,
	"could not finish the cron job declaration",
	"rerun the declaration if the cron job should still exist")
