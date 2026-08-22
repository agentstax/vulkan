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

// jsonAttrValue maps a slog attr value to its json rendering: durations keep
// their units as strings instead of collapsing to nanosecond ints; everything
// else passes through natively.
func jsonAttrValue(value slog.Value) any {
	switch value.Kind() {
	case slog.KindDuration:
		return value.Duration().String()
	case slog.KindTime:
		return value.Time()
	default:
		return value.Any()
	}
}
