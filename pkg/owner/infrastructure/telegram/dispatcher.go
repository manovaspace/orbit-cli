package telegram

import (
	"context"
	"time"
)

// Dispatcher sends admin grants to the configured forum topics.
type Dispatcher struct {
	Config *TelegramConfig
}

// SendGrant posts the grant to the Secrets topic.
func (d *Dispatcher) SendGrant(ctx context.Context, recipient, code, role string, expiresAt time.Time) error {
	return DispatchAdminGrantTelegram(ctx, d.Config, recipient, code, role, expiresAt)
}
