// Package crosscut holds cross-cutting helpers shared by the HTTP and command
// layers: structured logging and a uniform JSON error representation.
package crosscut

import (
	"log/slog"
	"os"
	"strings"
)

// NewLogger builds a structured logger writing to stderr at the given level.
func NewLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: lvl}
	handler := slog.NewJSONHandler(os.Stderr, opts)
	return slog.New(handler)
}
