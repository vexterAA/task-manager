package telegram

import "strings"

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

func helpText() string {
	return strings.Join([]string{
		"Коротко: бот помогает добавлять задачи, ставить дедлайны и напоминания.",
		"Управление — через кнопки под задачей.",
		"",
		"Кнопки:",
		"➕ Создать задачу — мастер добавления.",
		"📋 Мои задачи — список активных.",
		"⏰ Сегодня и просрочено — быстрый фильтр (в разработке).",
		"⚙️ Настройки — таймзона и дефолтные напоминания (в разработке).",
		"❓ Помощь — сюда.",
	}, "\n")
}

func mainMenuMarkup() *ReplyKeyboardMarkup {
	return &ReplyKeyboardMarkup{
		ResizeKeyboard: true,
		IsPersistent:   true,
		Keyboard: [][]KeyboardButton{
			{{Text: "➕ Создать задачу"}},
			{{Text: "📋 Мои задачи"}},
			{{Text: "⏰ Сегодня и просрочено"}},
			{{Text: "⚙️ Настройки"}},
			{{Text: "❓ Помощь"}},
		},
	}
}

func mapMenuCommand(text string) (string, string) {
	switch strings.TrimSpace(text) {
	case "➕ Создать задачу":
		return "add", ""
	case "📋 Мои задачи":
		return "list", ""
	case "⏰ Сегодня и просрочено":
		return "today", ""
	case "⚙️ Настройки":
		return "settings", ""
	case "❓ Помощь":
		return "help", ""
	default:
		return "", ""
	}
}
