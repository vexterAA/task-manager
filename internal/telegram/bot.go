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
		if session.State == domain.SessionStateSettingsTZ || session.State == domain.SessionStateSettingsRemind {
			return b.handleSettingsMessage(ctx, msg, user, tz, session, draft)
		}
		if session.State == domain.SessionStateTaskEditDeadline || session.State == domain.SessionStateTaskEditRemind {
			return b.handleTaskEditMessage(ctx, msg, user, tz, session, draft)
		}
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
		items, err := b.taskService.ListActive(user.ID, tz)
		if err != nil {
			return b.sendMenuText(ctx, msg.Chat.ID, "Не смог получить список задач.")
		}
		now := time.Now().In(usecaseTimeLocation(tz))
		filtered := make([]domain.Task, 0)
		for _, t := range items {
			if t.DueAt == nil {
				continue
			}
			due := t.DueAt.In(now.Location())
			if isSameDay(due, now) || due.Before(now) {
				filtered = append(filtered, t)
			}
		}
		if len(filtered) == 0 {
			return b.sendMenuText(ctx, msg.Chat.ID, "Сегодня и просрочено — пусто.")
		}
		lines := make([]string, 0, len(filtered)+1)
		lines = append(lines, "Сегодня и просрочено:")
		draft.ListTaskIDs = make([]int64, 0, len(filtered))
		for i, t := range filtered {
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
	case "settings":
		session.State = domain.SessionStateSettingsMenu
		if err := b.saveSession(session, draft); err != nil {
			return err
		}
		return b.sendMenuWithInline(ctx, msg.Chat.ID, "Настройки:", settingsInlineKeyboard())
	case "help":
		return b.sendMenuText(ctx, msg.Chat.ID, helpText())
	case "due":
		id, dueAt, err := parseDueArgs(args, tz)
		if err != nil {
			return b.sendMenuText(ctx, msg.Chat.ID, "Не понял. Пример: 12 2026-02-01 18:00")
		}
		if dueAt != nil && isPastTime(*dueAt, tz) {
			return b.sendMenuText(ctx, msg.Chat.ID, "Эта дата уже прошла. Дай дату в будущем.")
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
	if action, args, ok := parseTaskEditCallback(cb.Data); ok {
		return b.handleTaskEditCallback(ctx, cb, action, args)
	}
	if action, args, ok := parseSettingsCallback(cb.Data); ok {
		return b.handleSettingsCallback(ctx, cb, action, args)
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
		session, draft := b.loadSession(user.ID)
		session.State = domain.SessionStateTaskEditMenu
		draft.EditTaskID = id
		if err := b.saveSession(session, draft); err != nil {
			return err
		}
		_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "")
		return b.sendMenuWithInline(ctx, cb.Message.Chat.ID, "Что изменить?", taskEditInlineKeyboard())
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

func settingsInlineKeyboard() *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "🕒 Таймзона", CallbackData: "s:tz"},
			},
			{
				{Text: "🔔 Дефолтные напоминания", CallbackData: "s:remind"},
			},
			{
				{Text: "Отмена", CallbackData: "s:cancel"},
			},
		},
	}
}

func settingsRemindKeyboard() *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "Не напоминать", CallbackData: "s:remind:none"},
			},
			{
				{Text: "За 10 мин", CallbackData: "s:remind:10m"},
				{Text: "За 1 час", CallbackData: "s:remind:1h"},
			},
			{
				{Text: "Каждые 2 часа", CallbackData: "s:remind:2h"},
				{Text: "Каждый день", CallbackData: "s:remind:1d"},
			},
			{
				{Text: "Свой интервал…", CallbackData: "s:remind:custom"},
				{Text: "Назад", CallbackData: "s:back"},
			},
		},
	}
}

func taskEditInlineKeyboard() *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "⏰ Дедлайн", CallbackData: "te:deadline"},
				{Text: "🔔 Напоминания", CallbackData: "te:remind"},
			},
			{
				{Text: "Отмена", CallbackData: "te:cancel"},
			},
		},
	}
}

func taskEditDeadlineKeyboard() *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "Без дедлайна", CallbackData: "te:deadline:none"},
				{Text: "Сегодня", CallbackData: "te:deadline:today"},
			},
			{
				{Text: "Завтра", CallbackData: "te:deadline:tomorrow"},
				{Text: "Ввести дату…", CallbackData: "te:deadline:input"},
			},
			{
				{Text: "Назад", CallbackData: "te:back"},
			},
		},
	}
}

func taskEditRemindKeyboard(hasDefault bool) *InlineKeyboardMarkup {
	rows := make([][]InlineKeyboardButton, 0, 5)
	rows = append(rows, []InlineKeyboardButton{
		{Text: "Не напоминать", CallbackData: "te:remind:none"},
	})
	if hasDefault {
		rows = append(rows, []InlineKeyboardButton{
			{Text: "По умолчанию", CallbackData: "te:remind:default"},
		})
	}
	rows = append(rows, []InlineKeyboardButton{
		{Text: "За 10 мин", CallbackData: "te:remind:10m"},
		{Text: "За 1 час", CallbackData: "te:remind:1h"},
	})
	rows = append(rows, []InlineKeyboardButton{
		{Text: "Каждые 2 часа", CallbackData: "te:remind:2h"},
		{Text: "Каждый день", CallbackData: "te:remind:1d"},
	})
	rows = append(rows, []InlineKeyboardButton{
		{Text: "Свой интервал…", CallbackData: "te:remind:custom"},
		{Text: "Назад", CallbackData: "te:back"},
	})
	return &InlineKeyboardMarkup{InlineKeyboard: rows}
}

func (b *Bot) handleTaskEditCallback(ctx context.Context, cb *CallbackQuery, action, args string) error {
	user, err := b.users.GetByTelegramID(cb.From.ID)
	if err != nil {
		_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "Не нашёл пользователя.")
		return err
	}
	session, draft := b.loadSession(user.ID)
	if draft.EditTaskID == 0 {
		return b.client.AnswerCallbackQuery(ctx, cb.ID, "")
	}
	tz := user.Timezone
	if tz == "" {
		tz = "UTC"
	}
	switch action {
	case "cancel":
		draft.EditTaskID = 0
		draft.PendingInput = ""
		draft.PendingDate = ""
		session.State = domain.SessionStateIdle
		if err := b.saveSession(session, draft); err != nil {
			return err
		}
		_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "")
		return b.sendMenuText(ctx, cb.Message.Chat.ID, "Ок.")
	case "back":
		session.State = domain.SessionStateTaskEditMenu
		if err := b.saveSession(session, draft); err != nil {
			return err
		}
		_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "")
		return b.sendMenuWithInline(ctx, cb.Message.Chat.ID, "Что изменить?", taskEditInlineKeyboard())
	case "deadline":
		if args == "" {
			session.State = domain.SessionStateTaskEditDeadline
			if err := b.saveSession(session, draft); err != nil {
				return err
			}
			_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "")
			return b.sendMenuWithInline(ctx, cb.Message.Chat.ID, "Дедлайн:", taskEditDeadlineKeyboard())
		}
		switch args {
		case "none":
			if _, err := b.taskService.SetDue(draft.EditTaskID, nil, tz); err != nil {
				return err
			}
			draft.EditTaskID = 0
			session.State = domain.SessionStateIdle
			if err := b.saveSession(session, draft); err != nil {
				return err
			}
			_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "")
			return b.sendMenuText(ctx, cb.Message.Chat.ID, "Дедлайн убрал.")
		case "today":
			date := time.Now().In(usecaseTimeLocation(tz))
			draft.PendingInput = "deadline_time"
			draft.PendingDate = date.Format("2006-01-02")
			session.State = domain.SessionStateTaskEditDeadline
			if err := b.saveSession(session, draft); err != nil {
				return err
			}
			_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "Сегодня")
			return b.sendMenuText(ctx, cb.Message.Chat.ID, "Введи время (например 17:00 или 17.00)")
		case "tomorrow":
			date := time.Now().In(usecaseTimeLocation(tz)).Add(24 * time.Hour)
			draft.PendingInput = "deadline_time"
			draft.PendingDate = date.Format("2006-01-02")
			session.State = domain.SessionStateTaskEditDeadline
			if err := b.saveSession(session, draft); err != nil {
				return err
			}
			_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "Завтра")
			return b.sendMenuText(ctx, cb.Message.Chat.ID, "Введи время (например 17:00 или 17.00)")
		case "input":
			draft.PendingInput = "deadline"
			session.State = domain.SessionStateTaskEditDeadline
			if err := b.saveSession(session, draft); err != nil {
				return err
			}
			_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "")
			return b.sendMenuText(ctx, cb.Message.Chat.ID, "Напиши по‑человечески: сегодня 18:30, завтра 9, через 2ч, 12.02 18:00")
		default:
			return b.client.AnswerCallbackQuery(ctx, cb.ID, "")
		}
	case "remind":
		if args == "" {
			session.State = domain.SessionStateTaskEditRemind
			if err := b.saveSession(session, draft); err != nil {
				return err
			}
			_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "")
			return b.sendMenuWithInline(ctx, cb.Message.Chat.ID, "Напоминания:", taskEditRemindKeyboard(user.DefaultRemindKind != ""))
		}
		task, err := b.taskService.GetByID(draft.EditTaskID, tz)
		if err != nil {
			return err
		}
		switch args {
		case "none":
			if _, err := b.taskService.SetRemind(draft.EditTaskID, nil, tz); err != nil {
				return err
			}
		case "default":
			if user.DefaultRemindKind == "" {
				return b.client.AnswerCallbackQuery(ctx, cb.ID, "Нет дефолта.")
			}
			if user.DefaultRemindKind == "before" {
				if task.DueAt == nil {
					return b.client.AnswerCallbackQuery(ctx, cb.ID, "Нужен дедлайн.")
				}
				dur, err := parseIntervalInput(user.DefaultRemindInterval)
				if err != nil {
					return err
				}
				rt := task.DueAt.Add(-dur)
				if _, err := b.taskService.SetRemind(draft.EditTaskID, &rt, tz); err != nil {
					return err
				}
			} else if user.DefaultRemindKind == "interval" {
				dur, err := parseIntervalInput(user.DefaultRemindInterval)
				if err != nil {
					return err
				}
				rt := time.Now().In(usecaseTimeLocation(tz)).Add(dur)
				if _, err := b.taskService.SetRemind(draft.EditTaskID, &rt, tz); err != nil {
					return err
				}
			} else {
				if _, err := b.taskService.SetRemind(draft.EditTaskID, nil, tz); err != nil {
					return err
				}
			}
		case "10m", "1h":
			if task.DueAt == nil {
				return b.client.AnswerCallbackQuery(ctx, cb.ID, "Нужен дедлайн.")
			}
			dur, err := parseIntervalInput(args)
			if err != nil {
				return err
			}
			rt := task.DueAt.Add(-dur)
			if _, err := b.taskService.SetRemind(draft.EditTaskID, &rt, tz); err != nil {
				return err
			}
		case "2h", "1d":
			dur, err := parseIntervalInput(args)
			if err != nil {
				return err
			}
			rt := time.Now().In(usecaseTimeLocation(tz)).Add(dur)
			if _, err := b.taskService.SetRemind(draft.EditTaskID, &rt, tz); err != nil {
				return err
			}
		case "custom":
			draft.PendingInput = "remind_interval"
			session.State = domain.SessionStateTaskEditRemind
			if err := b.saveSession(session, draft); err != nil {
				return err
			}
			_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "")
			return b.sendMenuText(ctx, cb.Message.Chat.ID, intervalHint())
		default:
			return b.client.AnswerCallbackQuery(ctx, cb.ID, "")
		}
		draft.EditTaskID = 0
		draft.PendingInput = ""
		draft.PendingDate = ""
		session.State = domain.SessionStateIdle
		if err := b.saveSession(session, draft); err != nil {
			return err
		}
		_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "")
		return b.sendMenuText(ctx, cb.Message.Chat.ID, "Напоминания обновил.")
	default:
		return b.client.AnswerCallbackQuery(ctx, cb.ID, "")
	}
}

func (b *Bot) handleTaskEditMessage(ctx context.Context, msg *Message, user domain.User, tz string, session domain.UserSession, draft taskDraft) error {
	if draft.EditTaskID == 0 {
		return nil
	}
	switch session.State {
	case domain.SessionStateTaskEditDeadline:
		if draft.PendingInput == "" {
			return nil
		}
		dt, _, err := parseDeadlineInput(msg.Text, tz, draft.PendingInput, draft.PendingDate)
		if err != nil {
			switch {
			case errors.Is(err, errDeadlineTimeOnly):
				return b.sendMenuText(ctx, msg.Chat.ID, "Время в формате 17:00 или 17.00")
			case errors.Is(err, errDeadlinePast):
				return b.sendMenuText(ctx, msg.Chat.ID, "Эта дата уже прошла. Дай дату в будущем.")
			default:
				return b.sendMenuText(ctx, msg.Chat.ID, "Не понял дату. Примеры: сегодня 18:30, завтра 9, через 2ч, 12.02 18:00")
			}
		}
		if _, err := b.taskService.SetDue(draft.EditTaskID, &dt, tz); err != nil {
			return err
		}
		draft.EditTaskID = 0
		draft.PendingInput = ""
		draft.PendingDate = ""
		session.State = domain.SessionStateIdle
		if err := b.saveSession(session, draft); err != nil {
			return err
		}
		return b.sendMenuText(ctx, msg.Chat.ID, "Дедлайн обновлён.")
	case domain.SessionStateTaskEditRemind:
		if draft.PendingInput != "remind_interval" {
			return nil
		}
		dur, err := parseIntervalInput(msg.Text)
		if err != nil || dur <= 0 {
			return b.sendMenuText(ctx, msg.Chat.ID, intervalHint())
		}
		rt := time.Now().In(usecaseTimeLocation(tz)).Add(dur)
		if _, err := b.taskService.SetRemind(draft.EditTaskID, &rt, tz); err != nil {
			return err
		}
		draft.EditTaskID = 0
		draft.PendingInput = ""
		draft.PendingDate = ""
		session.State = domain.SessionStateIdle
		if err := b.saveSession(session, draft); err != nil {
			return err
		}
		return b.sendMenuText(ctx, msg.Chat.ID, "Напоминания обновил.")
	default:
		return nil
	}
}
func (b *Bot) handleSettingsCallback(ctx context.Context, cb *CallbackQuery, action, args string) error {
	user, err := b.users.GetByTelegramID(cb.From.ID)
	if err != nil {
		_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "Не нашёл пользователя.")
		return err
	}
	session, draft := b.loadSession(user.ID)
	switch action {
	case "cancel":
		session.State = domain.SessionStateIdle
		if err := b.saveSession(session, draft); err != nil {
			return err
		}
		_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "")
		return b.sendMenuText(ctx, cb.Message.Chat.ID, "Ок.")
	case "back":
		session.State = domain.SessionStateSettingsMenu
		if err := b.saveSession(session, draft); err != nil {
			return err
		}
		_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "")
		return b.sendMenuWithInline(ctx, cb.Message.Chat.ID, "Настройки:", settingsInlineKeyboard())
	case "tz":
		session.State = domain.SessionStateSettingsTZ
		if err := b.saveSession(session, draft); err != nil {
			return err
		}
		_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "")
		return b.sendMenuText(ctx, cb.Message.Chat.ID, "Пришли таймзону. Примеры: Europe/Moscow или +03:00")
	case "remind":
		session.State = domain.SessionStateSettingsRemind
		if err := b.saveSession(session, draft); err != nil {
			return err
		}
		_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "")
		return b.sendMenuWithInline(ctx, cb.Message.Chat.ID, "Дефолтные напоминания:", settingsRemindKeyboard())
	default:
		if action == "remind" {
			switch args {
			case "none":
				user.DefaultRemindKind = "none"
				user.DefaultRemindInterval = ""
			case "10m", "1h":
				user.DefaultRemindKind = "before"
				user.DefaultRemindInterval = args
			case "2h", "1d":
				user.DefaultRemindKind = "interval"
				user.DefaultRemindInterval = args
			case "custom":
				session.State = domain.SessionStateSettingsRemind
				if err := b.saveSession(session, draft); err != nil {
					return err
				}
				_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "")
				return b.sendMenuText(ctx, cb.Message.Chat.ID, intervalHint())
			default:
				return b.client.AnswerCallbackQuery(ctx, cb.ID, "")
			}
			updated, err := b.users.UpdateUser(user)
			if err != nil {
				return err
			}
			user = updated
			session.State = domain.SessionStateIdle
			if err := b.saveSession(session, draft); err != nil {
				return err
			}
			_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "")
			return b.sendMenuText(ctx, cb.Message.Chat.ID, "Сохранил дефолтные напоминания.")
		}
		return b.client.AnswerCallbackQuery(ctx, cb.ID, "")
	}
}

func (b *Bot) handleSettingsMessage(ctx context.Context, msg *Message, user domain.User, tz string, session domain.UserSession, draft taskDraft) error {
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return b.sendMenuText(ctx, msg.Chat.ID, "Напиши значение.")
	}
	if strings.EqualFold(text, "отмена") {
		session.State = domain.SessionStateIdle
		if err := b.saveSession(session, draft); err != nil {
			return err
		}
		return b.sendMenuText(ctx, msg.Chat.ID, "Ок.")
	}
	switch session.State {
	case domain.SessionStateSettingsTZ:
		if _, err := usecase.LocationFromTZ(text); err != nil {
			return b.sendMenuText(ctx, msg.Chat.ID, "Не понял таймзону. Примеры: Europe/Moscow или +03:00")
		}
		user.Timezone = text
		updated, err := b.users.UpdateUser(user)
		if err != nil {
			return err
		}
		_ = updated
		session.State = domain.SessionStateIdle
		if err := b.saveSession(session, draft); err != nil {
			return err
		}
		return b.sendMenuText(ctx, msg.Chat.ID, "Таймзона сохранена.")
	case domain.SessionStateSettingsRemind:
		dur, err := parseIntervalInput(text)
		if err != nil || dur <= 0 {
			return b.sendMenuText(ctx, msg.Chat.ID, intervalHint())
		}
		user.DefaultRemindKind = "interval"
		user.DefaultRemindInterval = dur.String()
		updated, err := b.users.UpdateUser(user)
		if err != nil {
			return err
		}
		_ = updated
		session.State = domain.SessionStateIdle
		if err := b.saveSession(session, draft); err != nil {
			return err
		}
		return b.sendMenuText(ctx, msg.Chat.ID, "Сохранил дефолтные напоминания.")
	default:
		return nil
	}
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
	overdue, err := b.taskService.ListOverdueForNotify(now)
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
	for _, t := range overdue {
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
		text := "⏰ Дедлайн просрочен\n" + formatTaskLineWithAttachments(toTaskInLocation(t, loc), len(attachments))
		if err := b.client.SendMessageWithMarkup(ctx, user.ChatID, text, taskInlineKeyboard(t.ID)); err != nil {
			log.Printf("overdue send error: %v", err)
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

func isSameDay(a, b time.Time) bool {
	y1, m1, d1 := a.Date()
	y2, m2, d2 := b.Date()
	return y1 == y2 && m1 == m2 && d1 == d2
}
