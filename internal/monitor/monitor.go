package monitor

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"amnezia-manager-bot/internal/config"
	"amnezia-manager-bot/internal/vpn"
)

// Alerts — подмножество alerts.Manager, нужное монитору.
type Alerts interface {
	ServerDown(ctx context.Context, serverID string)
	ServerUp(ctx context.Context, serverID string)
}

type srvState struct {
	downSince time.Time
	upSince   time.Time
	lastCheck time.Time
	alerted   bool
}

// ServerStatus — снимок состояния одного сервера для отображения админу.
type ServerStatus struct {
	ServerID  string
	Down      bool
	Since     time.Time
	LastCheck time.Time
}

// Monitor периодически проверяет серверы по SSH и уведомляет о недоступности
// дольше порога; восстановление тоже уведомляет (однократно на инцидент).
type Monitor struct {
	vpn       vpn.Provider
	alerts    Alerts
	servers   []config.ServerConfig
	interval  time.Duration
	threshold time.Duration
	log       *slog.Logger

	mu    sync.Mutex
	state map[string]*srvState
	now   func() time.Time
}

func New(vp vpn.Provider, a Alerts, servers []config.ServerConfig, interval, threshold time.Duration, log *slog.Logger) *Monitor {
	if log == nil {
		log = slog.Default()
	}
	states := map[string]*srvState{}
	for _, s := range servers {
		states[s.ID] = &srvState{}
	}
	return &Monitor{
		vpn: vp, alerts: a, servers: servers,
		interval: interval, threshold: threshold, log: log,
		state: states, now: time.Now,
	}
}

func (m *Monitor) Run(ctx context.Context) {
	t := time.NewTicker(m.interval)
	defer t.Stop()
	for {
		m.CheckNow(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// CheckNow проверяет все серверы один раз.
func (m *Monitor) CheckNow(ctx context.Context) {
	for _, s := range m.servers {
		m.checkServer(ctx, s.ID)
	}
}

func (m *Monitor) checkServer(ctx context.Context, serverID string) {
	err := m.vpn.HealthCheck(ctx, serverID)
	m.mu.Lock()
	st := m.state[serverID]
	if st == nil {
		st = &srvState{}
		m.state[serverID] = st
	}
	st.lastCheck = m.now()
	var down, up bool
	if err != nil {
		st.upSince = time.Time{}
		if st.downSince.IsZero() {
			st.downSince = m.now()
		}
		if !st.alerted && m.now().Sub(st.downSince) >= m.threshold {
			st.alerted = true
			down = true
		}
	} else {
		was := st.alerted
		st.downSince = time.Time{}
		st.alerted = false
		if st.upSince.IsZero() {
			st.upSince = m.now()
		}
		if was {
			up = true
		}
	}
	m.mu.Unlock()
	if down {
		m.alerts.ServerDown(ctx, serverID)
		m.log.Warn("server down alert", "server", serverID)
	}
	if up {
		m.alerts.ServerUp(ctx, serverID)
		m.log.Info("server recovered", "server", serverID)
	}
}

// Snapshot возвращает текущее состояние всех отслеживаемых серверов.
func (m *Monitor) Snapshot() []ServerStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ServerStatus, 0, len(m.servers))
	for _, s := range m.servers {
		st := m.state[s.ID]
		if st == nil {
			out = append(out, ServerStatus{ServerID: s.ID})
			continue
		}
		out = append(out, ServerStatus{
			ServerID:  s.ID,
			Down:      !st.downSince.IsZero(),
			Since:     st.upSince,
			LastCheck: st.lastCheck,
		})
		if !st.downSince.IsZero() {
			out[len(out)-1].Since = st.downSince
		}
	}
	return out
}
