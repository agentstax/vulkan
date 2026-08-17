package concurrency

import "testing"

func TestPermitRefusesWhileHeldAndReadmitsAfterRelease(t *testing.T) {
	permit, err := NewPermit()
	if err != nil {
		t.Fatalf("NewPermit: %v", err)
	}

	release, ok := permit.Acquire()
	if !ok {
		t.Fatal("first Acquire refused a free permit")
	}
	if _, ok := permit.Acquire(); ok {
		t.Fatal("second Acquire succeeded while held")
	}

	release()
	if _, ok := permit.Acquire(); !ok {
		t.Fatal("Acquire refused after release")
	}
}
