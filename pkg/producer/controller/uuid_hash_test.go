package controller

import (
	"testing"
	"uuid"
)

// the RFC 9562 v5 example vector: SHA-1 over the DNS namespace and
// "www.example.com" -- proves the vendored construction matches upstream.
func TestNewSHA1UUID(t *testing.T) {
	namespaceDNS := uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")

	got := newSHA1UUID(namespaceDNS, []byte("www.example.com"))
	if want := uuid.MustParse("2ed6657d-e927-568b-95e1-2665a8aea6a2"); got != want {
		t.Errorf("newSHA1UUID = %s, want %s", got, want)
	}
	if got != newSHA1UUID(namespaceDNS, []byte("www.example.com")) {
		t.Error("same namespace and data must produce the same UUID")
	}
}
