package cli

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/agentstax/vulkan/pkg/common/diagnostic"
	"github.com/agentstax/vulkan/pkg/migrate"
)

var errTestBroker = diagnostic.NewError("VK9801", diagnostic.Transient,
	"could not reach the test broker", "retry the produce call")

func TestRenderErrorBlockAlignsAllParts(t *testing.T) {
	raised := errTestBroker.With("topic", "orders", "version", 3).Wrap(errors.New("connection refused"))

	var builder strings.Builder
	renderErrorBlock(&builder, raised, "run `vulkan broker ping`")

	want := "error[VK9801]: could not reach the test broker\n" +
		"  topic:   \"orders\"\n" +
		"  version: 3\n" +
		"  cause:   connection refused\n" +
		"  retry:   safe -- an unchanged retry can succeed\n" +
		"  fix:     run `vulkan broker ping`\n" +
		"  docs:    https://vulkan-5ss.pages.dev/errors/VK9801\n"
	if builder.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", builder.String(), want)
	}
}

func TestRenderErrorBlockDropsAbsentParts(t *testing.T) {
	raised := migrate.ErrNotRegistered.With("topic", "__system.metrics")

	var builder strings.Builder
	renderErrorBlock(&builder, raised, "")

	want := "error[VK0017]: schema not registered\n" +
		"  topic: \"__system.metrics\"\n" +
		"  docs:  https://vulkan-5ss.pages.dev/errors/VK0017\n"
	if builder.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", builder.String(), want)
	}
}

func TestTranslateAdminErrorRewritesFixPerCode(t *testing.T) {
	translated := translateAdminError(fmt.Errorf("resolve topic: %w", migrate.ErrNotRegistered.With("topic", "__system.metrics")))

	ce, ok := translated.(*cliError)
	if !ok || ce.structured == nil {
		t.Fatalf("structured error not carried: %v", translated)
	}
	if ce.fix != "run `vulkan migrate init`" {
		t.Fatalf("fix not rewritten for the CLI: %q", ce.fix)
	}

	// a code with no CLI rewrite keeps the library fix
	translated = translateAdminError(errTestBroker.With("topic", "orders"))
	if ce, ok := translated.(*cliError); !ok || ce.fix != "retry the produce call" {
		t.Fatal("library fix not kept for a code without a CLI rewrite")
	}
}
