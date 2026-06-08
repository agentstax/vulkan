package main

import (
	"math/rand/v2"
	"time"
)

// just a fake step right now, ideally could do anything
func fetch(url string) (string, error) {
	// simulate async blocking work
	time.Sleep(time.Duration(rand.IntN(5000)) * time.Millisecond)

	return url, nil
}
