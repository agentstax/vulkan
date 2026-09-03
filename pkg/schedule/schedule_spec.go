package schedule

// ScheduleSpec names a schedule declaration: the schedule, the topic it
// produces to, and the cron expression it runs on. A struct rather than
// three positional strings -- a transposed pair of names would compile
// and register a schedule that produces onto the wrong topic.
type ScheduleSpec struct {
	Name  string
	Topic string
	Cron  string
}
