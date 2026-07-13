// Package logger holds the project's default logger construction. There is
// deliberately NO logger interface defined here -- the pluggable seam is
// *slog.Logger itself. slog split its API into a frontend (Logger, what
// log-emitting code calls) and a backend interface (Handler, what callers
// swap to change where/how logs land), so accepting the concrete *slog.Logger
// already gives callers full control: any zap/zerolog/logr backend plugs in
// as a Handler without this package knowing.
package logger

import (
	"io"
	"log/slog"
)

// Overriding with a custom/company logger means implementing slog.Handler
// (or importing the bridge the logging library already ships) and wrapping
// it in slog.New:
//
//	type AcmeHandler struct{ l *acmelog.Logger }
//
//	func (h *AcmeHandler) Enabled(_ context.Context, lvl slog.Level) bool {
//		return h.l.LevelEnabled(toAcmeLevel(lvl))
//	}
//
//	func (h *AcmeHandler) Handle(_ context.Context, r slog.Record) error {
//		fields := make(map[string]any, r.NumAttrs())
//		r.Attrs(func(a slog.Attr) bool { fields[a.Key] = a.Value.Any(); return true })
//		h.l.Emit(toAcmeLevel(r.Level), r.Message, fields)
//		return nil
//	}
//	// WithAttrs/WithGroup: return a copy carrying the pre-bound fields/prefix
//
//	consumer.Logger = slog.New(&AcmeHandler{l: acmeLogger})
//
// Most callers never write that adapter -- the popular backends already have
// one: zap (zapslog.NewHandler), zerolog (slog-zerolog), logr
// (logr.ToSlogHandler). Silence entirely with slog.New(slog.DiscardHandler).

// NewLogger is the no-opinions default: human-readable text lines to w, warn level
// and up -- quiet by default without being silent, so a caller who never
// thinks about logging still hears about real problems.
func NewLogger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelWarn}))
}
