package datastore

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// CreateAheadGate decides when an append should create the next partition
// early. Ids come from one sequence, so exactly one append fleet-wide sees
// each trigger point id -- no coordination.
type CreateAheadGate struct {
	triggerPointPercentages []float64
	data                    sync.Map // topicId -> *atomic.Int64
}

// triggerPointPercentages are positions within a partition, e.g. .80 & .95
func NewCreateAheadGate(triggerPointPercentages []float64) (*CreateAheadGate, error) {
	for _, triggerPointPercentage := range triggerPointPercentages {
		if !(triggerPointPercentage > 0.0 && triggerPointPercentage < 1.0) {
			return nil, fmt.Errorf("trigger point percentage must be > 0.0 and < 1.0, got %v", triggerPointPercentage)
		}
	}

	return &CreateAheadGate{
		triggerPointPercentages: triggerPointPercentages,
	}, nil
}

// a duplicate's zero id never lands on a trigger point
func (g *CreateAheadGate) shouldTriggerWithId(topicId int64, partitionSize int64, id int64) bool {
	atTriggerPoint := g.isTriggerPointId(partitionSize, id)
	if atTriggerPoint == -1 {
		return false
	}

	return g.tryToGetClaim(topicId, id/partitionSize, atTriggerPoint)
}

// an all-duplicates (0, 0) range never contains a trigger point
func (g *CreateAheadGate) shouldTriggerWithRange(topicId int64, partitionSize int64, firstId int64, lastId int64) bool {
	atTriggerPoint := g.isTriggerPointRange(partitionSize, firstId, lastId)
	if atTriggerPoint == -1 {
		return false
	}

	return g.tryToGetClaim(topicId, lastId/partitionSize, atTriggerPoint)
}

// Delete drops topicId's claim entry, keeping the map bounded by live topics.
func (g *CreateAheadGate) Delete(topicId int64) {
	g.data.Delete(topicId)
}

func (g *CreateAheadGate) isTriggerPointId(partitionSize int64, id int64) float64 {
	for _, triggerPointPercentage := range g.triggerPointPercentages {
		triggerPointId := getTriggerPointId(id, partitionSize, triggerPointPercentage)
		if id == triggerPointId {
			return triggerPointPercentage
		}
	}

	return -1
}

func (g *CreateAheadGate) isTriggerPointRange(partitionSize int64, firstId int64, lastId int64) float64 {
	for _, triggerPointPercentage := range g.triggerPointPercentages {
		triggerPointId := getTriggerPointId(lastId, partitionSize, triggerPointPercentage)
		if triggerPointId >= firstId && triggerPointId <= lastId {
			return triggerPointPercentage
		}
	}

	return -1
}

// monotonic, never reset -- a failed create stays claimed, the boundary heal
// covers it
func (g *CreateAheadGate) tryToGetClaim(topicId int64, partition int64, triggerPointPercentage float64) bool {
	claimId := createClaimId(partition, triggerPointPercentage)

	fresh := &atomic.Int64{}
	fresh.Store(-1) // seed below partition 0's first claim so it still wins

	value, _ := g.data.LoadOrStore(topicId, fresh)
	attempted := value.(*atomic.Int64)

	for {
		lastAttemptedId := attempted.Load()
		if claimId <= lastAttemptedId {
			return false
		}
		if attempted.CompareAndSwap(lastAttemptedId, claimId) {
			return true
		}
	}
}

// ***************
// *** HELPERS ***
// ***************

// percentages are validated by NewCreateAheadGate, so this cannot fail
func getTriggerPointId(id int64, partitionSize int64, triggerPointPercentage float64) int64 {
	partition := id / partitionSize
	start := partition * partitionSize
	offset := int64(float64(partitionSize) * triggerPointPercentage)
	return start + offset
}

// generates unique ordered (partition, triggerPointPercentage) id
// partition = 1, triggerPointPercentage = 0.85 => 185
func createClaimId(partition int64, triggerPointPercentage float64) int64 {
	return int64((float64(partition) + triggerPointPercentage) * 100)
}
