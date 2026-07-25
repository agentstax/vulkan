package maintain

import (
	"context"
	"errors"

	"golang.org/x/sync/errgroup"
)

// Duty is one duty/claim-coordinated maintenance loop.
// Janitor and WaterlineRoller implement it.
type Duty interface {
	Register(ctx context.Context) error
	Run(ctx context.Context) error
}

// Maintainer runs a fixed set of duties in one errgroup -- the bundled
// counterpart to running each duty as its own process.
type Maintainer struct {
	duties []Duty
}

func NewMaintainer(duties ...Duty) (*Maintainer, error) {
	if len(duties) == 0 {
		return nil, errors.New("at least one duty is required")
	}
	return &Maintainer{duties: duties}, nil
}

func (m *Maintainer) Register(ctx context.Context) error {
	for _, d := range m.duties {
		if err := d.Register(ctx); err != nil {
			return err
		}
	}
	return nil
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
