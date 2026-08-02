package maintain

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	"golang.org/x/sync/errgroup"
)

// Duty is one duty/claim-coordinated maintenance loop.
// Janitor, WaterlineRoller and Scheduler implement it.
//
// Register is offered a maintenance row's identity: duty is the row's kind,
// owner the row's owner, meta the row's tuning. A duty that doesn't run the
// passed kind declines with (false, nil); (true, nil) means it registered and
// is ready to Run.
type Duty interface {
	Register(ctx context.Context, duty string, owner *common.Owner, meta *DutyMetadata) (bool, error)
	Run(ctx context.Context) error
}

// Maintainer runs a fixed set of already-registered duties in one errgroup --
// the bundled counterpart to running each duty as its own process.
type Maintainer struct {
	duties []Duty
}

func NewMaintainer(duties ...Duty) (*Maintainer, error) {
	if len(duties) == 0 {
		return nil, errors.New("at least one duty is required")
	}
	return &Maintainer{duties: duties}, nil
}

// Run runs every duty until ctx cancels; duties return nil on a requested
// stop, so a clean shutdown surfaces as nil here too.
func (m *Maintainer) Run(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)
	for _, d := range m.duties {
		g.Go(func() error {
			return d.Run(ctx)
		})
	}
	return g.Wait()
}
