package main

import (
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/agentstax/vulkan/pkg/concurrency"
	"github.com/agentstax/vulkan/pkg/workflow"
	"github.com/google/uuid"
)

func getHello(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("got /hello request\n")
	io.WriteString(w, "Hello, HTTP!\n")
}

// infinite loop ticker to feed work to queue, 'simulate ingestion'
func feedQueue(queue concurrency.Queue[string]) {
	ticker := time.NewTicker(75 * time.Millisecond)

	go func() {
		for {
			t := <-ticker.C
			for i := range rand.IntN(10) {

				go func() {
					if queue.CanEnQueue() {
						err := queue.EnQueue(t.String() + " " + strconv.Itoa(i))
						if err != nil {
							fmt.Println(err.Error())
						}
					}
				}()
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

	const concurrencyLimit = 300

	workQueue, err := concurrency.NewPressureQueue[string](concurrencyLimit * 10)
	if err != nil {
		os.Exit(1)
	}

	workerPoolLimiter, err := concurrency.NewWorkerPoolLimiter(concurrencyLimit)
	if err != nil {
		os.Exit(1)
	}
	// workflowSem := semaphore.NewWeighted(concurrencyLimit)

	feedQueue(workQueue)

	// infinite wait - block main thread forever via Wait at end
	var wg sync.WaitGroup
	wg.Add(1)

	// queue processor
	go func() {
		for {
			threadId, err := uuid.NewV7()
			if err != nil {
				return // something is very wrong if this happens
			}

			// blocking - waits till can get permit
			err = workerPoolLimiter.AcquirePermit(ctx.ToContext(), threadId.String())
			if err != nil {
				return // context is likely cancel or shutdown in this case
			}

			// blocking - waits till can dequeue
			work, err := workQueue.DeQueue()
			if err != nil {
				workerPoolLimiter.ReleasePermit(ctx.ToContext(), threadId.String())
				continue
			}

			// dispatch gothread for work, in flight work limited by concurrency pool limit
			go func() {
				defer workerPoolLimiter.ReleasePermit(ctx.ToContext(), threadId.String())

				workflow.Run(ctx, &ScrapeInput{
					URL: work,
				})
			}()
		}
	}()

	// infinite wait - block main thread forever
	wg.Wait()
}
