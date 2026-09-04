package monitor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"amnezia-manager-bot/internal/config"
)

var errDown = errors.New("ssh unreachable")

type fakeVPN struct {
	mu   sync.Mutex
	errs map[string]error
}

func (f *fakeVPN) setErr(id string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errs[id] = err
}

func (f *fakeVPN) HealthCheck(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.errs[id]
}

func (f *fakeVPN) CreatePeer(context.Context, string, string, string) error { return nil }
func (f *fakeVPN) RemovePeer(context.Context, string, string) error         { return nil }

type fakeAlerts struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakeAlerts) ServerDown(_ context.Context, id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "down:"+id)
}

func (f *fakeAlerts) ServerUp(_ context.Context, id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "up:"+id)
}

func (f *fakeAlerts) snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func newMon(t *testing.T) (*Monitor, *fakeVPN, *fakeAlerts) {
	t.Helper()
	fv := &fakeVPN{errs: map[string]error{}}
	fa := &fakeAlerts{}
	m := New(fv, fa, []config.ServerConfig{{ID: "s1"}, {ID: "s2"}}, time.Minute, 2*time.Minute, nil)
	return m, fv, fa
}

func TestThreshold(t *testing.T) {
	m, fv, fa := newMon(t)
	ctx := context.Background()
	now := time.Now()
	m.now = func() time.Time { return now }

	fv.setErr("s1", errDown)
	m.CheckNow(ctx) // t0: сбой начался
	if calls := fa.snapshot(); len(calls) != 0 {
		t.Fatalf("early alert: %v", calls)
	}
	now = now.Add(2 * time.Minute)
	m.CheckNow(ctx) // порог достигнут
	if calls := fa.snapshot(); len(calls) != 1 || calls[0] != "down:s1" {
		t.Fatalf("calls = %v", calls)
	}
	now = now.Add(time.Minute)
	m.CheckNow(ctx) // всё ещё недоступен — не дублируем
	if calls := fa.snapshot(); len(calls) != 1 {
		t.Fatalf("duplicated: %v", calls)
	}
	fv.setErr("s1", nil)
	m.CheckNow(ctx) // восстановился
	if calls := fa.snapshot(); len(calls) != 2 || calls[1] != "up:s1" {
		t.Fatalf("calls = %v", calls)
	}
	m.CheckNow(ctx)
	if calls := fa.snapshot(); len(calls) != 2 {
		t.Fatalf("calls = %v", calls)
	}
}

func TestFlapUnderThresholdResets(t *testing.T) {
	m, fv, fa := newMon(t)
	ctx := context.Background()
	now := time.Now()
	m.now = func() time.Time { return now }

	fv.setErr("s1", errDown)
	m.CheckNow(ctx)
	now = now.Add(time.Minute) // < порога 2м
	fv.setErr("s1", nil)
	m.CheckNow(ctx) // флап исправился до порога — алертов нет
	if calls := fa.snapshot(); len(calls) != 0 {
		t.Fatalf("calls = %v", calls)
	}
	fv.setErr("s1", errDown)
	m.CheckNow(ctx) // downSince сброшен, отсчёт заново
	now = now.Add(2 * time.Minute)
	m.CheckNow(ctx)
	if calls := fa.snapshot(); len(calls) != 1 || calls[0] != "down:s1" {
		t.Fatalf("calls = %v", calls)
	}
}

func TestServersIndependent(t *testing.T) {
	m, fv, fa := newMon(t)
	ctx := context.Background()
	now := time.Now()
	m.now = func() time.Time { return now }
	fv.setErr("s1", errDown)
	fv.setErr("s2", errDown)
	m.CheckNow(ctx)
	now = now.Add(2 * time.Minute)
	fv.setErr("s2", nil)
	m.CheckNow(ctx) // s1 алерт, s2 восстановился до алерта
	if calls := fa.snapshot(); len(calls) != 1 || calls[0] != "down:s1" {
		t.Fatalf("calls = %v", calls)
	}
}
