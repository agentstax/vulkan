package main

import (
	"github.com/agentstax/vulkan/pkg/step"
	"github.com/agentstax/vulkan/pkg/workflow"
)

var workflowConfig = &workflow.Config{
	Concurrency: 4,
}

type ScrapeInput struct {
	URL string `json:"url"`
}

type ScrapeOutput struct {
	HTML string `json:"html"`
}

type ScrapeWorkflow struct{}

func (w *ScrapeWorkflow) Run(ctx *workflow.Context, input ScrapeInput) (*ScrapeOutput, error) {
	// Step 1: fetch url
	result, err := step.Run(func() (string, error) {
		return fetch(input.URL)
	})
	if err != nil {
		return &ScrapeOutput{}, err
	}

	// Step 2: extract data

	return &ScrapeOutput{
		HTML: result,
	}, nil
}
