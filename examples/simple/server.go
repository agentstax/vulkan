package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/agentstax/vulkan/pkg/workflow"
)

func getHello(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("got /hello request\n")
	io.WriteString(w, "Hello, HTTP!\n")
}

func main() {

	mux := http.NewServeMux()

	mux.HandleFunc("/hello", getHello)

	mux.Handle("/scrape", workflow.ToHandler(&ScrapeWorkflow{}))

	err := http.ListenAndServe(":3333", mux)

	if errors.Is(err, http.ErrServerClosed) {
		fmt.Printf("server closed\n")
	} else if err != nil {
		fmt.Printf("error starting server: %s\n", err)
		os.Exit(1)
	}
}
