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
	return b.client.SendMessageWithMarkup(ctx, chatID, "Отправь текст, пересланный пост или файл/картинку.", &ForceReply{
		ForceReply:            true,
		InputFieldPlaceholder: "Текст задачи / пересланный пост / файл",
	})
}

func (b *Bot) handleWizardMessage(ctx context.Context, msg *Message, user domain.User, tz string, session domain.UserSession, draft taskDraft) error {
	switch session.State {
	case domain.SessionStateCreateText:
		content, ok := extractWizardContent(msg)
		if !ok {
			return b.sendMenuText(ctx, msg.Chat.ID, "Пришли текст, пересланный пост или файл/картинку.")
		}
		if content.text != "" {
			draft.Text = content.text
		}
		if content.attachmentsFound {
			draft.Attachments = content.attachments
		}
		if content.forwardFound {
			draft.ForwardMeta = content.forwardMeta
		}
		if draft.Text == "" {
			draft.Text = "Вложение без текста"
		}
		session.State = domain.SessionStateCreateDeadline
		if err := b.saveSession(session, draft); err != nil {
			return err
		}
		return b.sendMenuWithInline(ctx, msg.Chat.ID, "Нужен дедлайн?", deadlineInlineKeyboard())
	case domain.SessionStateCreateDeadline:
		switch draft.PendingInput {
		case "deadline":
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
			draft.PendingDate = ""
		case "deadline_time":
			h, m, ok := parseTimeOfDay(msg.Text)
			if !ok {
				return b.sendMenuText(ctx, msg.Chat.ID, "Время в формате 17:00 или 17.00")
			}
			loc, err := usecase.LocationFromTZ(tz)
			if err != nil {
				return err
			}
			date, err := time.ParseInLocation("2006-01-02", draft.PendingDate, loc)
			if err != nil {
				return b.sendMenuText(ctx, msg.Chat.ID, "Не понял дату, попробуй ещё раз.")
			}
			dt := time.Date(date.Year(), date.Month(), date.Day(), h, m, 0, 0, loc)
			draft.Deadline = &dt
			draft.PendingInput = ""
			draft.PendingDate = ""
		default:
			return nil
		}
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
			return b.sendMenuText(ctx, msg.Chat.ID, "Формат интервала: 15m, 2h, 1d")
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
			date := time.Now().In(usecaseTimeLocation(tz))
			draft.PendingInput = "deadline_time"
			draft.PendingDate = date.Format("2006-01-02")
			if err := b.saveSession(session, draft); err != nil {
				return err
			}
			_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "Сегодня")
			return b.sendMenuText(ctx, cb.Message.Chat.ID, "Введи время (например 17:00 или 17.00)")
		case "tomorrow":
			date := time.Now().In(usecaseTimeLocation(tz)).Add(24 * time.Hour)
			draft.PendingInput = "deadline_time"
			draft.PendingDate = date.Format("2006-01-02")
			if err := b.saveSession(session, draft); err != nil {
				return err
			}
			_ = b.client.AnswerCallbackQuery(ctx, cb.ID, "Завтра")
			return b.sendMenuText(ctx, cb.Message.Chat.ID, "Введи время (например 17:00 или 17.00)")
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
			return b.sendMenuText(ctx, cb.Message.Chat.ID, "Пришли интервал: 15m, 2h, 1d")
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
			if err := b.client.SendMessageWithMarkup(ctx, cb.Message.Chat.ID, formatTaskLineWithAttachments(task, len(draft.Attachments)), taskInlineKeyboard(task.ID)); err != nil {
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

type draftAttachment struct {
	Type           string `json:"type"`
	TelegramFileID string `json:"telegram_file_id"`
	FileUniqueID   string `json:"file_unique_id"`
	Caption        string `json:"caption,omitempty"`
	MimeType       string `json:"mime_type,omitempty"`
	FileName       string `json:"file_name,omitempty"`
	FileSize       int64  `json:"file_size,omitempty"`
}

type taskDraft struct {
	Text           string            `json:"text"`
	Deadline       *time.Time        `json:"deadline,omitempty"`
	RemindKind     string            `json:"remind_kind,omitempty"`
	RemindInterval string            `json:"remind_interval,omitempty"`
	PendingInput   string            `json:"pending_input,omitempty"`
	PendingDate    string            `json:"pending_date,omitempty"`
	ForwardMeta    json.RawMessage   `json:"forward_meta,omitempty"`
	Attachments    []draftAttachment `json:"attachments,omitempty"`
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

type wizardContent struct {
	text             string
	attachments      []draftAttachment
	attachmentsFound bool
	forwardMeta      json.RawMessage
	forwardFound     bool
}

func extractWizardContent(msg *Message) (wizardContent, bool) {
	var content wizardContent
	if msg == nil {
		return content, false
	}
	text := strings.TrimSpace(msg.Text)
	caption := strings.TrimSpace(msg.Caption)
	if text != "" {
		content.text = text
	} else if caption != "" {
		content.text = caption
	}
	if len(msg.Photo) > 0 {
		p := pickLargestPhoto(msg.Photo)
		content.attachments = append(content.attachments, draftAttachment{
			Type:           "photo",
			TelegramFileID: p.FileID,
			FileUniqueID:   p.FileUniqueID,
			Caption:        caption,
			FileSize:       p.FileSize,
		})
		content.attachmentsFound = true
	}
	if msg.Document != nil {
		d := msg.Document
		content.attachments = append(content.attachments, draftAttachment{
			Type:           "document",
			TelegramFileID: d.FileID,
			FileUniqueID:   d.FileUniqueID,
			Caption:        caption,
			MimeType:       d.MimeType,
			FileName:       d.FileName,
			FileSize:       d.FileSize,
		})
		content.attachmentsFound = true
	}
	if msg.ForwardOrigin != nil {
		data, err := json.Marshal(msg.ForwardOrigin)
		if err == nil {
			content.forwardMeta = data
			content.forwardFound = true
		}
	}
	if content.text == "" {
		if content.forwardFound {
			content.text = "Пересланное сообщение"
		} else if content.attachmentsFound {
			content.text = "Вложение без текста"
		}
	}
	if content.text == "" && !content.attachmentsFound && !content.forwardFound {
		return content, false
	}
	return content, true
}

func pickLargestPhoto(items []PhotoSize) PhotoSize {
	best := items[0]
	bestSize := best.FileSize
	for _, p := range items[1:] {
		size := p.FileSize
		if size == 0 {
			size = int64(p.Width * p.Height)
		}
		if size > bestSize {
			best = p
			bestSize = size
		}
	}
	return best
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
	attachments := "нет"
	if len(draft.Attachments) > 0 {
		attachments = fmt.Sprintf("%d", len(draft.Attachments))
	}
	forward := "нет"
	if len(draft.ForwardMeta) > 0 {
		forward = "есть"
	}
	return fmt.Sprintf("Проверь:\nТекст: %s\nДедлайн: %s\nНапоминания: %s\nВложения: %s\nФорвард: %s", draft.Text, deadline, remind, attachments, forward)
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
	created, err := b.taskService.Create(user.ID, draft.Text, draft.Deadline, remindAt, draft.ForwardMeta, tz)
	if err != nil {
		return domain.Task{}, err
	}
	for _, a := range draft.Attachments {
		_, err := b.attachments.CreateAttachment(domain.Attachment{
			TaskID:         created.ID,
			Type:           a.Type,
			TelegramFileID: a.TelegramFileID,
			FileUniqueID:   a.FileUniqueID,
			Caption:        a.Caption,
			MimeType:       a.MimeType,
			FileName:       a.FileName,
			FileSize:       a.FileSize,
		})
		if err != nil {
			return domain.Task{}, err
		}
	}
	return created, nil
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

func usecaseTimeLocation(tz string) *time.Location {
	loc, err := usecase.LocationFromTZ(tz)
	if err != nil {
		return time.UTC
	}
	return loc
}
