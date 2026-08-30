package middleware

import (
	"context"
	"net/http"
	"regexp"
	"strings"
)

const (
	TraceparentHeader            = "traceparent"
	traceIDKey        contextKey = "trace_id"
)

var traceparentRegex = regexp.MustCompile(`^[0-9a-fA-F]{2}-([0-9a-fA-F]{32})-[0-9a-fA-F]{16}-[0-9a-fA-F]{2}$`)

// TraceContext extracts W3C traceparent headers and attaches the trace_id to the request context.
func TraceContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tp := strings.TrimSpace(r.Header.Get(TraceparentHeader))
		traceID := extractTraceID(tp)

		if traceID != "" {
			ctx := context.WithValue(r.Context(), traceIDKey, traceID)
			r = r.WithContext(ctx)
		}

		next.ServeHTTP(w, r)
	})
}

// GetTraceID retrieves the trace ID from the context, or returns "" if none was set.
func GetTraceID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(traceIDKey).(string); ok {
		return v
	}
	return ""
}

func extractTraceID(traceparent string) string {
	if traceparent == "" {
		return ""
	}
	matches := traceparentRegex.FindStringSubmatch(traceparent)
	if len(matches) == 2 {
		tid := strings.ToLower(matches[1])
		if tid != "00000000000000000000000000000000" {
			return tid
		}
	}
	return ""
}
