package memory

import (
	"context"
	"sort"
	"strconv"
	"sync"
	"time"

	"amnezia-manager-bot/internal/store"
)

type MemoryStore struct {
	mu         sync.Mutex
	users      map[int64]store.User
	access     map[int64]map[string]bool
	peers      map[int64]store.Peer
	statusMsgs map[string]map[int64]store.StatusMessage
	knownUsers map[int64]store.KnownUser
	nextPeerID int64
}

func New() *MemoryStore {
	return &MemoryStore{
		users:      map[int64]store.User{},
		access:     map[int64]map[string]bool{},
		peers:      map[int64]store.Peer{},
		statusMsgs: map[string]map[int64]store.StatusMessage{},
		knownUsers: map[int64]store.KnownUser{},
		nextPeerID: 1,
	}
}

func (m *MemoryStore) UpsertUser(_ context.Context, u store.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.users[u.TelegramID] = u
	return nil
}

func (m *MemoryStore) GetUser(_ context.Context, id int64) (store.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[id]
	if !ok {
		return store.User{}, store.ErrNotFound
	}
	return u, nil
}

func (m *MemoryStore) SetUserEnabled(_ context.Context, id int64, enabled bool) error {
	return m.updateUser(id, func(u *store.User) { u.Enabled = enabled })
}

func (m *MemoryStore) SetUserLimit(_ context.Context, id int64, limit int) error {
	return m.updateUser(id, func(u *store.User) { u.ConfigLimit = limit })
}

func (m *MemoryStore) SetUsername(_ context.Context, id int64, username string) error {
	return m.updateUser(id, func(u *store.User) { u.Username = username })
}

func (m *MemoryStore) updateUser(id int64, f func(*store.User)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[id]
	if !ok {
		return store.ErrNotFound
	}
	f(&u)
	m.users[id] = u
	return nil
}

func (m *MemoryStore) ListUsers(_ context.Context) ([]store.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]store.User, 0, len(m.users))
	for _, u := range m.users {
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TelegramID < out[j].TelegramID })
	return out, nil
}

func (m *MemoryStore) GrantAccess(_ context.Context, telegramID int64, serverID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.access[telegramID] == nil {
		m.access[telegramID] = map[string]bool{}
	}
	m.access[telegramID][serverID] = true
	return nil
}

func (m *MemoryStore) HasAccess(_ context.Context, telegramID int64, serverID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.access[telegramID][serverID], nil
}

func (m *MemoryStore) ListUserServers(_ context.Context, telegramID int64) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for id := range m.access[telegramID] {
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

func (m *MemoryStore) CreatePeer(_ context.Context, p store.Peer) (store.Peer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p.ID = m.nextPeerID
	m.nextPeerID++
	p.CreatedAt = time.Now().UTC()
	m.peers[p.ID] = p
	return p, nil
}

func (m *MemoryStore) ListActivePeers(_ context.Context, telegramID int64) ([]store.Peer, error) {
	return m.filterPeers(func(p store.Peer) bool { return p.RevokedAt == nil && p.TelegramID == telegramID }), nil
}

func (m *MemoryStore) ListActivePeersOnServer(_ context.Context, serverID string) ([]store.Peer, error) {
	return m.filterPeers(func(p store.Peer) bool { return p.RevokedAt == nil && p.ServerID == serverID }), nil
}

func (m *MemoryStore) ListActivePeersAll(_ context.Context) ([]store.Peer, error) {
	return m.filterPeers(func(p store.Peer) bool { return p.RevokedAt == nil }), nil
}

func (m *MemoryStore) filterPeers(f func(store.Peer) bool) []store.Peer {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []store.Peer
	for _, p := range m.peers {
		if f(p) {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (m *MemoryStore) GetActivePeer(_ context.Context, telegramID int64, id int64) (store.Peer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.peers[id]
	if !ok || p.RevokedAt != nil || p.TelegramID != telegramID {
		return store.Peer{}, store.ErrNotFound
	}
	return p, nil
}

func (m *MemoryStore) RevokePeer(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.peers[id]
	if !ok {
		return store.ErrNotFound
	}
	now := time.Now().UTC()
	p.RevokedAt = &now
	m.peers[id] = p
	return nil
}

func (m *MemoryStore) GetStatusMessage(_ context.Context, serverID string, adminID int64) (store.StatusMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sm, ok := m.statusMsgs[serverID][adminID]
	if !ok {
		return store.StatusMessage{}, store.ErrNotFound
	}
	return sm, nil
}

func (m *MemoryStore) SaveStatusMessage(_ context.Context, sm store.StatusMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.statusMsgs[sm.ServerID] == nil {
		m.statusMsgs[sm.ServerID] = map[int64]store.StatusMessage{}
	}
	m.statusMsgs[sm.ServerID][sm.AdminID] = sm
	return nil
}

func (m *MemoryStore) UpsertKnownUser(_ context.Context, telegramID int64, username, firstName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, u := range m.knownUsers {
		if u.Username == username && id != telegramID {
			u.Username = strconv.FormatInt(id, 10)
			m.knownUsers[id] = u
		}
	}
	m.knownUsers[telegramID] = store.KnownUser{TelegramID: telegramID, Username: username, FirstName: firstName}
	return nil
}

func (m *MemoryStore) FindKnownUser(_ context.Context, username string) (store.KnownUser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.knownUsers {
		if u.Username == username {
			return u, nil
		}
	}
	return store.KnownUser{}, store.ErrNotFound
}
