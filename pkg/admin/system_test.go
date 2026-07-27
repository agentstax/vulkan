package admin

import (
	"context"
	"testing"
)

// AlterSystem validates the patch before touching the datastore, so an empty
// patch fails fast (mirrors AlterConfig's at-least-one-field rule) without a DB.
func TestAlterSystemRejectsEmptyPatch(t *testing.T) {
	a := &MessageAdmin{}

	_, err := a.AlterSystem(context.Background(), nil)
	if err == nil {
		t.Fatal("empty patch: want error, got nil")
	}
}
