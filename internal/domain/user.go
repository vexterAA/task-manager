package domain

import "time"

type User struct {
	ID                    int64     `json:"id"`
	TelegramUserID        int64     `json:"telegram_user_id"`
	ChatID                int64     `json:"chat_id"`
	Timezone              string    `json:"timezone"`
	DefaultRemindKind     string    `json:"default_remind_kind,omitempty"`
	DefaultRemindInterval string    `json:"default_remind_interval,omitempty"`
	CreatedAt             time.Time `json:"created_at"`
}
