package maintain

import (
	"io"
	"testing"

	"github.com/agentstax/vulkan/pkg/logger"
)

// fake pool entry: finished flips whether its Run counts as already returned.
func fakeSpawned(finished bool) *spawnedDuty {
	sd := &spawnedDuty{stop: func() {}, done: make(chan struct{})}
	if finished {
		close(sd.done)
	}
	return sd
}

func TestDutyPoolDiff(t *testing.T) {
	unchanged := FleetDuty{Duty: DutyJanitor, TopicID: 1, TopicName: "a"}
	added := FleetDuty{Duty: DutyJanitor, TopicID: 2, TopicName: "b"}
	removed := FleetDuty{Duty: DutyWaterline, TopicID: 1, TopicName: "a", ConsumerGroup: "g"}
	exited := FleetDuty{Duty: DutyWaterline, TopicID: 2, TopicName: "b", ConsumerGroup: "g"}

	p := newDutyPool(logger.NewDefaultLogger(io.Discard), nil) // diff never touches the builder
	p.running[unchanged] = fakeSpawned(false)
	p.running[removed] = fakeSpawned(false)
	p.running[exited] = fakeSpawned(true)

	got := make(map[FleetDuty]changeType)
	for _, c := range p.diff([]FleetDuty{unchanged, added, exited}) {
		if _, dup := got[c.key]; dup {
			t.Fatalf("diff returned key %+v twice", c.key)
		}
		got[c.key] = c.change
	}

	want := map[FleetDuty]changeType{
		added:   dutyAdded,
		removed: dutyRemoved,
		exited:  dutyExited,
	}
	if len(got) != len(want) {
		t.Fatalf("diff returned %d changes, want %d (got %v)", len(got), len(want), got)
	}
	for key, ct := range want {
		if got[key] != ct {
			t.Fatalf("key %+v: change %v, want %v", key, got[key], ct)
		}
	}
}
