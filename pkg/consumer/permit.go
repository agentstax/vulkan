package consumer

import (
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/agentstax/vulkan/pkg/common"
	vulkanerrors "github.com/agentstax/vulkan/pkg/errors"
)

// consumePermit is held for the length of a Consume call, so a second Consume
// on the same group is refused rather than running a rival set of runners.
type consumePermit struct {
	owner *common.Owner
	held  atomic.Bool
}

func newConsumePermit(owner *common.Owner) (*consumePermit, error) {
	if owner == nil {
		return nil, errors.New("owner must not be nil")
	}
	return &consumePermit{owner: owner}, nil
}

func (p *consumePermit) acquire() (func(), error) {
	if !p.held.CompareAndSwap(false, true) {
		return nil, fmt.Errorf("%w: consumer group %q on topic %d", vulkanerrors.ErrAlreadyConsuming, p.owner.Name, p.owner.TopicId)
	}
	return func() { p.held.Store(false) }, nil
}
