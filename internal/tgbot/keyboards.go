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
	btnServers   = "📊 Серверы"
	btnAddUser   = "➕ Добавить"
	btnDisable   = "⛔️ Отключить"
	btnLimit     = "🔢 Лимит"
)

func button(text string) tgbotapi.KeyboardButton {
	return tgbotapi.KeyboardButton{Text: text}
}

func adminRows() [][]tgbotapi.KeyboardButton {
	return [][]tgbotapi.KeyboardButton{
		{button(btnUsers), button(btnServers)},
		{button(btnAddUser), button(btnDisable), button(btnLimit)},
	}
}

func userKeyboard() tgbotapi.ReplyKeyboardMarkup {
	kb := tgbotapi.NewReplyKeyboard(userRows()...)
	kb.ResizeKeyboard = true
	return kb
}

func userRows() [][]tgbotapi.KeyboardButton {
	return [][]tgbotapi.KeyboardButton{
		{button(btnNewConfig), button(btnDevices)},
		{button(btnHelp), button(btnSupport)},
	}
}

func adminKeyboard() tgbotapi.ReplyKeyboardMarkup {
	rows := append(adminRows(),
		[]tgbotapi.KeyboardButton{button(btnNewConfig), button(btnDevices)},
		[]tgbotapi.KeyboardButton{button(btnHelp)},
	)
	kb := tgbotapi.NewReplyKeyboard(rows...)
	kb.ResizeKeyboard = true
	return kb
}

var buttonLabels = map[string]struct{}{
	btnNewConfig: {},
	btnDevices:   {},
	btnHelp:      {},
	btnSupport:   {},
	btnUsers:     {},
	btnServers:   {},
	btnAddUser:   {},
	btnDisable:   {},
	btnLimit:     {},
}

func isButton(text string) bool {
	_, ok := buttonLabels[text]
	return ok
}
