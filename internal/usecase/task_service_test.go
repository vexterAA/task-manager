package usecase

import (
	"errors"
	"testing"
	"time"

	"example.com/yourapp/internal/domain"
	"example.com/yourapp/internal/storage/memory"
)

func TestTaskServiceCreate_ValidatesAndNormalizesTimes(t *testing.T) {
	repo := memory.New()
	user, err := repo.CreateUser(domain.User{TelegramUserID: 1, ChatID: 1, Timezone: "UTC"})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	svc := NewTaskService(repo)

	if _, err := svc.Create(user.ID, "   ", nil, nil, "+03:00"); !errors.Is(err, ErrInvalidText) {
		t.Fatalf("expected ErrInvalidText, got %v", err)
	}

	loc := time.FixedZone("+03:00", 3*3600)
	dueAt := time.Date(2026, 1, 2, 10, 0, 0, 0, loc)
	remindAt := time.Date(2026, 1, 2, 9, 30, 0, 0, loc)

	created, err := svc.Create(user.ID, "  test  ", &dueAt, &remindAt, "+03:00")
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if created.Text != "test" {
		t.Fatalf("expected trimmed text, got %q", created.Text)
	}
	if created.DueAt == nil || created.DueAt.Location().String() != loc.String() {
		t.Fatalf("expected due_at in %s, got %v", loc.String(), created.DueAt)
	}

	stored, err := repo.GetByID(created.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if stored.DueAt == nil || stored.DueAt.Location() != time.UTC {
		t.Fatalf("expected stored due_at in UTC, got %v", stored.DueAt)
	}
}

func TestTaskServiceListActive_ConvertsTimezone(t *testing.T) {
	repo := memory.New()
	user, err := repo.CreateUser(domain.User{TelegramUserID: 2, ChatID: 2, Timezone: "UTC"})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	dueAt := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	_, err = repo.CreateTask(domain.Task{
		UserID: user.ID,
		Text:   "task",
		Status: domain.TaskStatusActive,
		DueAt:  &dueAt,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	svc := NewTaskService(repo)
	items, err := svc.ListActive(user.ID, "+03:00")
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	if len(items) != 1 || items[0].DueAt == nil {
		t.Fatalf("expected 1 task with due_at, got %v", items)
	}
	if got := items[0].DueAt.Format("-07:00"); got != "+03:00" {
		t.Fatalf("expected due_at in +03:00, got %s", got)
	}
}

func TestTaskServiceListDueForNotify_Idempotent(t *testing.T) {
	repo := memory.New()
	user, err := repo.CreateUser(domain.User{TelegramUserID: 3, ChatID: 3, Timezone: "UTC"})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	svc := NewTaskService(repo)

	now := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	remindAt := now.Add(-time.Minute)
	_, err = svc.Create(user.ID, "notify", nil, &remindAt, "UTC")
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	items, err := svc.ListDueForNotify(now)
	if err != nil {
		t.Fatalf("list due: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 task, got %d", len(items))
	}

	items, err = svc.ListDueForNotify(now.Add(time.Second))
	if err != nil {
		t.Fatalf("list due second time: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected 0 tasks on second run, got %d", len(items))
	}
}
