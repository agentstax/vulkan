package main

// systemregister is the go-forward dev bootstrap: it stands up the shared
// control-plane schema in Go (RegisterSystem), superseding the golang-migrate
// migrate-up path. Idempotent -- safe to re-run.

import (
	"context"
	"fmt"
	"os"

	"github.com/agentstax/vulkan/pkg/admin"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
)

func main() {
	ctx := context.Background()

	ds, err := iDatastore.NewPostgresDatastore(ctx, &iDatastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
	})
	must(err)
	defer ds.Close()

	mAdmin, err := admin.NewMessageAdmin(ds, nil)
	must(err)

	must(mAdmin.RegisterSystem(ctx, nil))
	fmt.Println("system schema registered")
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
