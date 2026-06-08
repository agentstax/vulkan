package main

import (
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/agentstax/vulkan/pkg/workflow"
	"golang.org/x/sync/semaphore"
)

func getHello(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("got /hello request\n")
	io.WriteString(w, "Hello, HTTP!\n")
}

// infinite loop ticker to feed data to queue
func feedQueue(queue chan<- string) {
	ticker := time.NewTicker(75 * time.Millisecond)

	go func() {
		for {
			t := <-ticker.C
			for i := range rand.IntN(10) {
				queue <- t.String() + " " + strconv.Itoa(i)
			}
		}
	}()
}

func main() {

	// mux := http.NewServeMux()

	// mux.HandleFunc("/hello", getHello)

	// mux.Handle("/scrape", workflow.ToHandler(&ScrapeWorkflow{}))

	// err := http.ListenAndServe(":3333", mux)

	// if errors.Is(err, http.ErrServerClosed) {
	// 	fmt.Printf("server closed\n")
	// } else if err != nil {
	// 	fmt.Printf("error starting server: %s\n", err)
	// 	os.Exit(1)
	// }

	ctx := &workflow.Context{}
	workflow := &ScrapeWorkflow{}

	const concurrencyLimit = 30

	jobQueue := make(chan string, concurrencyLimit*10)
	workflowSem := semaphore.NewWeighted(concurrencyLimit)

	feedQueue(jobQueue)

	var wg sync.WaitGroup
	wg.Add(1)

	// queue processor
	go func() {
		for {
			permit := workflowSem.TryAcquire(1)
			if !permit {
				continue
			}

			timeString := <-jobQueue

			// gothread for work
			go func() {
				defer workflowSem.Release(1)

				workflow.Run(ctx, &ScrapeInput{
					URL: timeString,
				})
			}()
		}
	}()

	// infinite wait - block main thread forever
	wg.Wait()
}
