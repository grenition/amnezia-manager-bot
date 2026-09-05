package tgbot

import (
	"errors"
	"html"
	"strconv"
	"strings"

	"amnezia-manager-bot/internal/service"
)

const (
	textWelcomeUser   = "🛡 <b>Amnezia VPN</b>\n\nУправляйте конфигами кнопками под полем ввода:\n\n✨ <b>Новый конфиг</b> — создать конфиг устройства\n📱 <b>Мои устройства</b> — список и удаление\n📖 <b>Инструкция</b> — как подключиться\n🆘 <b>Помощь</b> — написать администраторам"
	textWelcomeAdmin  = "\n\n👑 <b>Режим администратора</b>\nДоступны кнопки: Пользователи · Добавить · Отключить · Лимит"
	textNoAccess      = "🚫 Нет доступа.\n\nПопросите администратора добавить ваш аккаунт."
	textAskDeviceName = "✨ Как назовём устройство?\n\n3–32 символа: латинские буквы, цифры, дефис, подчёркивание.\nНапример: <code>iphone</code>, <code>work-laptop</code>"
	textAskComplaint  = "🆘 Опишите проблему одним сообщением — я передам его администраторам."
	textComplaintSent = "✅ Обращение отправлено. Администраторы уведомлены."
	textDeleted       = "🗑 Конфиг удалён."
	textServiceDown   = "😔 Сервис временно недоступен, попробуйте позже."
	textFallback      = "Не понял сообщение 🤔\nИспользуйте кнопки под полем ввода или /help."
)

const textInstruction = "📖 <b>Как подключиться</b>\n\n<b>1. Установите приложение AmneziaWG</b>\n• Windows / macOS / Linux — <a href=\"https://amnezia.org\">amnezia.org</a>\n• iOS — App Store, поиск «AmneziaWG»\n• Android — Google Play, поиск «AmneziaWG»\n\n<b>2. Импортируйте конфиг</b>\nСохраните присланный ботом файл <code>.conf</code> и откройте его в приложении:\n<i>Add tunnel → Import file(s)</i> на ПК или «+» на телефоне.\n\n<b>3. Включите туннель</b>\nРоссийские сайты и приложения пойдут напрямую, всё остальное — через VPN.\n\n💡 Потеряли конфиг? Удалите устройство в 📱 <b>Мои устройства</b> и создайте заново — файл выдаётся один раз."

func configCaption(name string) string {
	return "✅ <b>«" + html.EscapeString(name) + "» готов</b>\n\nОткройте файл в AmneziaWG: <i>Add tunnel → Import file(s)</i> — и включите туннель.\n\n⚠️ Файл выдаётся один раз. Потеряли — удалите устройство и создайте заново."
}

func devicesText(names []string) string {
	if len(names) == 0 {
		return "📱 <b>Ваши устройства</b>\n\nКонфигов пока нет. Создайте первый — кнопка ✨ <b>Новый конфиг</b>."
	}
	var sb strings.Builder
	sb.WriteString("📱 <b>Ваши устройства</b>\n\n")
	for i, n := range names {
		sb.WriteString(strconv.Itoa(i+1) + ". <code>" + html.EscapeString(n) + "</code>\n")
	}
	sb.WriteString("\nУдаление — кнопкой под списком.")
	return sb.String()
}

func deleteConfirmText(name string) string {
	return "🗑 Удалить конфиг <b>" + html.EscapeString(name) + "</b>?\n\nPeer будет отозван на сервере, подключение оборвётся."
}

func userMessage(err error) string {
	switch {
	case errors.Is(err, service.ErrNoAccess):
		return textNoAccess
	case errors.Is(err, service.ErrLimitReached):
		return "😔 Достигнут лимит конфигов.\nУдалите ненужное устройство в 📱 <b>Мои устройства</b> или обратитесь к администратору."
	case errors.Is(err, service.ErrBadDeviceName):
		return "Имя устройства: 3–32 символа, латиница, цифры, дефис, подчёркивание.\n\nПопробуйте ещё раз — кнопка ✨ <b>Новый конфиг</b>."
	case errors.Is(err, service.ErrNotFound):
		return "Конфиг не найден. Обновите список — 📱 <b>Мои устройства</b>."
	case errors.Is(err, service.ErrBadLimit):
		return "Лимит — число от 1 до 1000."
	default:
		return textServiceDown
	}
}
