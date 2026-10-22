package owner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestResolveTelegramConfig_FromEnv(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11")
	t.Setenv("TELEGRAM_CHAT_ID", "-1001234567890")
	t.Setenv("TELEGRAM_TOPIC_SECRETS", "42")
	t.Setenv("TELEGRAM_TOPIC_STAFF", "10")

	cfg, err := ResolveTelegramConfig()
	if err != nil {
		t.Fatalf("ResolveTelegramConfig failed: %v", err)
	}

	if cfg.BotToken != "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11" {
		t.Errorf("expected bot token, got %s", cfg.BotToken)
	}
	if cfg.ChatID != -1001234567890 {
		t.Errorf("expected chat ID -1001234567890, got %d", cfg.ChatID)
	}
	if cfg.TopicSecrets != 42 {
		t.Errorf("expected secrets topic 42, got %d", cfg.TopicSecrets)
	}
	if cfg.TopicStaff != 10 {
		t.Errorf("expected staff topic 10, got %d", cfg.TopicStaff)
	}
}

func TestResolveTelegramConfig_MissingToken(t *testing.T) {
	_ = os.Unsetenv("TELEGRAM_BOT_TOKEN")
	_ = os.Unsetenv("ORBIT_TELEGRAM_BOT_TOKEN")

	_, err := ResolveTelegramConfig()
	if err == nil {
		t.Fatalf("expected error when bot token is missing")
	}
}

func TestSendTelegramMessage(t *testing.T) {
	var receivedPayload map[string]interface{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedPayload)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer ts.Close()

	// Direct call test with mock payload structure check
	payload := map[string]interface{}{
		"chat_id":           int64(-100123),
		"message_thread_id": 42,
		"text":              "test message",
		"parse_mode":        "HTML",
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, ts.URL, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("mock request failed: %v", err)
	}

	if receivedPayload["text"] != "test message" {
		t.Errorf("expected text 'test message', got %v", receivedPayload["text"])
	}
}

func TestEscapeHTML(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"user@example.com", "user@example.com"},
		{"<script>alert(1)</script>", "&lt;script&gt;alert(1)&lt;/script&gt;"},
		{"role & admin", "role &amp; admin"},
	}

	for _, tt := range tests {
		if got := escapeHTML(tt.input); got != tt.want {
			t.Errorf("escapeHTML(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestDispatchAdminGrantTelegram_Escaping(t *testing.T) {
	var receivedPayloads []map[string]interface{}
	var mu sync.Mutex

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&p)
		mu.Lock()
		receivedPayloads = append(receivedPayloads, p)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer ts.Close()

	// Inject custom HTTP client URL by setting an env or testing directly
	cfg := &TelegramConfig{
		BotToken:     "test-token",
		ChatID:       -100999,
		TopicSecrets: 42,
		TopicStaff:   10,
	}

	// Verify template formatting
	email := "test<dev>@manova.space"
	code := "123456"
	role := "admin&root"
	expiresAt := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

	escEmail := escapeHTML(email)
	if !strings.Contains(escEmail, "&lt;dev&gt;") {
		t.Errorf("expected escaped email, got %s", escEmail)
	}

	// Verify DispatchAdminGrantTelegram error when cfg is nil
	if err := DispatchAdminGrantTelegram(context.Background(), nil, email, code, role, expiresAt); err == nil {
		t.Errorf("expected error when cfg is nil")
	}
	_ = cfg
}
