package telegram

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"example.com/yourapp/internal/domain"
)

func formatTaskList(items []domain.Task) string {
	if len(items) == 0 {
		return "Пока пусто. Жми «Создать задачу» в меню."
	}
	lines := make([]string, 0, len(items)+1)
	lines = append(lines, "Активные задачи:")
	for _, t := range items {
		line := formatTaskLineWithAttachments(t, 0)
		if t.DueAt != nil {
			line += " — до " + formatTime(t.DueAt)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func formatTaskLine(t domain.Task) string {
	return formatTaskLineWithAttachments(t, 0)
}

func formatTaskLineWithAttachments(t domain.Task, attachments int) string {
	line := fmt.Sprintf("Задача #%d: %s", t.ID, taskLabel(t))
	if t.DueAt != nil {
		line += " — до " + formatTime(t.DueAt)
	}
	if attachments > 0 {
		line += fmt.Sprintf(" · 📎 %d", attachments)
	}
	return line
}

func formatTaskSummaryLine(index int, t domain.Task, attachments int) string {
	line := fmt.Sprintf("%d. %s", index, taskLabel(t))
	if t.DueAt != nil {
		line += " — до " + formatTime(t.DueAt)
	}
	if attachments > 0 {
		line += fmt.Sprintf(" · 📎 %d", attachments)
	}
	return line
}

func taskLabel(t domain.Task) string {
	if strings.TrimSpace(t.Title) != "" {
		return t.Title
	}
	return t.Text
}

func formatTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02 15:04")
}

func taskInlineKeyboard(id int64) *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "✅ Готово", CallbackData: fmt.Sprintf("t:done:%d", id)},
				{Text: "🗑 Удалить", CallbackData: fmt.Sprintf("t:del:%d", id)},
			},
			{
				{Text: "⏰ Дедлайн и напоминания", CallbackData: fmt.Sprintf("t:rem:%d", id)},
			},
			{
				{Text: "💤 Snooze 15м", CallbackData: fmt.Sprintf("t:snooze:%d", id)},
			},
			{
				{Text: "📎 Вложения", CallbackData: fmt.Sprintf("t:att:%d", id)},
			},
		},
	}
}

func listPickInlineKeyboard() *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "🔢 Ввести номер", CallbackData: "t:pick"},
			},
		},
	}
}

func doneInlineKeyboard() *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "✅ выполнено", CallbackData: "noop"},
			},
		},
	}
}

func deletedInlineKeyboard() *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "🗑 удалено", CallbackData: "noop"},
			},
		},
	}
}

func parseTaskCallback(data string) (string, int64, bool) {
	parts := strings.Split(data, ":")
	if len(parts) == 2 && parts[0] == "t" && parts[1] == "pick" {
		return "pick", 0, true
	}
	if len(parts) != 3 || parts[0] != "t" {
		return "", 0, false
	}
	id, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || id <= 0 {
		return "", 0, false
	}
	switch parts[1] {
	case "done", "del", "rem", "snooze", "att":
		return parts[1], id, true
	default:
		return "", 0, false
	}
}
