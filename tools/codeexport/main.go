package main

// code export: writes every VK-coded declaration as JSON for the doc site.
// The declarations are the source -- the site renders hand-written prose and
// reads this only for what a page cannot restate by hand: the diagnose
// queries, and the record each page's frontmatter is checked against.

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/agentstax/vulkan/pkg/common/diagnostic"

	// declaring packages, linked so the registry this binary sees is
	// complete. tools/conventions holds the walk that proves it.
	_ "github.com/agentstax/vulkan/pkg/alert"
	_ "github.com/agentstax/vulkan/pkg/common"
	_ "github.com/agentstax/vulkan/pkg/consume"
	_ "github.com/agentstax/vulkan/pkg/metrics"
	_ "github.com/agentstax/vulkan/pkg/migrate"
	_ "github.com/agentstax/vulkan/pkg/produce"
	_ "github.com/agentstax/vulkan/pkg/schedule"
	_ "github.com/agentstax/vulkan/pkg/system"
	_ "github.com/agentstax/vulkan/pkg/topic"
	_ "github.com/agentstax/vulkan/pkg/worker"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	out := flag.String("out", "", "file to write the JSON to; empty writes to stdout")
	flag.Parse()

	export, err := NewExport(diagnostic.Errors(), diagnostic.Events(), diagnostic.Metrics())
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')

	if *out == "" {
		_, err = os.Stdout.Write(encoded)
		return err
	}
	return os.WriteFile(*out, encoded, 0o644)
}
