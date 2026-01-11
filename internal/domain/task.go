package domain

import "time"

const (
	TaskStatusActive = "active"
	TaskStatusDone   = "done"
)

type Task struct {
	ID         int64      `json:"id"`
	UserID     int64      `json:"user_id"`
	Text       string     `json:"text"`
	Status     string     `json:"status"`
	DueAt      *time.Time `json:"due_at,omitempty"`
	RemindAt   *time.Time `json:"remind_at,omitempty"`
	NotifiedAt *time.Time `json:"notified_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

type Attachment struct {
	ID             int64  `json:"id"`
	TaskID         int64  `json:"task_id"`
	Type           string `json:"type"`
	TelegramFileID string `json:"telegram_file_id"`
	FileUniqueID   string `json:"file_unique_id"`
	Caption        string `json:"caption,omitempty"`
}
