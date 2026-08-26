package logging

// PipelineLoggerConfig declares the stages a PipelineLogger composes.
// Optional fields only -- the zero config is a plain passthrough to the
// sink.
type PipelineLoggerConfig struct {
	// Args are bound onto every emitted line.
	Args []any

	// Buffer opts into WithLogBuffer operation boundaries: records below
	// Error are held in the operation's ring, and the operation's first
	// Error drains them into its "preceding" attribute.
	Buffer bool

	// Suppress collapses repeats of one (level, message) Warn/Error line
	// inside the suppression window to the first line, with the dropped
	// total as suppressed_count on the next emission.
	Suppress bool
}
