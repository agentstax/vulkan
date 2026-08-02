package alert

import (
	"encoding/json"
	"testing"

	"github.com/agentstax/vulkan/pkg/common"
)

func TestJobsBuildsEveryJob(t *testing.T) {
	jobs, err := Jobs()
	if err != nil {
		t.Fatalf("Jobs: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("want 2 jobs, got %d", len(jobs))
	}
	for _, j := range jobs {
		if j.Schedule == nil {
			t.Errorf("job %q: nil schedule", j.Name)
		}
		if j.Config.Concurrency != common.ConcurrencyDefer {
			t.Errorf("job %q: concurrency = %q, want defer", j.Name, j.Config.Concurrency)
		}
	}
}

func TestThreshold(t *testing.T) {
	if got, err := threshold(nil); err != nil || got != 0 {
		t.Errorf("nil payload: got (%d, %v), want (0, nil)", got, err)
	}
	if got, err := threshold(json.RawMessage(`{"threshold": 7}`)); err != nil || got != 7 {
		t.Errorf("set payload: got (%d, %v), want (7, nil)", got, err)
	}
	if _, err := threshold(json.RawMessage(`{`)); err == nil {
		t.Error("malformed payload: expected an error")
	}
	if _, err := threshold(json.RawMessage(`{"threshold": -1}`)); err == nil {
		t.Error("negative threshold: expected an error")
	}
}
