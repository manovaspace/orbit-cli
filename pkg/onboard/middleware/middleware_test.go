package middleware_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/onboard/middleware"
)

func TestRequestID(t *testing.T) {
	t.Run("Generate_When_Absent", func(t *testing.T) {
		var capturedCtxReqID string
		handler := middleware.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedCtxReqID = middleware.GetRequestID(r.Context())
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		headerReqID := rec.Header().Get("X-Request-ID")
		if headerReqID == "" {
			t.Fatal("expected X-Request-ID response header to be set")
		}
		if len(headerReqID) != 32 {
			t.Errorf("expected 32 hex char request ID, got %q (len %d)", headerReqID, len(headerReqID))
		}
		if capturedCtxReqID != headerReqID {
			t.Errorf("expected context request ID %q to match header %q", capturedCtxReqID, headerReqID)
		}
	})

	t.Run("Preserve_When_Present", func(t *testing.T) {
		customID := "custom-req-id-abcdef-12345"
		var capturedCtxReqID string
		handler := middleware.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedCtxReqID = middleware.GetRequestID(r.Context())
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-Request-ID", customID)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		headerReqID := rec.Header().Get("X-Request-ID")
		if headerReqID != customID {
			t.Errorf("expected preserved header %q, got %q", customID, headerReqID)
		}
		if capturedCtxReqID != customID {
			t.Errorf("expected context request ID %q, got %q", customID, capturedCtxReqID)
		}
	})

	t.Run("GetRequestID_Nil_Context", func(t *testing.T) {
		if id := middleware.GetRequestID(nil); id != "" {
			t.Errorf("expected empty string for nil context, got %q", id)
		}
		if id := middleware.GetRequestID(context.Background()); id != "" {
			t.Errorf("expected empty string for background context, got %q", id)
		}
	})
}

func TestTraceContext(t *testing.T) {
	t.Run("Valid_W3C_Traceparent", func(t *testing.T) {
		traceparent := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
		expectedTraceID := "4bf92f3577b34da6a3ce929d0e0e4736"

		var capturedTraceID string
		handler := middleware.TraceContext(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedTraceID = middleware.GetTraceID(r.Context())
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/trace", nil)
		req.Header.Set("traceparent", traceparent)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if capturedTraceID != expectedTraceID {
			t.Errorf("expected trace ID %q, got %q", expectedTraceID, capturedTraceID)
		}
	})

	t.Run("Invalid_Traceparent", func(t *testing.T) {
		invalidCases := []string{
			"invalid-traceparent",
			"00-00000000000000000000000000000000-00f067aa0ba902b7-01",
			"00-short-00f067aa0ba902b7-01",
			"",
		}

		for _, tc := range invalidCases {
			var capturedTraceID string
			handler := middleware.TraceContext(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedTraceID = middleware.GetTraceID(r.Context())
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest("GET", "/trace", nil)
			if tc != "" {
				req.Header.Set("traceparent", tc)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if capturedTraceID != "" {
				t.Errorf("for input %q: expected empty trace ID, got %q", tc, capturedTraceID)
			}
		}
	})

	t.Run("GetTraceID_Nil_Context", func(t *testing.T) {
		if id := middleware.GetTraceID(nil); id != "" {
			t.Errorf("expected empty string for nil context, got %q", id)
		}
	})
}

func TestLogging(t *testing.T) {
	t.Run("Status_200_INFO_Level", func(t *testing.T) {
		var logBuf bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

		handler := middleware.RequestID(
			middleware.TraceContext(
				middleware.Logging(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "text/plain")
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("hello world"))
				})),
			),
		)

		req := httptest.NewRequest("GET", "/api/test", nil)
		req.Header.Set("User-Agent", "TestClient/1.0")
		req.Header.Set("CF-Connecting-IP", "203.0.113.10")
		req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		var entry map[string]interface{}
		if err := json.Unmarshal(logBuf.Bytes(), &entry); err != nil {
			t.Fatalf("failed to decode log JSON: %v, raw: %s", err, logBuf.String())
		}

		if entry["level"] != "INFO" {
			t.Errorf("expected level INFO, got %v", entry["level"])
		}
		if entry["method"] != "GET" {
			t.Errorf("expected method GET, got %v", entry["method"])
		}
		if entry["path"] != "/api/test" {
			t.Errorf("expected path /api/test, got %v", entry["path"])
		}
		if entry["status"] != float64(200) {
			t.Errorf("expected status 200, got %v", entry["status"])
		}
		if entry["client_ip"] != "203.0.113.10" {
			t.Errorf("expected client_ip 203.0.113.10, got %v", entry["client_ip"])
		}
		if entry["user_agent"] != "TestClient/1.0" {
			t.Errorf("expected user_agent TestClient/1.0, got %v", entry["user_agent"])
		}
		if entry["bytes_written"] != float64(11) {
			t.Errorf("expected bytes_written 11, got %v", entry["bytes_written"])
		}
		if entry["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" {
			t.Errorf("expected trace_id 4bf92f3577b34da6a3ce929d0e0e4736, got %v", entry["trace_id"])
		}
		if reqID, ok := entry["request_id"].(string); !ok || reqID == "" {
			t.Errorf("expected non-empty request_id in log, got %v", entry["request_id"])
		}
	})

	t.Run("Status_404_WARN_Level", func(t *testing.T) {
		var logBuf bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

		handler := middleware.Logging(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("not found"))
		}))

		req := httptest.NewRequest("GET", "/missing", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		var entry map[string]interface{}
		if err := json.Unmarshal(logBuf.Bytes(), &entry); err != nil {
			t.Fatalf("failed to decode log JSON: %v", err)
		}

		if entry["level"] != "WARN" {
			t.Errorf("expected level WARN for 404, got %v", entry["level"])
		}
		if entry["status"] != float64(404) {
			t.Errorf("expected status 404, got %v", entry["status"])
		}
	})

	t.Run("Status_500_ERROR_Level", func(t *testing.T) {
		var logBuf bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

		handler := middleware.Logging(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("server error"))
		}))

		req := httptest.NewRequest("POST", "/fail", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		var entry map[string]interface{}
		if err := json.Unmarshal(logBuf.Bytes(), &entry); err != nil {
			t.Fatalf("failed to decode log JSON: %v", err)
		}

		if entry["level"] != "ERROR" {
			t.Errorf("expected level ERROR for 500, got %v", entry["level"])
		}
		if entry["status"] != float64(500) {
			t.Errorf("expected status 500, got %v", entry["status"])
		}
	})

	t.Run("Implicit_200_Status", func(t *testing.T) {
		var logBuf bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&logBuf, nil))

		handler := middleware.Logging(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("implicit 200"))
		}))

		req := httptest.NewRequest("GET", "/implicit", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		var entry map[string]interface{}
		if err := json.Unmarshal(logBuf.Bytes(), &entry); err != nil {
			t.Fatalf("failed to decode log JSON: %v", err)
		}

		if entry["status"] != float64(200) {
			t.Errorf("expected status 200, got %v", entry["status"])
		}
	})

	t.Run("Nil_Logger_Fallback", func(t *testing.T) {
		handler := middleware.Logging(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/nil-logger", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rec.Code)
		}
	})
}

func TestRecovery(t *testing.T) {
	t.Run("Recovers_From_Panic_And_Logs_Stack", func(t *testing.T) {
		var logBuf bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&logBuf, nil))

		handler := middleware.RequestID(
			middleware.Recovery(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				panic("something unexpected crashed!")
			})),
		)

		req := httptest.NewRequest("GET", "/panic", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 Internal Server Error, got %d", rec.Code)
		}

		var errResp map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
			t.Fatalf("failed to decode 500 error response JSON: %v", err)
		}
		if errResp["error"] != "internal server error" || errResp["code"] != float64(500) {
			t.Errorf("unexpected error payload: %+v", errResp)
		}

		var logEntry map[string]interface{}
		if err := json.Unmarshal(logBuf.Bytes(), &logEntry); err != nil {
			t.Fatalf("failed to parse recovery slog JSON: %v, raw: %s", err, logBuf.String())
		}

		if logEntry["level"] != "ERROR" {
			t.Errorf("expected ERROR level, got %v", logEntry["level"])
		}
		if !strings.Contains(logEntry["msg"].(string), "something unexpected crashed!") {
			t.Errorf("expected panic message in msg, got %v", logEntry["msg"])
		}
		if stack, ok := logEntry["stack"].(string); !ok || !strings.Contains(stack, "panic") {
			t.Errorf("expected stack trace in log entry, got %v", logEntry["stack"])
		}
		if reqID, ok := logEntry["request_id"].(string); !ok || reqID == "" {
			t.Errorf("expected request_id in recovery log entry, got %v", logEntry["request_id"])
		}
	})

	t.Run("Passes_Through_Normal_Requests", func(t *testing.T) {
		handler := middleware.Recovery(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		}))

		req := httptest.NewRequest("GET", "/normal", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got %d", rec.Code)
		}
		if rec.Body.String() != "ok" {
			t.Errorf("expected body 'ok', got %q", rec.Body.String())
		}
	})
}

func TestCombinedChain(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))

	// Chain: Recovery -> RequestID -> TraceContext -> Logging -> Mux
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ping", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("pong"))
	})
	mux.HandleFunc("GET /crash", func(w http.ResponseWriter, r *http.Request) {
		panic("database connection lost")
	})

	chain := middleware.Recovery(logger)(
		middleware.RequestID(
			middleware.TraceContext(
				middleware.Logging(logger)(mux),
			),
		),
	)

	t.Run("Success_Flow", func(t *testing.T) {
		logBuf.Reset()
		req := httptest.NewRequest("GET", "/ping", nil)
		req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
		rec := httptest.NewRecorder()

		chain.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rec.Code)
		}
		if rec.Header().Get("X-Request-ID") == "" {
			t.Errorf("expected X-Request-ID response header")
		}

		var logEntry map[string]interface{}
		if err := json.Unmarshal(logBuf.Bytes(), &logEntry); err != nil {
			t.Fatalf("failed to parse log JSON: %v", err)
		}
		if logEntry["status"] != float64(200) || logEntry["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" {
			t.Errorf("unexpected log entry: %+v", logEntry)
		}
	})

	t.Run("Panic_Flow", func(t *testing.T) {
		logBuf.Reset()
		req := httptest.NewRequest("GET", "/crash", nil)
		rec := httptest.NewRecorder()

		chain.ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Errorf("expected status 500, got %d", rec.Code)
		}

		var logEntry map[string]interface{}
		if err := json.Unmarshal(logBuf.Bytes(), &logEntry); err != nil {
			t.Fatalf("failed to parse log JSON: %v", err)
		}
		if logEntry["level"] != "ERROR" || !strings.Contains(logEntry["msg"].(string), "database connection lost") {
			t.Errorf("unexpected panic log entry: %+v", logEntry)
		}
	})
}
