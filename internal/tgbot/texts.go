package tgbot

import (
	"errors"

	"amnezia-manager-bot/internal/service"
)

const (
	textNoAccess       = "Нет доступа."
	textMenu           = "Главное меню:"
	textAskDeviceName  = "Введите имя устройства (3–32 символа: латинские буквы, цифры, «-», «_»)."
	textAskComplaint   = "Опишите проблему одним сообщением."
	textConfigOnce     = "Файл выдаётся один раз и повторно не высылается. Сохраните его. Потерянный конфиг нужно удалить и создать заново."
	textComplaintSent  = "Обращение отправлено администраторам."
	textInstruction    = "Как подключиться:\n1. Установите приложение AmneziaWG (amnezia.org; iOS — App Store, Android — Google Play).\n2. Откройте полученный .conf файл в приложении (Add tunnel → Import file(s)).\n3. Включите туннель.\n\nЕсли конфиг потерян — удалите устройство в боте и создайте заново."
	textDeleted        = "Конфиг удалён."
	textUnknownCommand = "Неизвестная команда."
	textServiceDown    = "Сервис временно недоступен, попробуйте позже."
)

// userMessage переводит ошибки сервиса в тексты без внутренних деталей.
func userMessage(err error) string {
	switch {
	case errors.Is(err, service.ErrNoAccess):
		return textNoAccess
	case errors.Is(err, service.ErrLimitReached):
		return "Достигнут лимит активных конфигов. Удалите ненужный или обратитесь к администратору."
	case errors.Is(err, service.ErrBadDeviceName):
		return "Некорректное имя устройства: 3–32 символа, латинские буквы, цифры, «-», «_»."
	case errors.Is(err, service.ErrNotFound):
		return "Конфиг не найден."
	case errors.Is(err, service.ErrBadLimit):
		return "Лимит должен быть числом от 1 до 50."
	default:
		return textServiceDown
	}
}
