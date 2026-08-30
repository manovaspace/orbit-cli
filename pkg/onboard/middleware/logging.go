package middleware

import (
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
)

// responseWriter wraps http.ResponseWriter to capture HTTP status code and written byte count.
type responseWriter struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int64
	wroteHeader  bool
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{
		ResponseWriter: w,
		statusCode:   http.StatusOK,
		bytesWritten: 0,
		wroteHeader:  false,
	}
}

func (rw *responseWriter) WriteHeader(code int) {
	if !rw.wroteHeader {
		rw.statusCode = code
		rw.wroteHeader = true
		rw.ResponseWriter.WriteHeader(code)
	}
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	n, err := rw.ResponseWriter.Write(b)
	rw.bytesWritten += int64(n)
	return n, err
}

func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// Logging returns a middleware that logs HTTP requests with structured JSON via log/slog.
func Logging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			l := logger
			if l == nil {
				l = slog.Default()
			}

			start := time.Now()
			rw := newResponseWriter(w)

			next.ServeHTTP(rw, r)

			durationMs := float64(time.Since(start).Microseconds()) / 1000.0

			reqID := GetRequestID(r.Context())
			if reqID == "" {
				reqID = r.Header.Get(RequestIDHeader)
			}
			traceID := GetTraceID(r.Context())
			clientIP := extractClientIP(r)

			attrs := []any{
				slog.String("request_id", reqID),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rw.statusCode),
				slog.Float64("duration_ms", durationMs),
				slog.Int64("bytes_written", rw.bytesWritten),
				slog.String("client_ip", clientIP),
				slog.String("user_agent", r.UserAgent()),
			}

			if traceID != "" {
				attrs = append(attrs, slog.String("trace_id", traceID))
			}

			ctx := r.Context()
			if rw.statusCode >= 500 {
				l.ErrorContext(ctx, "HTTP request", attrs...)
			} else if rw.statusCode >= 400 {
				l.WarnContext(ctx, "HTTP request", attrs...)
			} else {
				l.InfoContext(ctx, "HTTP request", attrs...)
			}
		})
	}
}

func extractClientIP(r *http.Request) string {
	if cfIP := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); cfIP != "" {
		return cfIP
	}
	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 && strings.TrimSpace(parts[0]) != "" {
			return strings.TrimSpace(parts[0])
		}
	}
	if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
		return xri
	}
	if r.RemoteAddr != "" {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err == nil {
			return host
		}
		return r.RemoteAddr
	}
	return ""
}
