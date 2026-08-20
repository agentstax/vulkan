package cron

import "github.com/agentstax/vulkan/pkg/common"

// ErrCronJobNotFound means the named cron job has no row.
var ErrCronJobNotFound = common.NewError("VK0013", common.Permanent,
	"cron job not found",
	"register it with MessageAdmin.RegisterCronJob first")
