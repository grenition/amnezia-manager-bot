package tgbot

import (
	"context"
	"log/slog"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"amnezia-manager-bot/internal/alerts"
	"amnezia-manager-bot/internal/monitor"
	"amnezia-manager-bot/internal/service"
)

// StatusProvider отдаёт текущее состояние серверов (реализация — monitor.Monitor).
type StatusProvider interface {
	Snapshot() []monitor.ServerStatus
}

type Bot struct {
	api         *tgbotapi.BotAPI
	svc         *service.Service
	alerts      *alerts.Manager
	st          *states
	log         *slog.Logger
	serverNames map[string]string
	status      StatusProvider
}

func New(api *tgbotapi.BotAPI, svc *service.Service, a *alerts.Manager, log *slog.Logger, serverNames map[string]string, status StatusProvider) *Bot {
	return &Bot{api: api, svc: svc, alerts: a, st: newStates(), log: log, serverNames: serverNames, status: status}
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
	if _, err := b.api.Request(tgbotapi.NewSetMyCommands(
		tgbotapi.BotCommand{Command: "start", Description: "Главное меню"},
		tgbotapi.BotCommand{Command: "help", Description: "Как подключиться"},
	)); err != nil {
		b.log.Warn("set my commands failed", "err", err)
	}
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
	b.rememberUser(ctx, m.From.ID, m.From.UserName, m.From.FirstName)
	uid := m.From.ID
	chatID := int64(m.Chat.ID)

	if m.IsCommand() {
		b.handleCommand(ctx, uid, chatID, m)
		return
	}
	if isButton(m.Text) {
		b.st.clear(uid)
		b.handleButton(ctx, uid, chatID, m.Text)
		return
	}
	switch b.st.get(uid) {
	case stateDeviceName:
		b.st.clear(uid)
		b.handleDeviceName(ctx, uid, chatID, m.Text)
	case stateComplaint:
		b.st.clear(uid)
		b.handleComplaintText(ctx, uid, m.From.UserName, m.From.FirstName, chatID, m.Text)
	case stateAdminAddUser:
		b.st.clear(uid)
		b.adminResolveUser(ctx, uid, chatID, m.Text, "add")
	case stateAdminDisableUser:
		b.st.clear(uid)
		b.adminResolveUser(ctx, uid, chatID, m.Text, "disable")
	case stateAdminLimitUser:
		b.st.clear(uid)
		b.adminResolveUser(ctx, uid, chatID, m.Text, "limit")
	case stateAdminLimitValue:
		b.st.clear(uid)
		b.adminLimitValue(ctx, uid, chatID, m.Text)
	default:
		b.sendHTML(chatID, textFallback)
	}
}

func (b *Bot) handleCommand(ctx context.Context, uid int64, chatID int64, m *tgbotapi.Message) {
	switch m.Command() {
	case "start":
		b.handleStart(ctx, uid, chatID)
	case "help":
		b.sendHTML(chatID, textInstruction)
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
			b.cmdUsersList(ctx, int64(m.Chat.ID))
		}
	default:
		b.sendHTML(chatID, textFallback)
	}
}

func (b *Bot) handleButton(ctx context.Context, uid int64, chatID int64, label string) {
	switch label {
	case btnNewConfig:
		if b.userAllowed(ctx, uid, chatID) {
			b.st.set(uid, stateDeviceName)
			b.sendHTML(chatID, textAskDeviceName)
		}
	case btnDevices:
		if b.userAllowed(ctx, uid, chatID) {
			b.showDevices(ctx, uid, chatID)
		}
	case btnHelp:
		b.sendHTML(chatID, textInstruction)
	case btnSupport:
		if b.userAllowed(ctx, uid, chatID) {
			b.st.set(uid, stateComplaint)
			b.sendHTML(chatID, textAskComplaint)
		}
	case btnUsers:
		if b.adminOnly(uid, chatID) {
			b.cmdUsersList(ctx, chatID)
		}
	case btnServers:
		if b.adminOnly(uid, chatID) {
			b.cmdServersStatus(ctx, chatID)
		}
	case btnAddUser:
		if b.adminOnly(uid, chatID) {
			b.st.set(uid, stateAdminAddUser)
			b.sendHTML(chatID, textAdminAskUsername)
		}
	case btnDisable:
		if b.adminOnly(uid, chatID) {
			b.st.set(uid, stateAdminDisableUser)
			b.sendHTML(chatID, textAdminAskUsernameDisable)
		}
	case btnLimit:
		if b.adminOnly(uid, chatID) {
			b.st.set(uid, stateAdminLimitUser)
			b.sendHTML(chatID, textAdminAskUsernameLimit)
		}
	}
}

func (b *Bot) handleStart(ctx context.Context, uid int64, chatID int64) {
	if !b.svc.IsAdmin(uid) {
		if err := b.svc.CheckAccess(ctx, uid); err != nil {
			b.sendHTML(chatID, textNoAccess)
			return
		}
	}
	text := textWelcomeUser
	kb := userKeyboard()
	if b.svc.IsAdmin(uid) {
		text += textWelcomeAdmin
		kb = adminKeyboard()
	}
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = tgbotapi.ModeHTML
	msg.ReplyMarkup = kb
	if _, err := b.api.Send(msg); err != nil {
		b.log.Error("send welcome failed", "err", err)
	}
}

func (b *Bot) handleCallback(ctx context.Context, q *tgbotapi.CallbackQuery) {
	b.answerCallback(q.ID)
	if q.Message == nil {
		return
	}
	b.rememberUser(ctx, q.From.ID, q.From.UserName, q.From.FirstName)
	chatID := int64(q.Message.Chat.ID)
	msgID := int64(q.Message.MessageID)
	uid := q.From.ID

	op, args := parseCallback(q.Data)
	switch op {
	case "create":
		if b.userAllowed(ctx, uid, chatID) {
			b.st.set(uid, stateDeviceName)
			b.sendHTML(chatID, textAskDeviceName)
		}
	case "devs":
		if b.userAllowed(ctx, uid, chatID) {
			b.editDevices(ctx, uid, chatID, msgID)
		}
	case "del":
		if b.userAllowed(ctx, uid, chatID) && len(args) == 2 {
			b.confirmDelete(chatID, msgID, args[0], args[1])
		}
	case "delok":
		if b.userAllowed(ctx, uid, chatID) && len(args) == 2 {
			b.doDelete(ctx, uid, chatID, msgID, args[0], args[1])
		}
	case "admadd":
		if b.adminOnly(uid, chatID) && len(args) == 2 {
			b.adminAddConfirm(ctx, chatID, msgID, args[0], args[1])
		}
	case "admdis":
		if b.adminOnly(uid, chatID) && len(args) == 2 {
			b.adminDisableConfirm(ctx, chatID, msgID, args[0], args[1])
		}
	case "admno":
		b.editHTML(chatID, msgID, "Отменено.", nil)
	}
}

func parseCallback(data string) (string, []string) {
	parts := strings.Split(data, ":")
	if len(parts) < 2 {
		return parts[0], nil
	}
	return parts[0], parts[1:]
}

func (b *Bot) rememberUser(ctx context.Context, telegramID int64, username, firstName string) {
	b.svc.RememberUser(ctx, telegramID, username, firstName)
}

func (b *Bot) userAllowed(ctx context.Context, uid int64, chatID int64) bool {
	if b.svc.IsAdmin(uid) {
		return true
	}
	if err := b.svc.CheckAccess(ctx, uid); err != nil {
		b.sendHTML(chatID, textNoAccess)
		return false
	}
	return true
}

func (b *Bot) adminOnly(uid int64, chatID int64) bool {
	if !b.svc.IsAdmin(uid) {
		b.sendHTML(chatID, textNoAccess)
		return false
	}
	return true
}

func (b *Bot) sendHTML(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = tgbotapi.ModeHTML
	if _, err := b.api.Send(msg); err != nil {
		b.log.Error("send message failed", "chat", chatID, "err", err)
	}
}

func (b *Bot) editHTML(chatID, messageID int64, text string, markup *tgbotapi.InlineKeyboardMarkup) {
	edit := tgbotapi.NewEditMessageText(chatID, int(messageID), text)
	edit.ParseMode = tgbotapi.ModeHTML
	if markup != nil {
		edit.ReplyMarkup = markup
	}
	if _, err := b.api.Send(edit); err != nil {
		b.log.Error("edit message failed", "chat", chatID, "err", err)
	}
}

func (b *Bot) answerCallback(id string) {
	if _, err := b.api.Request(tgbotapi.NewCallback(id, "")); err != nil {
		b.log.Error("answer callback failed", "err", err)
	}
}
