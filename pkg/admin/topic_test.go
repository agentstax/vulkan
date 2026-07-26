package admin

import (
	"context"
	"errors"
	"testing"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/topic"
)

func TestRegisterTopicRejectsReservedPrefix(t *testing.T) {
	a := &MessageAdmin{}

	_, err := a.RegisterTopic(context.Background(), common.SystemTopicPrefix+"foo", topic.SchemaVersion(1), nil)
	if !errors.Is(err, ErrReservedTopicName) {
		t.Fatalf("expected ErrReservedTopicName, got %v", err)
	}
}

func TestRenameTopicRejectsReservedPrefixBothDirections(t *testing.T) {
	a := &MessageAdmin{}

	if _, err := a.RenameTopic(context.Background(), common.SystemTopicPrefix+"foo", "user.topic"); !errors.Is(err, ErrReservedTopicName) {
		t.Fatalf("rename-of a reserved name: expected ErrReservedTopicName, got %v", err)
	}
	if _, err := a.RenameTopic(context.Background(), "user.topic", common.SystemTopicPrefix+"foo"); !errors.Is(err, ErrReservedTopicName) {
		t.Fatalf("rename-to a reserved name: expected ErrReservedTopicName, got %v", err)
	}
}

func TestDestroyTopicRejectsReservedPrefix(t *testing.T) {
	a := &MessageAdmin{allowDestroy: true}

	err := a.DestroyTopic(context.Background(), common.SystemTopicPrefix+"foo", topic.SchemaVersion(1), DestroyOptions{Force: true})
	if !errors.Is(err, ErrReservedTopicName) {
		t.Fatalf("expected ErrReservedTopicName, got %v", err)
	}
}

func TestIsReservedTopicName(t *testing.T) {
	cases := map[string]bool{
		"__system.metrics":   true,
		"__system.":          true,
		"__systemfoo":        false,
		"user.__system.evil": false,
		"orders.created":     false,
	}
	for name, want := range cases {
		if got := isReservedTopicName(name); got != want {
			t.Errorf("isReservedTopicName(%q) = %v, want %v", name, got, want)
		}
	}
}
