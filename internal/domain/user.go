package domain

import "time"

type User struct {
	ID             int64     `json:"id"`
	TelegramUserID int64     `json:"telegram_user_id"`
	ChatID         int64     `json:"chat_id"`
	Timezone       string    `json:"timezone"`
	CreatedAt      time.Time `json:"created_at"`
}
