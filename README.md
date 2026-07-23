https://excalidraw.com/#json=xIyANXvjoo9OLRQn1QTJH,FS1ktN84YCRGl2KB0YsbFw

## Development

The `cmd/vulkan` CLI is a nested module (its own `go.mod`) so its dependencies
stay out of the core library's module graph. Building it against your local
library checkout needs a Go workspace, which is gitignored — create it once:

```sh
go work init . ./cmd/vulkan
```

This writes a `go.work` linking the root module and the CLI module, so
`go build` / `go test` / `go run` resolve the CLI's import of the library to
your working tree instead of a published version.
