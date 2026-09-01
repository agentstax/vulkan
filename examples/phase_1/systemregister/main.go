package main

// systemregister is the go-forward dev bootstrap: it stands up the shared
// control-plane tables in Go (RegisterSystem), superseding the golang-migrate
// migrate-up path. Idempotent -- safe to re-run.

import (
	"context"
	"fmt"
	"os"

	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
)

func main() {
	if err := run(); err != nil {
		fmt.Printf("\n❌ LAB FAILED: %s\n", err.Error())
		os.Exit(1)
	}
}

// labFailure is what die panics with; run recovers it into its error so
// main's deferred cleanup runs on a failed assertion.
type labFailure struct {
	message string
}

func (f labFailure) Error() string {
	return f.message
}

func run() (err error) {
	defer func() {
		switch recovered := recover().(type) {
		case nil:
		case labFailure:
			err = recovered
		default:
			panic(recovered)
		}
	}()
	ctx := context.Background()

	ds, err := iDatastore.NewPostgresDatastore(ctx, "example_user", "localhost", "example_db", &iDatastore.PostgresConnectionConfig{Pass: "example_password"})
	must(err)
	defer ds.Close()

	client, err := vulkan.NewClient(ds, nil)
	must(err)

	must(client.RegisterSystem(ctx, nil))
	fmt.Println("system registered")
	return nil
}

func must(err error) {
	if err != nil {
		die(err.Error())
	}
}

func die(msg string) {
	panic(labFailure{message: msg})
}
