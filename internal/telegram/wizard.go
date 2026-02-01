package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"example.com/yourapp/internal/domain"
	"example.com/yourapp/internal/usecase"
)

func (b *Bot) startWizard(ctx context.Context, user domain.User, chatID int64) error {
	draft := taskDraft{}
	session := domain.UserSession{
		UserID: user.ID,
		State:  domain.SessionStateCreateText,
	}
	if err := b.saveSession(session, draft); err != nil {
		return err
	}
	return b.client.SendMessageWithMarkup(ctx, chatID, "Напиши текст задачи.", &ForceReply{
		ForceReply:            true,
		InputFieldPlaceholder: "Текст задачи",
	})
}

func (b *Bot) handleWizardMessage(ctx context.Context, msg *Message, user domain.User, tz string, session domain.UserSession, draft taskDraft) error {
	switch session.State {
	case domain.SessionStateCreateText:
		text := strings.TrimSpace(msg.Text)
		if text == "" {
			return b.sendMenuText(ctx, msg.Chat.ID, "Текст пустой. Напиши задачу нормально.")
		}
		draft.Text = text
		session.State = domain.SessionStateCreateDeadline
		if err := b.saveSession(session, draft); err != nil {
			return err
		}
		return b.sendMenuWithInline(ctx, msg.Chat.ID, "Нужен дедлайн?", deadlineInlineKeyboard())
	case domain.SessionStateCreateDeadline:
		if draft.PendingInput != "deadline" {
			return nil
		}
		dt, noDeadline, err := parseFlexibleDateTime(msg.Text, tz)
		if err != nil {
			return b.sendMenuText(ctx, msg.Chat.ID, "Не понял дату. Примеры: сегодня 18:30, завтра 9, через 2ч, 12.02 18:00")
		}
		if noDeadline {
			draft.Deadline = nil
		} else {
			draft.Deadline = &dt
		}
		draft.PendingInput = ""
		session.State = domain.SessionStateCreateRemind
		if err := b.saveSession(session, draft); err != nil {
			return err
		}
		return b.sendMenuWithInline(ctx, msg.Chat.ID, "Напоминания?", remindInlineKeyboard())
	case domain.SessionStateCreateRemind:
		if draft.PendingInput != "remind_interval" {
			return nil
		}
		dur, err := parseDurationFlexible(strings.TrimSpace(msg.Text))
		if err != nil || dur <= 0 {
			return b.sendMenuText(ctx, msg.Chat.ID, "Формат интервала: 15m / 2h / 1d")
		}
		draft.RemindKind = "interval"
		draft.RemindInterval = dur.String()
		draft.PendingInput = ""
		session.State = domain.SessionStateCreateConfirm
		if err := b.saveSession(session, draft); err != nil {
			return err
		}
		return b.sendMenuWithInline(ctx, msg.Chat.ID, draftSummary(draft), confirmInlineKeyboard())
	case domain.SessionStateCreateConfirm:
		return b.sendMenuWithInline(ctx, msg.Chat.ID, draftSummary(draft), confirmInlineKeyboard())
	case domain.SessionStateCreateEdit:
		return b.sendMenuWithInline(ctx, msg.Chat.ID, "Что хочешь изменить?", editInlineKeyboard())
	default:
		return nil
	}
}

func (b *Bot) handleWizardCallback(ctx context.Context, cb *CallbackQuery, action, args string) error {
	user, err := b.users.GetByTelegramID(cb.From.ID)
	if err != nil {
		_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "Не нашёл пользователя.")
		return err
	}
	tz := user.Timezone
	if tz == "" {
		tz = "UTC"
	}
	session, draft := b.loadSession(user.ID)
	if session.State == domain.SessionStateIdle {
		return b.client.AnswerCallbackQuery(ctx, cb.ID, "")
	}
	switch action {
	case "cancel":
		_ = b.sessions.DeleteSession(user.ID)
		_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "Ок, отменил.")
		return b.sendMenuText(ctx, cb.Message.Chat.ID, "Создание задачи отменено.")
	case "deadline":
		if session.State != domain.SessionStateCreateDeadline {
			return b.client.AnswerCallbackQuery(ctx, cb.ID, "")
		}
		switch args {
		case "none":
			draft.Deadline = nil
			session.State = domain.SessionStateCreateRemind
			if err := b.saveSession(session, draft); err != nil {
				return err
			}
			_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "")
			return b.sendMenuWithInline(ctx, cb.Message.Chat.ID, "Напоминания?", remindInlineKeyboard())
		case "today":
			dt := endOfDay(time.Now(), tz)
			draft.Deadline = &dt
			session.State = domain.SessionStateCreateRemind
			if err := b.saveSession(session, draft); err != nil {
				return err
			}
			_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "Сегодня")
			return b.sendMenuWithInline(ctx, cb.Message.Chat.ID, "Напоминания?", remindInlineKeyboard())
		case "tomorrow":
			dt := endOfDay(time.Now().Add(24*time.Hour), tz)
			draft.Deadline = &dt
			session.State = domain.SessionStateCreateRemind
			if err := b.saveSession(session, draft); err != nil {
				return err
			}
			_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "Завтра")
			return b.sendMenuWithInline(ctx, cb.Message.Chat.ID, "Напоминания?", remindInlineKeyboard())
		case "input":
			draft.PendingInput = "deadline"
			if err := b.saveSession(session, draft); err != nil {
				return err
			}
			_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "")
			return b.sendMenuText(ctx, cb.Message.Chat.ID, "Напиши по‑человечески: сегодня 18:30, завтра 9, через 2ч, 12.02 18:00")
		default:
			return b.client.AnswerCallbackQuery(ctx, cb.ID, "")
		}
	case "remind":
		if session.State != domain.SessionStateCreateRemind {
			return b.client.AnswerCallbackQuery(ctx, cb.ID, "")
		}
		switch args {
		case "none":
			draft.RemindKind = "none"
			draft.RemindInterval = ""
			session.State = domain.SessionStateCreateConfirm
			if err := b.saveSession(session, draft); err != nil {
				return err
			}
			_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "")
			return b.sendMenuWithInline(ctx, cb.Message.Chat.ID, draftSummary(draft), confirmInlineKeyboard())
		case "10m", "1h":
			if draft.Deadline == nil {
				return b.client.AnswerCallbackQuery(ctx, cb.ID, "Нужен дедлайн.")
			}
			draft.RemindKind = "before"
			draft.RemindInterval = args
			session.State = domain.SessionStateCreateConfirm
			if err := b.saveSession(session, draft); err != nil {
				return err
			}
			_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "")
			return b.sendMenuWithInline(ctx, cb.Message.Chat.ID, draftSummary(draft), confirmInlineKeyboard())
		case "2h", "1d":
			draft.RemindKind = "interval"
			draft.RemindInterval = args
			session.State = domain.SessionStateCreateConfirm
			if err := b.saveSession(session, draft); err != nil {
				return err
			}
			_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "")
			return b.sendMenuWithInline(ctx, cb.Message.Chat.ID, draftSummary(draft), confirmInlineKeyboard())
		case "custom":
			draft.PendingInput = "remind_interval"
			if err := b.saveSession(session, draft); err != nil {
				return err
			}
			_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "")
			return b.sendMenuText(ctx, cb.Message.Chat.ID, "Пришли интервал: 15m / 2h / 1d")
		default:
			return b.client.AnswerCallbackQuery(ctx, cb.ID, "")
		}
	case "confirm":
		if session.State != domain.SessionStateCreateConfirm {
			return b.client.AnswerCallbackQuery(ctx, cb.ID, "")
		}
		switch args {
		case "create":
			task, err := b.createTaskFromDraft(user, draft, tz)
			if err != nil {
				_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "Не получилось создать.")
				return err
			}
			_ = b.sessions.DeleteSession(user.ID)
			_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "Создал.")
			if err := b.client.SendMessageWithMarkup(ctx, cb.Message.Chat.ID, formatTaskLine(task), taskInlineKeyboard(task.ID)); err != nil {
				return err
			}
			return b.sendMenuText(ctx, cb.Message.Chat.ID, "Готово! Что дальше?")
		case "edit":
			draft.PendingInput = ""
			session.State = domain.SessionStateCreateEdit
			if err := b.saveSession(session, draft); err != nil {
				return err
			}
			_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "")
			return b.sendMenuWithInline(ctx, cb.Message.Chat.ID, "Что хочешь изменить?", editInlineKeyboard())
		case "cancel":
			_ = b.sessions.DeleteSession(user.ID)
			_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "Ок, отменил.")
			return b.sendMenuText(ctx, cb.Message.Chat.ID, "Создание задачи отменено.")
		default:
			return b.client.AnswerCallbackQuery(ctx, cb.ID, "")
		}
	case "edit":
		if session.State != domain.SessionStateCreateEdit {
			return b.client.AnswerCallbackQuery(ctx, cb.ID, "")
		}
		switch args {
		case "text":
			session.State = domain.SessionStateCreateText
			if err := b.saveSession(session, draft); err != nil {
				return err
			}
			_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "")
			return b.client.SendMessageWithMarkup(ctx, cb.Message.Chat.ID, "Ок, напиши новый текст задачи.", &ForceReply{
				ForceReply:            true,
				InputFieldPlaceholder: "Текст задачи",
			})
		case "deadline":
			session.State = domain.SessionStateCreateDeadline
			if err := b.saveSession(session, draft); err != nil {
				return err
			}
			_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "")
			return b.sendMenuWithInline(ctx, cb.Message.Chat.ID, "Нужен дедлайн?", deadlineInlineKeyboard())
		case "remind":
			session.State = domain.SessionStateCreateRemind
			if err := b.saveSession(session, draft); err != nil {
				return err
			}
			_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "")
			return b.sendMenuWithInline(ctx, cb.Message.Chat.ID, "Напоминания?", remindInlineKeyboard())
		default:
			return b.client.AnswerCallbackQuery(ctx, cb.ID, "")
		}
	default:
		return b.client.AnswerCallbackQuery(ctx, cb.ID, "")
	}
}

func deadlineInlineKeyboard() *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "Без дедлайна", CallbackData: "w:deadline:none"},
				{Text: "Сегодня", CallbackData: "w:deadline:today"},
			},
			{
				{Text: "Завтра", CallbackData: "w:deadline:tomorrow"},
				{Text: "Ввести дату…", CallbackData: "w:deadline:input"},
			},
			{
				{Text: "Отмена", CallbackData: "w:cancel"},
			},
		},
	}
}

func remindInlineKeyboard() *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "Не напоминать", CallbackData: "w:remind:none"},
			},
			{
				{Text: "За 10 мин", CallbackData: "w:remind:10m"},
				{Text: "За 1 час", CallbackData: "w:remind:1h"},
			},
			{
				{Text: "Каждые 2 часа", CallbackData: "w:remind:2h"},
				{Text: "Каждый день", CallbackData: "w:remind:1d"},
			},
			{
				{Text: "Свой интервал…", CallbackData: "w:remind:custom"},
				{Text: "Отмена", CallbackData: "w:cancel"},
			},
		},
	}
}

func confirmInlineKeyboard() *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "✅ Создать", CallbackData: "w:confirm:create"},
				{Text: "✏️ Изменить", CallbackData: "w:confirm:edit"},
			},
			{
				{Text: "❌ Отмена", CallbackData: "w:confirm:cancel"},
			},
		},
	}
}

func editInlineKeyboard() *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "✏️ Текст", CallbackData: "w:edit:text"},
				{Text: "⏰ Дедлайн", CallbackData: "w:edit:deadline"},
			},
			{
				{Text: "🔔 Напоминания", CallbackData: "w:edit:remind"},
				{Text: "❌ Отмена", CallbackData: "w:confirm:cancel"},
			},
		},
	}
}

func parseWizardCallback(data string) (string, string, bool) {
	parts := strings.Split(data, ":")
	if len(parts) < 2 || parts[0] != "w" {
		return "", "", false
	}
	if len(parts) == 2 && parts[1] == "cancel" {
		return "cancel", "", true
	}
	if len(parts) != 3 {
		return "", "", false
	}
	return parts[1], parts[2], true
}

type taskDraft struct {
	Text           string     `json:"text"`
	Deadline       *time.Time `json:"deadline,omitempty"`
	RemindKind     string     `json:"remind_kind,omitempty"`
	RemindInterval string     `json:"remind_interval,omitempty"`
	PendingInput   string     `json:"pending_input,omitempty"`
}

func (b *Bot) loadSession(userID int64) (domain.UserSession, taskDraft) {
	session, err := b.sessions.GetSession(userID)
	if err != nil {
		return domain.UserSession{UserID: userID, State: domain.SessionStateIdle}, taskDraft{}
	}
	var draft taskDraft
	if session.DraftJSON != "" {
		_ = json.Unmarshal([]byte(session.DraftJSON), &draft)
	}
	return session, draft
}

func (b *Bot) saveSession(session domain.UserSession, draft taskDraft) error {
	data, err := json.Marshal(draft)
	if err != nil {
		return err
	}
	session.DraftJSON = string(data)
	return b.sessions.UpsertSession(session)
}

func draftSummary(draft taskDraft) string {
	var deadline string
	if draft.Deadline != nil {
		deadline = draft.Deadline.Format("2006-01-02 15:04")
	} else {
		deadline = "без дедлайна"
	}
	remind := "не напоминать"
	switch draft.RemindKind {
	case "before":
		remind = "за " + draft.RemindInterval + " до дедлайна"
	case "interval":
		remind = "каждые " + draft.RemindInterval
	}
	return fmt.Sprintf("Проверь:\nТекст: %s\nДедлайн: %s\nНапоминания: %s", draft.Text, deadline, remind)
}

func (b *Bot) createTaskFromDraft(user domain.User, draft taskDraft, tz string) (domain.Task, error) {
	var remindAt *time.Time
	switch draft.RemindKind {
	case "before":
		if draft.Deadline == nil {
			return domain.Task{}, errors.New("deadline required")
		}
		dur, err := parseDurationFlexible(draft.RemindInterval)
		if err != nil {
			return domain.Task{}, err
		}
		rt := draft.Deadline.Add(-dur)
		remindAt = &rt
	case "interval":
		dur, err := parseDurationFlexible(draft.RemindInterval)
		if err != nil {
			return domain.Task{}, err
		}
		rt := time.Now().In(draft.DeadlineLocationOrUTC(tz)).Add(dur)
		remindAt = &rt
	}
	return b.taskService.Create(user.ID, draft.Text, draft.Deadline, remindAt, tz)
}

func (d taskDraft) DeadlineLocationOrUTC(tz string) *time.Location {
	if d.Deadline != nil {
		return d.Deadline.Location()
	}
	loc, err := usecase.LocationFromTZ(tz)
	if err != nil {
		return time.UTC
	}
	return loc
}

func endOfDay(now time.Time, tz string) time.Time {
	loc, err := usecase.LocationFromTZ(tz)
	if err != nil {
		loc = time.UTC
	}
	year, month, day := now.In(loc).Date()
	return time.Date(year, month, day, 23, 59, 0, 0, loc)
}
