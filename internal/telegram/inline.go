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

func formatTaskLine(t domain.Task) string {
	line := fmt.Sprintf("%d) %s", t.ID, t.Text)
	if t.DueAt != nil {
		line += " — до " + formatTime(t.DueAt)
	}
	return line
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
				{Text: "⏰ Напоминания / Дедлайн", CallbackData: fmt.Sprintf("t:rem:%d", id)},
			},
			{
				{Text: "💤 Snooze 15м", CallbackData: fmt.Sprintf("t:snooze:%d", id)},
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
	if len(parts) != 3 || parts[0] != "t" {
		return "", 0, false
	}
	id, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || id <= 0 {
		return "", 0, false
	}
	switch parts[1] {
	case "done", "del", "rem", "snooze":
		return parts[1], id, true
	default:
		return "", 0, false
	}
}
