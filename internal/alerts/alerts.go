package alerts

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"amnezia-manager-bot/internal/store"
)

// Sender — минимальный интерфейс отправки Telegram-сообщений (реализация — tgbot.Sender).
type Sender interface {
	SendMessage(chatID int64, text string) (messageID int64, err error)
	EditMessage(chatID, messageID int64, text string) error
}

type Complaint struct {
	AuthorID  int64
	FirstName string
	Username  string
	Text      string
	At        time.Time
}

// Manager ведёт по одному статусному сообщению на (сервер, админ) для алертов
// доступности; обращения пользователей уходят отдельными сообщениями.
type Manager struct {
	store       store.Store
	sender      Sender
	serverNames map[string]string
	adminIDs    []int64
	log         *slog.Logger

	mu        sync.Mutex
	downSince map[string]time.Time
}

func NewManager(st store.Store, sender Sender, serverNames map[string]string, adminIDs []int64) *Manager {
	return &Manager{
		store:       st,
		sender:      sender,
		serverNames: serverNames,
		adminIDs:    adminIDs,
		log:         slog.Default(),
		downSince:   map[string]time.Time{},
	}
}

func (m *Manager) name(serverID string) string {
	if n, ok := m.serverNames[serverID]; ok && n != "" {
		return n
	}
	return serverID
}

func (m *Manager) ServerDown(ctx context.Context, serverID string) {
	m.mu.Lock()
	if _, ok := m.downSince[serverID]; !ok {
		m.downSince[serverID] = time.Now()
	}
	m.mu.Unlock()
	m.updateCards(ctx, serverID)
}

func (m *Manager) ServerUp(ctx context.Context, serverID string) {
	m.mu.Lock()
	delete(m.downSince, serverID)
	m.mu.Unlock()
	m.updateCards(ctx, serverID)
}

// Complaint отправляет обращение отдельным сообщением каждому админу —
// его нельзя прятать в редактируемую статусную карточку.
func (m *Manager) Complaint(ctx context.Context, serverID string, c Complaint) {
	who := c.FirstName
	if c.Username != "" {
		if who != "" {
			who += " "
		}
		who += "@" + c.Username
	}
	if who == "" {
		who = fmt.Sprintf("id %d", c.AuthorID)
	}
	text := fmt.Sprintf("%s [id %d]\n\n%s", who, c.AuthorID, c.Text)
	if len(text) > 3500 {
		text = text[:3500] + "…"
	}
	for _, admin := range m.adminIDs {
		if _, err := m.sender.SendMessage(admin, "🆘 Обращение · "+m.name(serverID)+"\n\n"+text); err != nil {
			m.log.Error("send complaint failed", "server", serverID, "admin", admin, "err", err)
		}
	}
}

func (m *Manager) card(serverID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	name := m.name(serverID)
	if since, ok := m.downSince[serverID]; ok {
		return fmt.Sprintf("🔴 Сервер «%s» недоступен с %s", name, since.Format("02.01 15:04"))
	}
	return fmt.Sprintf("🟢 Сервер «%s» работает", name)
}

func (m *Manager) updateCards(ctx context.Context, serverID string) {
	text := m.card(serverID)
	for _, admin := range m.adminIDs {
		sm, err := m.store.GetStatusMessage(ctx, serverID, admin)
		if err == nil {
			if m.sender.EditMessage(sm.ChatID, sm.MessageID, text) == nil {
				continue
			}
			m.log.Warn("edit status message failed, sending new", "server", serverID, "admin", admin)
		}
		msgID, err := m.sender.SendMessage(admin, text)
		if err != nil {
			m.log.Error("send status message failed", "server", serverID, "admin", admin, "err", err)
			continue
		}
		if err := m.store.SaveStatusMessage(ctx, store.StatusMessage{
			ServerID: serverID, AdminID: admin, ChatID: admin, MessageID: msgID,
		}); err != nil {
			m.log.Error("save status message failed", "server", serverID, "admin", admin, "err", err)
		}
	}
}
