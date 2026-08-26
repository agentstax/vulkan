package logging

// The record pipeline under the Logger seam: every call becomes a record
// walked through the chain of stages the pipeline's config composes.

import (
	"context"
	"log/slog"
	"slices"
	"time"
)

// record is the unit the pipeline processes: one log call's facts, with
// the level as data rather than a method name.
type record struct {
	loggedAt time.Time
	level    slog.Level
	message  string
	args     []any
}

// handler is the internal processing seam: a stage handles the record
// and passes it on, possibly modified; returning nil stops the chain.
type handler interface {
	handle(ctx context.Context, record *record) *record
}

// PipelineLogger walks every log call as a record through the stages its
// Config composes, in one fixed order: capture -> enrich -> suppress ->
// drain -> sink.
//   - capture before enrich: held records skip the bound attributes the
//     emitted line already carries
//   - suppress before drain: a dropped repeat Error leaves the held
//     narration for the next emitted one
type PipelineLogger struct {
	Config *PipelineLoggerConfig

	chain []handler
}

// NewPipelineLogger composes the stages cfg declares around sink. A sink
// that is already a *PipelineLogger merges instead of nesting: Args
// concatenate onto its bound args, Buffer and Suppress stay on once on.
// cfg may be nil or a sparse struct; the zero config is a plain passthrough.
func NewPipelineLogger(sink Logger, cfg *PipelineLoggerConfig) *PipelineLogger {
	if cfg == nil {
		cfg = &PipelineLoggerConfig{}
	}

	var chain []handler
	chain = append(chain, newSinkHandler(sink))
	if cfg.Buffer {
		chain = append(chain, newDrainHandler())
	}
	if cfg.Suppress {
		chain = append(chain, newSuppressHandler())
	}
	if len(cfg.Args) > 0 {
		chain = append(chain, newEnrichHandler(cfg.Args))
	}
	if cfg.Buffer {
		chain = append(chain, newCaptureHandler())
	}

	existing, existingFound := sink.(*PipelineLogger)
	if existingFound {
		// the chain is rebuilt below from the merged config
		cfg = &PipelineLoggerConfig{
			Args:     slices.Concat(existing.Config.Args, cfg.Args),
			Buffer:   existing.Config.Buffer || cfg.Buffer,
			Suppress: existing.Config.Suppress || cfg.Suppress,
		}

		// the existing pipeline's sink stage holds the real underlying
		// logger -- the fresh one wraps the pipeline itself
		sinkStage := getHandlerFromChain[*sinkHandler](existing.chain)

		// the existing suppress stage carries the window state
		suppressStage := getHandlerFromChain[*suppressHandler](existing.chain)
		if suppressStage == nil {
			suppressStage = getHandlerFromChain[*suppressHandler](chain)
		}

		drainStage := getHandlerFromChain[*drainHandler](existing.chain)
		if drainStage == nil {
			drainStage = getHandlerFromChain[*drainHandler](chain)
		}

		captureStage := getHandlerFromChain[*captureHandler](existing.chain)
		if captureStage == nil {
			captureStage = getHandlerFromChain[*captureHandler](chain)
		}

		// enrich is rebuilt from the concatenated args -- neither
		// side's stage holds them
		chain = nil
		chain = append(chain, sinkStage)
		if cfg.Buffer {
			chain = append(chain, drainStage)
		}
		if cfg.Suppress {
			chain = append(chain, suppressStage)
		}
		if len(cfg.Args) > 0 {
			chain = append(chain, newEnrichHandler(cfg.Args))
		}
		if cfg.Buffer {
			chain = append(chain, captureStage)
		}
	}

	return &PipelineLogger{Config: cfg, chain: chain}
}

func (l *PipelineLogger) DebugContext(ctx context.Context, message string, args ...any) {
	l.handleChain(ctx, &record{loggedAt: time.Now(), level: slog.LevelDebug, message: message, args: args})
}

func (l *PipelineLogger) InfoContext(ctx context.Context, message string, args ...any) {
	l.handleChain(ctx, &record{loggedAt: time.Now(), level: slog.LevelInfo, message: message, args: args})
}

func (l *PipelineLogger) WarnContext(ctx context.Context, message string, args ...any) {
	l.handleChain(ctx, &record{loggedAt: time.Now(), level: slog.LevelWarn, message: message, args: args})
}

func (l *PipelineLogger) ErrorContext(ctx context.Context, message string, args ...any) {
	l.handleChain(ctx, &record{loggedAt: time.Now(), level: slog.LevelError, message: message, args: args})
}

// The chain appends sink-first, so the walk runs backward: capture ->
// enrich -> suppress -> drain -> sink.
func (l *PipelineLogger) handleChain(ctx context.Context, record *record) {
	for i := len(l.chain) - 1; i >= 0; i-- {
		record = l.chain[i].handle(ctx, record)
		if record == nil {
			return
		}
	}
}

// getHandlerFromChain returns the chain's stage of type T -- nil when
// the chain composes none.
func getHandlerFromChain[T handler](chain []handler) T {
	for _, stage := range chain {
		if foundHandler, ok := stage.(T); ok {
			return foundHandler
		}
	}
	var none T
	return none
}
