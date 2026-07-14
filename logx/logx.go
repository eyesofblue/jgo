// Package logx provides structured, context-aware logging backed by slog.
package logx

import (
	"context"
	"log/slog"

	"github.com/eyesofblue/jgo/middleware/traceid"
)

// Logger wraps slog.Logger with context-aware method names and automatic trace
// correlation fields.
type Logger struct {
	logger *slog.Logger
}

// New wraps logger. A nil logger uses slog.Default.
func New(logger *slog.Logger) *Logger {
	if logger == nil {
		logger = slog.Default()
	}
	return &Logger{logger: logger}
}

// Slog returns the wrapped standard-library logger.
func (l *Logger) Slog() *slog.Logger {
	if l == nil || l.logger == nil {
		return slog.Default()
	}
	return l.logger
}

// With returns a child logger with the supplied structured attributes.
func (l *Logger) With(args ...any) *Logger {
	return New(l.Slog().With(args...))
}

func (l *Logger) DebugCtx(ctx context.Context, msg string, args ...any) {
	l.log(ctx, slog.LevelDebug, msg, args...)
}

func (l *Logger) InfoCtx(ctx context.Context, msg string, args ...any) {
	l.log(ctx, slog.LevelInfo, msg, args...)
}

func (l *Logger) WarnCtx(ctx context.Context, msg string, args ...any) {
	l.log(ctx, slog.LevelWarn, msg, args...)
}

func (l *Logger) ErrorCtx(ctx context.Context, msg string, args ...any) {
	l.log(ctx, slog.LevelError, msg, args...)
}

func (l *Logger) log(ctx context.Context, level slog.Level, msg string, args ...any) {
	if ctx == nil {
		ctx = context.Background()
	}
	args = withTraceFields(ctx, args)
	l.Slog().Log(ctx, level, msg, args...)
}

func DebugCtx(ctx context.Context, msg string, args ...any) {
	New(nil).DebugCtx(ctx, msg, args...)
}

func InfoCtx(ctx context.Context, msg string, args ...any) {
	New(nil).InfoCtx(ctx, msg, args...)
}

func WarnCtx(ctx context.Context, msg string, args ...any) {
	New(nil).WarnCtx(ctx, msg, args...)
}

func ErrorCtx(ctx context.Context, msg string, args ...any) {
	New(nil).ErrorCtx(ctx, msg, args...)
}

func withTraceFields(ctx context.Context, args []any) []any {
	traceID := traceid.FromContext(ctx)
	spanID := traceid.SpanIDFromContext(ctx)
	if traceID == "" && spanID == "" {
		return args
	}
	result := make([]any, 0, len(args)+4)
	result = append(result, args...)
	if traceID != "" && !hasKey(args, "trace_id") {
		result = append(result, "trace_id", traceID)
	}
	if spanID != "" && !hasKey(args, "span_id") {
		result = append(result, "span_id", spanID)
	}
	return result
}

func hasKey(args []any, key string) bool {
	for index := 0; index < len(args); {
		if attribute, ok := args[index].(slog.Attr); ok && attribute.Key == key {
			return true
		}
		if _, ok := args[index].(slog.Attr); ok {
			index++
			continue
		}
		if name, ok := args[index].(string); ok && index+1 < len(args) {
			if name == key {
				return true
			}
			index += 2
			continue
		}
		index++
	}
	return false
}
