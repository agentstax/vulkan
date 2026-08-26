package cli

import (
	"encoding/json"
	"io"
	"log/slog"
)

// writeJSON renders one value as the command's single output document:
// two-space indented, trailing newline.
func writeJSON(w io.Writer, document any) {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(document)
}

// jsonAttributeValue maps a slog attribute value to its json rendering:
// durations keep their units as strings instead of collapsing to nanosecond
// ints; everything else passes through natively.
func jsonAttributeValue(value slog.Value) any {
	switch value.Kind() {
	case slog.KindDuration:
		return value.Duration().String()
	case slog.KindTime:
		return value.Time()
	default:
		return value.Any()
	}
}
