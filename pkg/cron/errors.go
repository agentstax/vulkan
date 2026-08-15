package cron

import "errors"

// ErrCronJobNotFound means the named cron job has no row.
var ErrCronJobNotFound = errors.New("cron job not found")
