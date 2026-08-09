package metrics

import "time"

// AbandonedRoutineSnapshot is derived from the __system.metrics event stream
// for one (topic, group) -- no in-process counter is kept anywhere, every
// number here comes from pairing abandoned/cleared events already on the
// topic.
type AbandonedRoutineSnapshot struct {
	Outstanding         int64         // abandoned events with no matching cleared
	Total               int64         // distinct abandoned keys currently in the window
	SelfClearLatencyAvg time.Duration // mean(cleared.At - abandoned.At) over matched pairs; 0 if no pair has cleared yet
}
