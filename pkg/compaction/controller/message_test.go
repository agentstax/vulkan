package controller

import (
	"testing"

	"github.com/agentstax/vulkan/pkg/common"
)

func TestListKeyMessagesRejectsNonPositiveLimit(t *testing.T) {
	controller := &CompactionController{}
	for _, limit := range []int{0, -1} {
		_, err := controller.ListKeyMessages[common.RawPayload](t.Context(), 41, "orders", limit)
		if err == nil {
			t.Errorf("limit %d did not return an error", limit)
		}
	}
}
