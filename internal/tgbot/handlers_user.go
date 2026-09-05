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
	name = strings.TrimSpace(name)
	if isButton(name) || strings.HasPrefix(name, "/") {
		b.sendHTML(chatID, textFallback)
		return
	}
	action := tgbotapi.NewChatAction(chatID, "upload_document")
	if _, err := b.api.Request(action); err != nil {
		b.log.Error("chat action failed", "err", err)
	}
	cc, err := b.svc.CreateConfig(ctx, uid, name)
	if err != nil {
		b.log.Error("create config failed", "user", uid, "err", err)
		b.sendHTML(chatID, userMessage(err))
		return
	}
	doc := tgbotapi.NewDocument(chatID, tgbotapi.FileBytes{Name: cc.FileName, Bytes: []byte(cc.Content)})
	doc.Caption = configCaption(cc.DeviceName)
	doc.ParseMode = tgbotapi.ModeHTML
	if _, err := b.api.Send(doc); err != nil {
		b.log.Error("send document failed", "user", uid, "err", err)
		b.sendHTML(chatID, textServiceDown)
	}
}

func devicesMarkup(names []string, ids []int64) *tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton
	if len(names) == 0 {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✨ Создать конфиг", "create"),
		))
	} else {
		for i, n := range names {
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("🗑 "+n, fmt.Sprintf("del:%d:%s", ids[i], n)),
			))
		}
	}
	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
	return &kb
}

func (b *Bot) devicesView(ctx context.Context, uid int64) (string, *tgbotapi.InlineKeyboardMarkup, error) {
	peers, _, err := b.svc.ListDevices(ctx, uid)
	if err != nil {
		return "", nil, err
	}
	names := make([]string, 0, len(peers))
	ids := make([]int64, 0, len(peers))
	for _, p := range peers {
		names = append(names, p.DeviceName)
		ids = append(ids, p.ID)
	}
	return devicesText(names), devicesMarkup(names, ids), nil
}

func (b *Bot) showDevices(ctx context.Context, uid int64, chatID int64) {
	text, markup, err := b.devicesView(ctx, uid)
	if err != nil {
		b.sendHTML(chatID, userMessage(err))
		return
	}
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = tgbotapi.ModeHTML
	msg.ReplyMarkup = markup
	if _, err := b.api.Send(msg); err != nil {
		b.log.Error("send devices failed", "err", err)
	}
}

func (b *Bot) editDevices(ctx context.Context, uid int64, chatID int64, msgID int64) {
	text, markup, err := b.devicesView(ctx, uid)
	if err != nil {
		b.sendHTML(chatID, userMessage(err))
		return
	}
	b.editHTML(chatID, msgID, text, markup)
}

func (b *Bot) confirmDelete(chatID, msgID int64, idStr, name string) {
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Удалить", "delok:"+idStr+":"+name),
			tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", "devs"),
		),
	)
	b.editHTML(chatID, msgID, deleteConfirmText(name), &kb)
}

func (b *Bot) doDelete(ctx context.Context, uid int64, chatID int64, msgID int64, idStr, name string) {
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		b.sendHTML(chatID, userMessage(service.ErrNotFound))
		return
	}
	if err := b.svc.DeleteConfig(ctx, uid, id); err != nil {
		b.log.Error("delete config failed", "user", uid, "err", err)
		b.sendHTML(chatID, userMessage(err))
		return
	}
	b.editDevices(ctx, uid, chatID, msgID)
}

func (b *Bot) handleComplaintText(ctx context.Context, uid int64, username string, chatID int64, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		b.sendHTML(chatID, textAskComplaint)
		return
	}
	serverID, _, err := b.svc.ServerForComplaint(ctx, uid)
	if err != nil {
		b.log.Error("complaint server failed", "err", err)
		b.sendHTML(chatID, textServiceDown)
		return
	}
	b.alerts.Complaint(ctx, serverID, alerts.Complaint{AuthorID: uid, Username: username, Text: text})
	b.log.Info("complaint registered", "user", uid, "server", serverID)
	b.sendHTML(chatID, textComplaintSent)
}
