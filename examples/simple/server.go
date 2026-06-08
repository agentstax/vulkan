package main

import (
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"time"
)

func getHello(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("got /hello request\n")
	io.WriteString(w, "Hello, HTTP!\n")
}

// infinite loop ticker to feed data to queue
func feedQueue(queue chan<- string) {
	ticker := time.NewTicker(250 * time.Millisecond)

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

	const concurrencyLimit = 5
	jobQueue := make(chan string, concurrencyLimit)

	feedQueue(jobQueue)

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		for {
			timeString := <-jobQueue
			fmt.Println(timeString)
		}
	}()

	wg.Wait()
}
