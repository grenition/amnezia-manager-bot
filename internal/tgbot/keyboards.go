package tgbot

import (
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const (
	btnNewConfig = "✨ Новый конфиг"
	btnDevices   = "📱 Мои устройства"
	btnHelp      = "📖 Инструкция"
	btnSupport   = "🆘 Помощь"
	btnUsers     = "👑 Пользователи"
	btnAddUser   = "➕ Добавить"
	btnDisable   = "⛔️ Отключить"
	btnLimit     = "🔢 Лимит"
)

func button(text string) tgbotapi.KeyboardButton {
	return tgbotapi.KeyboardButton{Text: text}
}

func userRows() [][]tgbotapi.KeyboardButton {
	return [][]tgbotapi.KeyboardButton{
		{button(btnNewConfig), button(btnDevices)},
		{button(btnHelp), button(btnSupport)},
	}
}

func adminRows() [][]tgbotapi.KeyboardButton {
	return [][]tgbotapi.KeyboardButton{
		{button(btnUsers)},
		{button(btnAddUser), button(btnDisable), button(btnLimit)},
	}
}

func userKeyboard() tgbotapi.ReplyKeyboardMarkup {
	kb := tgbotapi.NewReplyKeyboard(userRows()...)
	kb.ResizeKeyboard = true
	return kb
}

func adminKeyboard() tgbotapi.ReplyKeyboardMarkup {
	kb := tgbotapi.NewReplyKeyboard(append(adminRows(), userRows()...)...)
	kb.ResizeKeyboard = true
	return kb
}

var buttonLabels = map[string]struct{}{
	btnNewConfig: {},
	btnDevices:   {},
	btnHelp:      {},
	btnSupport:   {},
	btnUsers:     {},
	btnAddUser:   {},
	btnDisable:   {},
	btnLimit:     {},
}

func isButton(text string) bool {
	_, ok := buttonLabels[text]
	return ok
}
