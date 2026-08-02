package cron

import "errors"

// ErrCronJobNotFound means the named cron job has no row.
var ErrCronJobNotFound = errors.New("cron job not found")

// ErrCronJobConfigMismatch means Register was called with a schedule/data/cfg
// that differs from the name's existing row.
var ErrCronJobConfigMismatch = errors.New("cron job config does not match existing cron job")
