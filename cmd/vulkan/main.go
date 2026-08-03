package main

import (
	"context"
	"os"

	"github.com/agentstax/vulkan/cmd/vulkan/internal/cli"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(cli.Execute(context.Background(), version))
}
