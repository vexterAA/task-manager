package repository

import (
	"time"

	"example.com/yourapp/internal/domain"
)

// TaskRepository stores tasks in UTC and returns them in UTC.
// ListDueForNotify should mark returned tasks as notified to keep notifications idempotent.
type TaskRepository interface {
	Create(task domain.Task) (domain.Task, error)
	ListActive(userID int64) ([]domain.Task, error)
	GetByID(id int64) (domain.Task, error)
	MarkDone(id int64) (domain.Task, error)
	Delete(id int64) error
	SetDue(id int64, dueAt *time.Time) (domain.Task, error)
	SetRemind(id int64, remindAt *time.Time) (domain.Task, error)
	ListDueForNotify(now time.Time) ([]domain.Task, error)
}
