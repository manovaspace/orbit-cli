package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"time"
)

type contextKey string

const (
	RequestIDHeader            = "X-Request-ID"
	requestIDKey    contextKey = "request_id"
)

// RequestID returns a middleware that ensures every HTTP request has a unique X-Request-ID.
// If the incoming request has an X-Request-ID header, it is preserved.
// Otherwise, a cryptographically secure random 16-byte hex string (32 characters) is generated.
// The request ID is added to the response headers and injected into the request context.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := strings.TrimSpace(r.Header.Get(RequestIDHeader))
		if reqID == "" {
			reqID = generateRequestID()
		}

		w.Header().Set(RequestIDHeader, reqID)
		ctx := context.WithValue(r.Context(), requestIDKey, reqID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetRequestID retrieves the request ID from the context, or returns "" if not found.
func GetRequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}

func generateRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "req-" + hex.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(b)
}
