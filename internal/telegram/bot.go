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
	sessions    repository.SessionRepository
	attachments repository.AttachmentRepository
	pollTimeout time.Duration
	notifyEvery time.Duration
	sessionTTL  time.Duration
}

func NewBot(token string, taskService *usecase.TaskService, users repository.UserRepository, sessions repository.SessionRepository, attachments repository.AttachmentRepository, pollTimeout, notifyEvery, sessionTTL time.Duration) *Bot {
	return &Bot{
		client:      NewClient(token),
		taskService: taskService,
		users:       users,
		sessions:    sessions,
		attachments: attachments,
		pollTimeout: pollTimeout,
		notifyEvery: notifyEvery,
		sessionTTL:  sessionTTL,
	}
}

func (b *Bot) Run(ctx context.Context) error {
	if b.notifyEvery > 0 {
		go b.runNotifier(ctx)
	}
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
			if upd.Message != nil {
				if err := b.handleMessage(ctx, upd.Message); err != nil {
					log.Printf("telegram handle message error: %v", err)
				}
				continue
			}
			if upd.CallbackQuery != nil {
				if err := b.handleCallback(ctx, upd.CallbackQuery); err != nil {
					log.Printf("telegram callback error: %v", err)
				}
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
		command, args = mapMenuCommand(msg.Text)
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
	session, draft := b.loadSession(user.ID)

	if command == "" {
		if session.State == domain.SessionStatePickTask {
			return b.handlePickTaskMessage(ctx, msg, user, tz, session, draft)
		}
		if session.State != domain.SessionStateIdle {
			return b.handleWizardMessage(ctx, msg, user, tz, session, draft)
		}
		return nil
	}

	switch command {
	case "start":
		return b.sendMenuText(ctx, msg.Chat.ID, helpText())
	case "add":
		return b.startWizard(ctx, user, msg.Chat.ID)
	case "list":
		items, err := b.taskService.ListActive(user.ID, tz)
		if err != nil {
			return b.sendMenuText(ctx, msg.Chat.ID, "Не смог получить список задач.")
		}
		if len(items) == 0 {
			return b.sendMenuText(ctx, msg.Chat.ID, "Пока пусто. Жми «Создать задачу» в меню.")
		}
		lines := make([]string, 0, len(items)+1)
		lines = append(lines, "Активные задачи:")
		draft.ListTaskIDs = make([]int64, 0, len(items))
		for i, t := range items {
			attachments, err := b.attachments.ListAttachmentsByTaskID(t.ID)
			if err != nil {
				return err
			}
			draft.ListTaskIDs = append(draft.ListTaskIDs, t.ID)
			lines = append(lines, formatTaskSummaryLine(i+1, t, len(attachments)))
		}
		if err := b.saveSession(session, draft); err != nil {
			return err
		}
		if err := b.client.SendMessageWithMarkup(ctx, msg.Chat.ID, strings.Join(lines, "\n"), listPickInlineKeyboard()); err != nil {
			return err
		}
		return b.sendMenuText(ctx, msg.Chat.ID, "Чтобы открыть задачу, нажми «Ввести номер».")
	case "today":
		return b.sendMenuText(ctx, msg.Chat.ID, "Пока не сделал фильтр «сегодня/просрочено».")
	case "settings":
		return b.sendMenuText(ctx, msg.Chat.ID, "Настройки позже: таймзона и дефолтные напоминания.")
	case "help":
		return b.sendMenuText(ctx, msg.Chat.ID, helpText())
	case "due":
		id, dueAt, err := parseDueArgs(args, tz)
		if err != nil {
			return b.sendMenuText(ctx, msg.Chat.ID, "Не понял. Пример: 12 2026-02-01 18:00")
		}
		if err := b.ensureTaskOwner(id, user.ID, tz); err != nil {
			return b.sendMenuText(ctx, msg.Chat.ID, "Задача не найдена.")
		}
		task, err := b.taskService.SetDue(id, dueAt, tz)
		if err != nil {
			return b.sendMenuText(ctx, msg.Chat.ID, "Не смог поставить срок.")
		}
		if _, err := b.taskService.SetRemind(id, dueAt, tz); err != nil {
			return b.sendMenuText(ctx, msg.Chat.ID, "Срок поставил, а напоминание — нет :(")
		}
		return b.sendMenuText(ctx, msg.Chat.ID, fmt.Sprintf("Срок для #%d: %s.", task.ID, formatTime(dueAt)))
	default:
		return b.sendMenuText(ctx, msg.Chat.ID, "Не понял. Жми кнопки в меню.")
	}
}

func (b *Bot) handleCallback(ctx context.Context, cb *CallbackQuery) error {
	if cb.Message == nil {
		return b.client.AnswerCallbackQuery(ctx, cb.ID, "")
	}
	if action, args, ok := parseWizardCallback(cb.Data); ok {
		return b.handleWizardCallback(ctx, cb, action, args)
	}
	action, id, ok := parseTaskCallback(cb.Data)
	if !ok {
		return b.client.AnswerCallbackQuery(ctx, cb.ID, "")
	}
	user, err := b.users.GetByTelegramID(cb.From.ID)
	if err != nil {
		_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "Не нашёл пользователя.")
		return err
	}
	tz := user.Timezone
	if tz == "" {
		tz = "UTC"
	}
	if action == "pick" {
		session, draft := b.loadSession(user.ID)
		if len(draft.ListTaskIDs) == 0 {
			_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "Сначала открой список.")
			return nil
		}
		session.State = domain.SessionStatePickTask
		if err := b.saveSession(session, draft); err != nil {
			return err
		}
		_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "")
		return b.sendMenuText(ctx, cb.Message.Chat.ID, "Напиши номер задачи из списка.")
	}
	if err := b.ensureTaskOwner(id, user.ID, tz); err != nil {
		_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "Задача не найдена.")
		return err
	}
	switch action {
	case "done":
		task, err := b.taskService.MarkDone(id, tz)
		if err != nil {
			_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "Не смог завершить.")
			return err
		}
		if err := b.client.EditMessageReplyMarkup(ctx, cb.Message.Chat.ID, cb.Message.MessageID, doneInlineKeyboard()); err != nil {
			log.Printf("edit markup error: %v", err)
		}
		return b.client.AnswerCallbackQuery(ctx, cb.ID, fmt.Sprintf("Готово: #%d", task.ID))
	case "del":
		if err := b.taskService.Delete(id); err != nil {
			_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "Не смог удалить.")
			return err
		}
		if err := b.client.EditMessageReplyMarkup(ctx, cb.Message.Chat.ID, cb.Message.MessageID, deletedInlineKeyboard()); err != nil {
			log.Printf("edit markup error: %v", err)
		}
		return b.client.AnswerCallbackQuery(ctx, cb.ID, fmt.Sprintf("Удалил: #%d", id))
	case "rem":
		_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "Экран напоминаний пока не готов.")
		return nil
	case "snooze":
		now := time.Now().UTC().Add(15 * time.Minute)
		if _, err := b.taskService.SetRemind(id, &now, tz); err != nil {
			_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "Не смог отложить.")
			return err
		}
		_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "Отложил на 15 минут.")
		return nil
	case "att":
		_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "")
		items, err := b.attachments.ListAttachmentsByTaskID(id)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			return b.client.SendMessage(ctx, cb.Message.Chat.ID, "Вложений нет.")
		}
		for _, a := range items {
			switch a.Type {
			case "photo":
				if err := b.client.SendPhoto(ctx, cb.Message.Chat.ID, a.TelegramFileID, a.Caption); err != nil {
					return err
				}
			default:
				if err := b.client.SendDocument(ctx, cb.Message.Chat.ID, a.TelegramFileID, a.Caption); err != nil {
					return err
				}
			}
		}
		return nil
	default:
		return b.client.AnswerCallbackQuery(ctx, cb.ID, "")
	}
}

func (b *Bot) handlePickTaskMessage(ctx context.Context, msg *Message, user domain.User, tz string, session domain.UserSession, draft taskDraft) error {
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return b.sendMenuText(ctx, msg.Chat.ID, "Пришли номер задачи из списка.")
	}
	if strings.EqualFold(text, "отмена") {
		session.State = domain.SessionStateIdle
		if err := b.saveSession(session, draft); err != nil {
			return err
		}
		return b.sendMenuText(ctx, msg.Chat.ID, "Ок, вернулись в меню.")
	}
	idx, err := strconv.Atoi(text)
	if err != nil || idx <= 0 || idx > len(draft.ListTaskIDs) {
		return b.sendMenuText(ctx, msg.Chat.ID, "Нужен номер из списка.")
	}
	taskID := draft.ListTaskIDs[idx-1]
	if err := b.ensureTaskOwner(taskID, user.ID, tz); err != nil {
		return b.sendMenuText(ctx, msg.Chat.ID, "Задача не найдена.")
	}
	task, err := b.taskService.GetByID(taskID, tz)
	if err != nil {
		return err
	}
	attachments, err := b.attachments.ListAttachmentsByTaskID(taskID)
	if err != nil {
		return err
	}
	session.State = domain.SessionStateIdle
	if err := b.saveSession(session, draft); err != nil {
		return err
	}
	return b.client.SendMessageWithMarkup(ctx, msg.Chat.ID, formatTaskLineWithAttachments(task, len(attachments)), taskInlineKeyboard(task.ID))
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

func (b *Bot) sendMenuText(ctx context.Context, chatID int64, text string) error {
	return b.client.SendMessageWithMarkup(ctx, chatID, text, mainMenuMarkup())
}

func (b *Bot) sendMenuWithInline(ctx context.Context, chatID int64, text string, inline *InlineKeyboardMarkup) error {
	return b.client.SendMessageWithMarkup(ctx, chatID, text, inline)
}

func (b *Bot) runNotifier(ctx context.Context) {
	ticker := time.NewTicker(b.notifyEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := b.processReminders(ctx); err != nil {
				log.Printf("notify error: %v", err)
			}
		}
	}
}

func (b *Bot) processReminders(ctx context.Context) error {
	now := time.Now().UTC()
	items, err := b.taskService.ListDueForNotify(now)
	if err != nil {
		return err
	}
	for _, t := range items {
		user, err := b.users.GetUserByID(t.UserID)
		if err != nil {
			continue
		}
		tz := user.Timezone
		if tz == "" {
			tz = "UTC"
		}
		loc, err := usecase.LocationFromTZ(tz)
		if err != nil {
			loc = time.UTC
		}
		attachments, err := b.attachments.ListAttachmentsByTaskID(t.ID)
		if err != nil {
			attachments = nil
		}
		text := "🔔 Напоминание\n" + formatTaskLineWithAttachments(toTaskInLocation(t, loc), len(attachments))
		if err := b.client.SendMessageWithMarkup(ctx, user.ChatID, text, taskInlineKeyboard(t.ID)); err != nil {
			log.Printf("notify send error: %v", err)
		}
	}
	return nil
}

func toTaskInLocation(t domain.Task, loc *time.Location) domain.Task {
	t.CreatedAt = t.CreatedAt.In(loc)
	t.UpdatedAt = t.UpdatedAt.In(loc)
	if t.DueAt != nil {
		tt := t.DueAt.In(loc)
		t.DueAt = &tt
	}
	if t.RemindAt != nil {
		tt := t.RemindAt.In(loc)
		t.RemindAt = &tt
	}
	if t.NotifiedAt != nil {
		tt := t.NotifiedAt.In(loc)
		t.NotifiedAt = &tt
	}
	return t
}
