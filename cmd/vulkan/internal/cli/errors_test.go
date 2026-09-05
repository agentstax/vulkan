package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/agentstax/vulkan/pkg/common/diagnostic"
	"github.com/agentstax/vulkan/pkg/migrate"
)

var errTestBroker = diagnostic.NewDiagnosticError("VK9801", diagnostic.RecoveryTransient,
	"could not reach the test broker", "retry the produce call")

var metricTestDepth = diagnostic.NewDiagnosticMetric("VK9802",
	"vulkan.test.queue_depth", "gauge", "{message}", "test queue depth", diagnostic.MetricScopeConsumerGroup, "topic", "group")

func TestRenderMetricBlockAlignsAllParts(t *testing.T) {
	var builder strings.Builder
	renderMetricBlock(&builder, metricTestDepth)

	want := "metric[VK9802]: vulkan.test.queue_depth\n" +
		"  kind:           gauge\n" +
		"  unit:           {message}\n" +
		"  scope:          consumer_group\n" +
		"  attribute keys: topic, group\n" +
		"  description:    test queue depth\n" +
		"  docs:           https://vulkan-5ss.pages.dev/errors/VK9802\n"
	if builder.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", builder.String(), want)
	}
}

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

	want := "error[VK0017]: system not registered\n" +
		"  topic: \"__system.metrics\"\n" +
		"  docs:  https://vulkan-5ss.pages.dev/errors/VK0017\n"
	if builder.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", builder.String(), want)
	}
}

func TestErrorHandlerJSONStructured(t *testing.T) {
	raised := errTestBroker.With("topic", "orders", "version", 3).Wrap(errors.New("connection refused"))

	var builder strings.Builder
	errorHandler(&builder, &globalFlags{output: "json"}, failStructured(raised, "run `vulkan broker ping`"))

	var document errorDocument
	if err := json.Unmarshal([]byte(builder.String()), &document); err != nil {
		t.Fatalf("output is not one json document: %v\n%s", err, builder.String())
	}
	object := document.Error
	if object.Code != "VK9801" || object.Problem != "could not reach the test broker" || object.Recovery != "transient" {
		t.Fatalf("wrong parts: %+v", object)
	}
	if object.Values["topic"] != "orders" || object.Values["version"] != float64(3) {
		t.Fatalf("wrong values: %+v", object.Values)
	}
	if object.Cause != "connection refused" || object.Fix != "run `vulkan broker ping`" {
		t.Fatalf("wrong cause/fix: %+v", object)
	}
	if object.Docs != "https://vulkan-5ss.pages.dev/errors/VK9801" {
		t.Fatalf("wrong docs: %q", object.Docs)
	}
}

func TestErrorHandlerJSONPlainCarriesOnlyProblem(t *testing.T) {
	var builder strings.Builder
	errorHandler(&builder, &globalFlags{output: "json"}, failOp("topic %q not found", "orders"))

	var decoded map[string]map[string]any
	if err := json.Unmarshal([]byte(builder.String()), &decoded); err != nil {
		t.Fatalf("output is not one json document: %v\n%s", err, builder.String())
	}
	object := decoded["error"]
	if object["problem"] != `topic "orders" not found` {
		t.Fatalf("wrong problem: %v", object)
	}
	if len(object) != 1 {
		t.Fatalf("a plain error carries only problem, got: %v", object)
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
