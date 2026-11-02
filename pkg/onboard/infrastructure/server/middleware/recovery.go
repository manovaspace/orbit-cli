package middleware

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
)

type recoveryResponse struct {
	Error string `json:"error"`
	Code  int    `json:"code"`
}

// Recovery returns a middleware that recovers from panics in downstream handlers,
// logs the error and stack trace at ERROR level, and responds with a 500 JSON response.
func Recovery(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					l := logger
					if l == nil {
						l = slog.Default()
					}

					stack := string(debug.Stack())
					reqID := GetRequestID(r.Context())
					if reqID == "" {
						reqID = r.Header.Get(RequestIDHeader)
					}
					traceID := GetTraceID(r.Context())

					attrs := []any{
						slog.Any("panic", rec),
						slog.String("stack", stack),
						slog.String("method", r.Method),
						slog.String("path", r.URL.Path),
					}
					if reqID != "" {
						attrs = append(attrs, slog.String("request_id", reqID))
					}
					if traceID != "" {
						attrs = append(attrs, slog.String("trace_id", traceID))
					}

					l.ErrorContext(r.Context(), fmt.Sprintf("panic recovered: %v", rec), attrs...)

					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					_ = json.NewEncoder(w).Encode(recoveryResponse{
						Error: "internal server error",
						Code:  http.StatusInternalServerError,
					})
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}
