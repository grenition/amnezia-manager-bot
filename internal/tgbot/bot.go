package tgbot

import (
	"context"
	"log/slog"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"amnezia-manager-bot/internal/alerts"
	"amnezia-manager-bot/internal/service"
)

type Bot struct {
	api         *tgbotapi.BotAPI
	svc         *service.Service
	alerts      *alerts.Manager
	st          *states
	log         *slog.Logger
	serverNames map[string]string
}

func New(api *tgbotapi.BotAPI, svc *service.Service, a *alerts.Manager, log *slog.Logger, serverNames map[string]string) *Bot {
	return &Bot{api: api, svc: svc, alerts: a, st: newStates(), log: log, serverNames: serverNames}
}

// Sender адаптирует telegram-bot-api к alerts.Sender.
type Sender struct{ api *tgbotapi.BotAPI }

func NewSender(api *tgbotapi.BotAPI) Sender { return Sender{api: api} }

func (s Sender) SendMessage(chatID int64, text string) (int64, error) {
	m, err := s.api.Send(tgbotapi.NewMessage(chatID, text))
	if err != nil {
		return 0, err
	}
	return int64(m.MessageID), nil
}

func (s Sender) EditMessage(chatID, messageID int64, text string) error {
	edit := tgbotapi.NewEditMessageText(chatID, int(messageID), text)
	_, err := s.api.Send(edit)
	return err
}

func (b *Bot) Run(ctx context.Context) error {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := b.api.GetUpdatesChan(u)
	b.log.Info("bot started", "username", b.api.Self.UserName)
	for {
		select {
		case <-ctx.Done():
			return nil
		case upd := <-updates:
			b.handleUpdate(ctx, upd)
		}
	}
}

func (b *Bot) handleUpdate(ctx context.Context, u tgbotapi.Update) {
	switch {
	case u.CallbackQuery != nil:
		b.handleCallback(ctx, u.CallbackQuery)
	case u.Message != nil:
		b.handleMessage(ctx, u.Message)
	}
}

func (b *Bot) handleMessage(ctx context.Context, m *tgbotapi.Message) {
	if m.From == nil {
		return
	}
	uid := m.From.ID
	if m.IsCommand() {
		b.handleCommand(ctx, uid, m)
		return
	}
	switch b.st.get(uid) {
	case stateDeviceName:
		b.st.clear(uid)
		b.handleDeviceName(ctx, uid, int64(m.Chat.ID), m.Text)
	case stateComplaint:
		b.st.clear(uid)
		b.handleComplaintText(ctx, uid, m.From.UserName, int64(m.Chat.ID), m.Text)
	}
}

func (b *Bot) handleCommand(ctx context.Context, uid int64, m *tgbotapi.Message) {
	chatID := int64(m.Chat.ID)
	switch m.Command() {
	case "start":
		b.handleStart(ctx, uid, chatID)
	case "adduser":
		if b.adminOnly(uid, chatID) {
			b.cmdAddUser(ctx, m)
		}
	case "disableuser":
		if b.adminOnly(uid, chatID) {
			b.cmdDisableUser(ctx, m)
		}
	case "setlimit":
		if b.adminOnly(uid, chatID) {
			b.cmdSetLimit(ctx, m)
		}
	case "users":
		if b.adminOnly(uid, chatID) {
			b.cmdUsers(ctx, m)
		}
	default:
		b.sendText(chatID, textUnknownCommand)
	}
}

func (b *Bot) adminOnly(uid int64, chatID int64) bool {
	if !b.svc.IsAdmin(uid) {
		b.sendText(chatID, textNoAccess)
		return false
	}
	return true
}

func (b *Bot) handleCallback(ctx context.Context, q *tgbotapi.CallbackQuery) {
	b.answerCallback(q.ID)
	if q.Message == nil {
		return
	}
	chatID := int64(q.Message.Chat.ID)
	uid := q.From.ID
	if !b.userAllowed(ctx, uid, chatID) {
		return
	}
	switch {
	case q.Data == "create":
		b.st.set(uid, stateDeviceName)
		b.sendText(chatID, textAskDeviceName)
	case q.Data == "devices":
		b.showDevices(ctx, uid, chatID)
	case q.Data == "help":
		b.sendText(chatID, textInstruction)
	case q.Data == "complaint":
		b.st.set(uid, stateComplaint)
		b.sendText(chatID, textAskComplaint)
	case len(q.Data) > 4 && q.Data[:4] == "del:":
		b.confirmDelete(chatID, q.Data[4:])
	case len(q.Data) > 6 && q.Data[:6] == "delok:":
		b.doDelete(ctx, uid, chatID, q.Data[6:])
	}
}

func (b *Bot) userAllowed(ctx context.Context, uid int64, chatID int64) bool {
	if b.svc.IsAdmin(uid) {
		return true
	}
	if err := b.svc.CheckAccess(ctx, uid); err != nil {
		b.sendText(chatID, textNoAccess)
		return false
	}
	return true
}

func (b *Bot) handleStart(ctx context.Context, uid int64, chatID int64) {
	if !b.svc.IsAdmin(uid) {
		if err := b.svc.CheckAccess(ctx, uid); err != nil {
			b.sendText(chatID, textNoAccess)
			return
		}
	}
	msg := tgbotapi.NewMessage(chatID, textMenu)
	msg.ReplyMarkup = menuKeyboard()
	if _, err := b.api.Send(msg); err != nil {
		b.log.Error("send menu failed", "err", err)
	}
}

func menuKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Создать конфиг", "create"),
			tgbotapi.NewInlineKeyboardButtonData("Мои устройства", "devices"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Инструкция", "help"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Пожаловаться", "complaint"),
		),
	)
}

func (b *Bot) sendText(chatID int64, text string) {
	if _, err := b.api.Send(tgbotapi.NewMessage(chatID, text)); err != nil {
		b.log.Error("send message failed", "chat", chatID, "err", err)
	}
}

func (b *Bot) answerCallback(id string) {
	if _, err := b.api.Request(tgbotapi.NewCallback(id, "")); err != nil {
		b.log.Error("answer callback failed", "err", err)
	}
}
