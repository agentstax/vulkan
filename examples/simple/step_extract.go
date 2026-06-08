package main

import (
	"math/rand/v2"
	"time"
)

func extract(html string) (string, error) {
	// simulate async blocking work
	time.Sleep(time.Duration(rand.IntN(2500)) * time.Millisecond)

	return html, nil
}
