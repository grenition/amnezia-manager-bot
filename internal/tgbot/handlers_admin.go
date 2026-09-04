package tgbot

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (b *Bot) cmdAddUser(ctx context.Context, m *tgbotapi.Message) {
	chatID := int64(m.Chat.ID)
	args := strings.Fields(m.CommandArguments())
	if len(args) == 0 {
		b.sendText(chatID, "Использование: /adduser <telegram_id> [username]")
		return
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id <= 0 {
		b.sendText(chatID, "Некорректный Telegram ID.")
		return
	}
	username := ""
	if len(args) > 1 {
		username = args[1]
	}
	u, err := b.svc.AdminAddUser(ctx, id, username)
	if err != nil {
		b.log.Error("adduser failed", "err", err)
		b.sendText(chatID, userMessage(err))
		return
	}
	b.sendText(chatID, fmt.Sprintf("Пользователь %d добавлен, лимит %d.", u.TelegramID, u.ConfigLimit))
}

func (b *Bot) cmdDisableUser(ctx context.Context, m *tgbotapi.Message) {
	chatID := int64(m.Chat.ID)
	id, err := strconv.ParseInt(strings.TrimSpace(m.CommandArguments()), 10, 64)
	if err != nil || id <= 0 {
		b.sendText(chatID, "Использование: /disableuser <telegram_id>")
		return
	}
	if err := b.svc.AdminDisableUser(ctx, id); err != nil {
		b.log.Error("disableuser failed", "err", err)
		b.sendText(chatID, userMessage(err))
		return
	}
	b.sendText(chatID, fmt.Sprintf("Пользователь %d отключён.", id))
}

func (b *Bot) cmdSetLimit(ctx context.Context, m *tgbotapi.Message) {
	chatID := int64(m.Chat.ID)
	args := strings.Fields(m.CommandArguments())
	if len(args) != 2 {
		b.sendText(chatID, "Использование: /setlimit <telegram_id> <limit>")
		return
	}
	id, err1 := strconv.ParseInt(args[0], 10, 64)
	limit, err2 := strconv.Atoi(args[1])
	if err1 != nil || err2 != nil || id <= 0 {
		b.sendText(chatID, "Некорректные аргументы.")
		return
	}
	if err := b.svc.AdminSetLimit(ctx, id, limit); err != nil {
		b.log.Error("setlimit failed", "err", err)
		b.sendText(chatID, userMessage(err))
		return
	}
	b.sendText(chatID, fmt.Sprintf("Лимит пользователя %d изменён на %d.", id, limit))
}

func (b *Bot) cmdUsers(ctx context.Context, m *tgbotapi.Message) {
	chatID := int64(m.Chat.ID)
	users, err := b.svc.AdminListUsers(ctx)
	if err != nil {
		b.log.Error("users failed", "err", err)
		b.sendText(chatID, textServiceDown)
		return
	}
	var sb strings.Builder
	sb.WriteString("Пользователи:\n")
	for _, u := range users {
		status := "✅"
		if !u.Enabled {
			status = "⛔️"
		}
		fmt.Fprintf(&sb, "%s %d @%s — активных %d, лимит %d\n", status, u.TelegramID, u.Username, u.ActiveConfigs, u.ConfigLimit)
	}
	b.sendText(chatID, sb.String())
}
