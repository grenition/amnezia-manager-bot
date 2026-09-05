package tgbot

import (
	"context"
	"fmt"
	"html"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"amnezia-manager-bot/internal/service"
	"amnezia-manager-bot/internal/store"
)

const (
	textAdminAskUsername        = "➕ <b>Добавить пользователя</b>\n\nПришлите @username.\n\nЧеловек должен сначала открыть бота и нажать <b>Start</b> — иначе я его не найду."
	textAdminAskUsernameDisable = "⛔️ <b>Отключить пользователя</b>\n\nПришлите @username. Все его конфиги будут отозваны."
	textAdminAskUsernameLimit   = "🔢 <b>Изменить лимит</b>\n\nПришлите @username пользователя."
	textAdminNotFound           = "😔 Не нашёл @%s.\n\nПопросите человека открыть бота, нажать Start и повторите."
)

func knownUserLine(u store.KnownUser) string {
	name := strings.TrimSpace(u.FirstName)
	if name == "" {
		name = "@" + u.Username
	}
	return fmt.Sprintf("<b>%s</b> · @%s · <code>%d</code>", html.EscapeString(name), html.EscapeString(u.Username), u.TelegramID)
}

func (b *Bot) adminResolveUser(ctx context.Context, uid int64, chatID int64, input, purpose string) {
	username := strings.TrimPrefix(strings.TrimSpace(input), "@")
	username = strings.TrimSuffix(username, "@")
	if username == "" || strings.Contains(username, " ") {
		b.sendHTML(chatID, "Пришлите @username одним сообщением.")
		return
	}
	known, err := b.svc.FindKnownUser(ctx, username)
	if err != nil {
		b.sendHTML(chatID, fmt.Sprintf(textAdminNotFound, html.EscapeString(username)))
		return
	}
	switch purpose {
	case "add":
		kb := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ Добавить", fmt.Sprintf("admadd:%d:%s", known.TelegramID, known.Username)),
				tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", "admno"),
			),
		)
		b.sendConfirm(chatID, "➕ Добавить доступ?\n\n"+knownUserLine(known), &kb)
	case "disable":
		kb := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ Отключить", fmt.Sprintf("admdis:%d:%s", known.TelegramID, known.Username)),
				tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", "admno"),
			),
		)
		b.sendConfirm(chatID, "⛔️ Отключить пользователя?\n\n"+knownUserLine(known)+"\n\nВсе его конфиги будут отозваны.", &kb)
	case "limit":
		b.st.setTarget(uid, stateAdminLimitValue, known.TelegramID, known.Username)
		b.sendConfirm(chatID, "🔢 Введите новый лимит для\n"+knownUserLine(known)+"\n\nОт 1 до 1000.", nil)
	}
}

func (b *Bot) sendConfirm(chatID int64, text string, kb *tgbotapi.InlineKeyboardMarkup) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = tgbotapi.ModeHTML
	if kb != nil {
		msg.ReplyMarkup = kb
	}
	if _, err := b.api.Send(msg); err != nil {
		b.log.Error("send confirm failed", "err", err)
	}
}

func (b *Bot) adminAddConfirm(ctx context.Context, chatID, msgID int64, idStr, username string) {
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		b.editHTML(chatID, msgID, userMessage(service.ErrNotFound), nil)
		return
	}
	if _, err := b.svc.AdminAddUser(ctx, id, username); err != nil {
		b.log.Error("admin add failed", "err", err)
		b.editHTML(chatID, msgID, userMessage(err), nil)
		return
	}
	b.editHTML(chatID, msgID, "✅ @<code>"+html.EscapeString(username)+"</code> добавлен.", nil)
}

func (b *Bot) adminDisableConfirm(ctx context.Context, chatID, msgID int64, idStr, username string) {
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		b.editHTML(chatID, msgID, userMessage(service.ErrNotFound), nil)
		return
	}
	if err := b.svc.AdminDisableUser(ctx, id); err != nil {
		b.log.Error("admin disable failed", "err", err)
		b.editHTML(chatID, msgID, userMessage(err), nil)
		return
	}
	b.editHTML(chatID, msgID, "✅ @<code>"+html.EscapeString(username)+"</code> отключён, конфиги отозваны.", nil)
}

func (b *Bot) adminLimitValue(ctx context.Context, uid int64, chatID int64, input string) {
	id, username := b.st.target(uid)
	if id == 0 {
		b.sendHTML(chatID, "Сначала пришлите @username — кнопка 🔢 <b>Лимит</b>.")
		return
	}
	limit, err := strconv.Atoi(strings.TrimSpace(input))
	if err != nil {
		b.sendHTML(chatID, "Лимит — число от 1 до 1000. Попробуйте ещё раз.")
		return
	}
	if err := b.svc.AdminSetLimit(ctx, id, limit); err != nil {
		b.log.Error("admin set limit failed", "err", err)
		b.sendHTML(chatID, userMessage(err))
		return
	}
	b.sendHTML(chatID, fmt.Sprintf("✅ Лимит @<code>%s</code>: %d конфигов.", html.EscapeString(username), limit))
}

func (b *Bot) cmdUsersList(ctx context.Context, chatID int64) {
	users, err := b.svc.AdminListUsers(ctx)
	if err != nil {
		b.log.Error("users failed", "err", err)
		b.sendHTML(chatID, textServiceDown)
		return
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "👑 <b>Пользователи · %d</b>\n\n", len(users))
	for _, u := range users {
		who := "@" + u.Username
		if u.Username == "" {
			who = fmt.Sprintf("<code>%d</code>", u.TelegramID)
		} else {
			who = "@" + html.EscapeString(u.Username)
		}
		status := "✅"
		if !u.Enabled {
			status = "⛔️"
		}
		fmt.Fprintf(&sb, "%s %s · %d конф.\n", status, who, u.ActiveConfigs)
	}
	b.sendHTML(chatID, sb.String())
}

func (b *Bot) cmdAddUser(ctx context.Context, m *tgbotapi.Message) {
	chatID := int64(m.Chat.ID)
	args := strings.Fields(m.CommandArguments())
	if len(args) == 0 {
		b.sendHTML(chatID, "Использование: /adduser <telegram_id> [username]")
		return
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id <= 0 {
		b.sendHTML(chatID, "Некорректный Telegram ID.")
		return
	}
	username := ""
	if len(args) > 1 {
		username = args[1]
	}
	if _, err := b.svc.AdminAddUser(ctx, id, username); err != nil {
		b.log.Error("adduser failed", "err", err)
		b.sendHTML(chatID, userMessage(err))
		return
	}
	b.sendHTML(chatID, fmt.Sprintf("✅ Пользователь <code>%d</code> добавлен.", id))
}

func (b *Bot) cmdDisableUser(ctx context.Context, m *tgbotapi.Message) {
	chatID := int64(m.Chat.ID)
	id, err := strconv.ParseInt(strings.TrimSpace(m.CommandArguments()), 10, 64)
	if err != nil || id <= 0 {
		b.sendHTML(chatID, "Использование: /disableuser <telegram_id>")
		return
	}
	if err := b.svc.AdminDisableUser(ctx, id); err != nil {
		b.log.Error("disableuser failed", "err", err)
		b.sendHTML(chatID, userMessage(err))
		return
	}
	b.sendHTML(chatID, fmt.Sprintf("✅ Пользователь <code>%d</code> отключён.", id))
}

func (b *Bot) cmdSetLimit(ctx context.Context, m *tgbotapi.Message) {
	chatID := int64(m.Chat.ID)
	args := strings.Fields(m.CommandArguments())
	if len(args) != 2 {
		b.sendHTML(chatID, "Использование: /setlimit <telegram_id> <limit>")
		return
	}
	id, err1 := strconv.ParseInt(args[0], 10, 64)
	limit, err2 := strconv.Atoi(args[1])
	if err1 != nil || err2 != nil || id <= 0 {
		b.sendHTML(chatID, "Некорректные аргументы.")
		return
	}
	if err := b.svc.AdminSetLimit(ctx, id, limit); err != nil {
		b.log.Error("setlimit failed", "err", err)
		b.sendHTML(chatID, userMessage(err))
		return
	}
	b.sendHTML(chatID, fmt.Sprintf("✅ Лимит пользователя <code>%d</code>: %d.", id, limit))
}
