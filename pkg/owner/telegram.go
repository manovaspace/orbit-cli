package owner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// TelegramConfig defines the configuration required for Telegram Forum notification.
type TelegramConfig struct {
	BotToken     string `json:"bot_token"`
	ChatID       int64  `json:"chat_id"`
	TopicSecrets int    `json:"topic_secrets"`
	TopicStaff   int    `json:"topic_staff"`
}

// ResolveTelegramConfig resolves Telegram settings from config or environment variables.
func ResolveTelegramConfig() (*TelegramConfig, error) {
	token := strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))
	if token == "" {
		token = strings.TrimSpace(os.Getenv("ORBIT_TELEGRAM_BOT_TOKEN"))
	}
	if token == "" {
		return nil, errors.New("TELEGRAM_BOT_TOKEN environment variable not set")
	}

	rawChatID := strings.TrimSpace(os.Getenv("TELEGRAM_CHAT_ID"))
	if rawChatID == "" {
		rawChatID = strings.TrimSpace(os.Getenv("ORBIT_TELEGRAM_CHAT_ID"))
	}
	if rawChatID == "" {
		return nil, errors.New("TELEGRAM_CHAT_ID environment variable not set")
	}

	chatID, err := strconv.ParseInt(rawChatID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid TELEGRAM_CHAT_ID %q: %w", rawChatID, err)
	}

	var topicSecrets int
	rawSecrets := strings.TrimSpace(os.Getenv("TELEGRAM_TOPIC_SECRETS"))
	if rawSecrets == "" {
		rawSecrets = strings.TrimSpace(os.Getenv("ORBIT_TELEGRAM_TOPIC_SECRETS"))
	}
	if rawSecrets != "" {
		if val, err := strconv.Atoi(rawSecrets); err == nil {
			topicSecrets = val
		}
	}

	var topicStaff int
	rawStaff := strings.TrimSpace(os.Getenv("TELEGRAM_TOPIC_STAFF"))
	if rawStaff == "" {
		rawStaff = strings.TrimSpace(os.Getenv("ORBIT_TELEGRAM_TOPIC_STAFF"))
	}
	if rawStaff != "" {
		if val, err := strconv.Atoi(rawStaff); err == nil {
			topicStaff = val
		}
	}

	return &TelegramConfig{
		BotToken:     token,
		ChatID:       chatID,
		TopicSecrets: topicSecrets,
		TopicStaff:   topicStaff,
	}, nil
}

// SendTelegramMessage sends a message to a Telegram chat / topic thread via HTTP API.
func SendTelegramMessage(ctx context.Context, token string, chatID int64, threadID int, htmlText string) error {
	if strings.TrimSpace(token) == "" {
		return errors.New("telegram bot token is empty")
	}
	if chatID == 0 {
		return errors.New("telegram chat ID is required")
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	payload := map[string]interface{}{
		"chat_id":    chatID,
		"text":       htmlText,
		"parse_mode": "HTML",
	}
	if threadID > 0 {
		payload["message_thread_id"] = threadID
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal telegram payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create telegram request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send telegram message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("telegram API returned error status %d", resp.StatusCode)
	}

	return nil
}

// DispatchAdminGrantTelegram formats and sends an admin grant to Telegram (Secrets topic).
func DispatchAdminGrantTelegram(ctx context.Context, cfg *TelegramConfig, email, code, role string, expiresAt time.Time) error {
	if cfg == nil {
		return errors.New("telegram configuration is nil")
	}

	secretsMsg := fmt.Sprintf("👤 <b>Admin Grant Issued</b>\n"+
		"📧 <code>%s</code>\n"+
		"🔑 OTP <code>%s</code>\n"+
		"🛡 Role: <b>%s</b>\n"+
		"⏳ Expires: %s\n\n"+
		"<i>Usage:</i>\n<code>orbit admin init %s --code %s</code>",
		email, code, role, expiresAt.UTC().Format("15:04:05 UTC"), email, code,
	)

	targetThread := cfg.TopicSecrets
	if targetThread == 0 {
		targetThread = cfg.TopicStaff
	}

	if err := SendTelegramMessage(ctx, cfg.BotToken, cfg.ChatID, targetThread, secretsMsg); err != nil {
		return err
	}

	// Post audit note in Staff topic if distinct from Secrets topic
	if cfg.TopicStaff > 0 && cfg.TopicStaff != cfg.TopicSecrets {
		staffMsg := fmt.Sprintf("✅ <b>%s</b> admin grant issued · expires %s",
			email, expiresAt.UTC().Format("15:04:05 UTC"),
		)
		_ = SendTelegramMessage(ctx, cfg.BotToken, cfg.ChatID, cfg.TopicStaff, staffMsg)
	}

	return nil
}
