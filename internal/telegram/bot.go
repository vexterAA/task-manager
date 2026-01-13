package telegram

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"example.com/yourapp/internal/domain"
	"example.com/yourapp/internal/repository"
	"example.com/yourapp/internal/storage"
	"example.com/yourapp/internal/usecase"
)

type Bot struct {
	client      *Client
	taskService *usecase.TaskService
	users       repository.UserRepository
	pollTimeout time.Duration
}

func NewBot(token string, taskService *usecase.TaskService, users repository.UserRepository, pollTimeout time.Duration) *Bot {
	return &Bot{
		client:      NewClient(token),
		taskService: taskService,
		users:       users,
		pollTimeout: pollTimeout,
	}
}

func (b *Bot) Run(ctx context.Context) error {
	offset := 0
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		updates, err := b.client.GetUpdates(ctx, offset, b.pollTimeout)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return err
			}
			log.Printf("telegram getUpdates error: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}
		for _, upd := range updates {
			offset = upd.UpdateID + 1
			if upd.Message == nil || upd.Message.Text == "" {
				continue
			}
			if err := b.handleMessage(ctx, upd.Message); err != nil {
				log.Printf("telegram handle message error: %v", err)
			}
		}
	}
}

func (b *Bot) handleMessage(ctx context.Context, msg *Message) error {
	if msg.From == nil {
		return nil
	}
	command, args := parseCommand(msg.Text)
	if command == "" {
		return nil
	}

	user, err := b.ensureUser(msg)
	if err != nil {
		_ = b.client.SendMessage(ctx, msg.Chat.ID, "Что-то пошло не так, попробуй ещё раз.")
		return err
	}
	tz := user.Timezone
	if tz == "" {
		tz = "UTC"
	}

	switch command {
	case "start":
		return b.client.SendMessage(ctx, msg.Chat.ID, helpText())
	case "add":
		text, dueAt, err := parseAddArgs(args, tz)
		if err != nil {
			return b.client.SendMessage(ctx, msg.Chat.ID, "Формат: /add <текст> [YYYY-MM-DD HH:MM]")
		}
		var remindAt *time.Time
		if dueAt != nil {
			remindAt = dueAt
		}
		task, err := b.taskService.Create(user.ID, text, dueAt, remindAt, tz)
		if err != nil {
			if errors.Is(err, usecase.ErrInvalidText) {
				return b.client.SendMessage(ctx, msg.Chat.ID, "Текст пустой, давай по‑нормальному :)")
			}
			return b.client.SendMessage(ctx, msg.Chat.ID, "Не смог добавить задачу.")
		}
		return b.client.SendMessage(ctx, msg.Chat.ID, fmt.Sprintf("Ок, добавил задачу #%d.", task.ID))
	case "list":
		items, err := b.taskService.ListActive(user.ID, tz)
		if err != nil {
			return b.client.SendMessage(ctx, msg.Chat.ID, "Не смог получить список задач.")
		}
		return b.client.SendMessage(ctx, msg.Chat.ID, formatTaskList(items))
	case "done":
		id, err := parseIDArg(args)
		if err != nil {
			return b.client.SendMessage(ctx, msg.Chat.ID, "Формат: /done <id>")
		}
		if err := b.ensureTaskOwner(id, user.ID, tz); err != nil {
			return b.client.SendMessage(ctx, msg.Chat.ID, "Задача не найдена.")
		}
		task, err := b.taskService.MarkDone(id, tz)
		if err != nil {
			return b.client.SendMessage(ctx, msg.Chat.ID, "Не смог завершить задачу.")
		}
		return b.client.SendMessage(ctx, msg.Chat.ID, fmt.Sprintf("Готово, задача #%d закрыта.", task.ID))
	case "del":
		id, err := parseIDArg(args)
		if err != nil {
			return b.client.SendMessage(ctx, msg.Chat.ID, "Формат: /del <id>")
		}
		if err := b.ensureTaskOwner(id, user.ID, tz); err != nil {
			return b.client.SendMessage(ctx, msg.Chat.ID, "Задача не найдена.")
		}
		if err := b.taskService.Delete(id); err != nil {
			return b.client.SendMessage(ctx, msg.Chat.ID, "Не смог удалить задачу.")
		}
		return b.client.SendMessage(ctx, msg.Chat.ID, fmt.Sprintf("Удалил задачу #%d.", id))
	case "due":
		id, dueAt, err := parseDueArgs(args, tz)
		if err != nil {
			return b.client.SendMessage(ctx, msg.Chat.ID, "Формат: /due <id> <YYYY-MM-DD HH:MM>")
		}
		if err := b.ensureTaskOwner(id, user.ID, tz); err != nil {
			return b.client.SendMessage(ctx, msg.Chat.ID, "Задача не найдена.")
		}
		task, err := b.taskService.SetDue(id, dueAt, tz)
		if err != nil {
			return b.client.SendMessage(ctx, msg.Chat.ID, "Не смог поставить срок.")
		}
		if _, err := b.taskService.SetRemind(id, dueAt, tz); err != nil {
			return b.client.SendMessage(ctx, msg.Chat.ID, "Срок поставил, а напоминание — нет :(")
		}
		return b.client.SendMessage(ctx, msg.Chat.ID, fmt.Sprintf("Срок для #%d: %s.", task.ID, formatTime(dueAt)))
	default:
		return b.client.SendMessage(ctx, msg.Chat.ID, "Не понял команду. /start покажет хелп.")
	}
}

func (b *Bot) ensureUser(msg *Message) (domain.User, error) {
	user, err := b.users.GetByTelegramID(msg.From.ID)
	if err == nil {
		return user, nil
	}
	if !errors.Is(err, storage.ErrNotFound) {
		return domain.User{}, err
	}
	return b.users.CreateUser(domain.User{
		TelegramUserID: msg.From.ID,
		ChatID:         msg.Chat.ID,
		Timezone:       "UTC",
	})
}

func (b *Bot) ensureTaskOwner(taskID, userID int64, tz string) error {
	task, err := b.taskService.GetByID(taskID, tz)
	if err != nil {
		return err
	}
	if task.UserID != userID {
		return storage.ErrNotFound
	}
	return nil
}

func parseCommand(text string) (string, string) {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "/") {
		return "", ""
	}
	parts := strings.SplitN(trimmed, " ", 2)
	cmd := strings.TrimPrefix(parts[0], "/")
	if idx := strings.Index(cmd, "@"); idx >= 0 {
		cmd = cmd[:idx]
	}
	cmd = strings.ToLower(cmd)
	if len(parts) == 1 {
		return cmd, ""
	}
	return cmd, strings.TrimSpace(parts[1])
}

func parseAddArgs(args, tz string) (string, *time.Time, error) {
	args = strings.TrimSpace(args)
	if args == "" {
		return "", nil, errors.New("empty")
	}
	fields := strings.Fields(args)
	if len(fields) >= 3 {
		datePart := fields[len(fields)-2]
		timePart := fields[len(fields)-1]
		if looksLikeDate(datePart) && looksLikeTime(timePart) {
			dt, err := parseDateTime(datePart, timePart, tz)
			if err != nil {
				return "", nil, err
			}
			text := strings.TrimSpace(strings.Join(fields[:len(fields)-2], " "))
			if text == "" {
				return "", nil, errors.New("empty text")
			}
			return text, &dt, nil
		}
	}
	return args, nil, nil
}

func parseDueArgs(args, tz string) (int64, *time.Time, error) {
	fields := strings.Fields(args)
	if len(fields) < 3 {
		return 0, nil, errors.New("invalid")
	}
	id, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil || id <= 0 {
		return 0, nil, errors.New("id")
	}
	dt, err := parseDateTime(fields[1], fields[2], tz)
	if err != nil {
		return 0, nil, err
	}
	return id, &dt, nil
}

func parseIDArg(args string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(args), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("id")
	}
	return id, nil
}

func parseDateTime(datePart, timePart, tz string) (time.Time, error) {
	loc, err := usecase.LocationFromTZ(tz)
	if err != nil {
		return time.Time{}, err
	}
	return time.ParseInLocation("2006-01-02 15:04", datePart+" "+timePart, loc)
}

func looksLikeDate(s string) bool {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return false
	}
	for i := 0; i < len(s); i++ {
		if i == 4 || i == 7 {
			continue
		}
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func looksLikeTime(s string) bool {
	if len(s) != 5 || s[2] != ':' {
		return false
	}
	for i := 0; i < len(s); i++ {
		if i == 2 {
			continue
		}
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func formatTaskList(items []domain.Task) string {
	if len(items) == 0 {
		return "Пока пусто. Добавь задачу через /add."
	}
	lines := make([]string, 0, len(items)+1)
	lines = append(lines, "Активные задачи:")
	for _, t := range items {
		line := fmt.Sprintf("%d) %s", t.ID, t.Text)
		if t.DueAt != nil {
			line += " — до " + formatTime(t.DueAt)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func formatTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02 15:04")
}

func helpText() string {
	return strings.Join([]string{
		"Команды:",
		"/start — этот хелп",
		"/add <текст> [YYYY-MM-DD HH:MM] — добавить задачу",
		"/list — активные задачи",
		"/done <id> — завершить",
		"/del <id> — удалить",
		"/due <id> <YYYY-MM-DD HH:MM> — срок и напоминание",
	}, "\n")
}
