package telegram

import (
	"context"
	"errors"
	"fmt"
	"log"
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
}

func NewBot(token string, taskService *usecase.TaskService, users repository.UserRepository, sessions repository.SessionRepository, attachments repository.AttachmentRepository, pollTimeout time.Duration) *Bot {
	return &Bot{
		client:      NewClient(token),
		taskService: taskService,
		users:       users,
		sessions:    sessions,
		attachments: attachments,
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
		for _, t := range items {
			attachments, err := b.attachments.ListAttachmentsByTaskID(t.ID)
			if err != nil {
				return err
			}
			text := formatTaskLineWithAttachments(t, len(attachments))
			if err := b.client.SendMessageWithMarkup(ctx, msg.Chat.ID, text, taskInlineKeyboard(t.ID)); err != nil {
				return err
			}
		}
		return b.sendMenuText(ctx, msg.Chat.ID, "Выбери действие или кнопку ниже.")
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
		_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "Snooze пока не готов.")
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
