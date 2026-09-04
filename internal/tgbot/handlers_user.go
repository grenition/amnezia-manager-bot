package tgbot

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"amnezia-manager-bot/internal/alerts"
	"amnezia-manager-bot/internal/service"
)

func (b *Bot) handleDeviceName(ctx context.Context, uid int64, chatID int64, name string) {
	cc, err := b.svc.CreateConfig(ctx, uid, strings.TrimSpace(name))
	if err != nil {
		b.log.Error("create config failed", "user", uid, "err", err)
		b.sendText(chatID, userMessage(err))
		return
	}
	doc := tgbotapi.NewDocument(chatID, tgbotapi.FileBytes{Name: cc.FileName, Bytes: []byte(cc.Content)})
	if _, err := b.api.Send(doc); err != nil {
		b.log.Error("send document failed", "user", uid, "err", err)
		b.sendText(chatID, textServiceDown)
		return
	}
	b.sendText(chatID, textConfigOnce)
}

func (b *Bot) showDevices(ctx context.Context, uid int64, chatID int64) {
	peers, limit, err := b.svc.ListDevices(ctx, uid)
	if err != nil {
		b.sendText(chatID, userMessage(err))
		return
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Активных конфигов: %d из %d.\n", len(peers), limit)
	if len(peers) == 0 {
		sb.WriteString("Конфигов нет.")
	}
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, p := range peers {
		fmt.Fprintf(&sb, "• %s (создан %s)\n", p.DeviceName, p.CreatedAt.Format("02.01.2006"))
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Удалить: "+p.DeviceName, fmt.Sprintf("del:%d", p.ID)),
		))
	}
	msg := tgbotapi.NewMessage(chatID, sb.String())
	if len(rows) > 0 {
		msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
	}
	if _, err := b.api.Send(msg); err != nil {
		b.log.Error("send devices failed", "err", err)
	}
}

func (b *Bot) confirmDelete(chatID int64, idStr string) {
	msg := tgbotapi.NewMessage(chatID, "Удалить конфиг? Действие необратимо.")
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Да, удалить", "delok:"+idStr),
			tgbotapi.NewInlineKeyboardButtonData("Отмена", "devices"),
		),
	)
	if _, err := b.api.Send(msg); err != nil {
		b.log.Error("send confirm failed", "err", err)
	}
}

func (b *Bot) doDelete(ctx context.Context, uid int64, chatID int64, idStr string) {
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		b.sendText(chatID, userMessage(service.ErrNotFound))
		return
	}
	if err := b.svc.DeleteConfig(ctx, uid, id); err != nil {
		b.log.Error("delete config failed", "user", uid, "err", err)
		b.sendText(chatID, userMessage(err))
		return
	}
	b.sendText(chatID, textDeleted)
}

func (b *Bot) handleComplaintText(ctx context.Context, uid int64, username string, chatID int64, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		b.sendText(chatID, textAskComplaint)
		return
	}
	serverID, _, err := b.svc.ServerForComplaint(ctx, uid)
	if err != nil {
		b.log.Error("complaint server failed", "err", err)
		b.sendText(chatID, textServiceDown)
		return
	}
	b.alerts.Complaint(ctx, serverID, alerts.Complaint{AuthorID: uid, Username: username, Text: text})
	b.log.Info("complaint registered", "user", uid, "server", serverID)
	b.sendText(chatID, textComplaintSent)
}
