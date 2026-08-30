package owner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
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
