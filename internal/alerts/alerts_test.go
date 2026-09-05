package alerts

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"amnezia-manager-bot/internal/store"
	"amnezia-manager-bot/internal/store/memory"
)

type fakeSender struct {
	mu     sync.Mutex
	sent   []string
	edited map[string]string
	nextID int64
	failEd bool
}

func newFakeSender() *fakeSender { return &fakeSender{edited: map[string]string{}, nextID: 500} }

func (f *fakeSender) SendMessage(chatID int64, text string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	f.sent = append(f.sent, fmt.Sprintf("%d:%s", chatID, text))
	return f.nextID, nil
}

func (f *fakeSender) EditMessage(chatID, messageID int64, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failEd {
		return fmt.Errorf("edit failed")
	}
	f.edited[fmt.Sprintf("%d:%d", chatID, messageID)] = text
	return nil
}

func newMgr(t *testing.T) (*Manager, *fakeSender, *memory.MemoryStore) {
	t.Helper()
	st := memory.New()
	fs := newFakeSender()
	m := NewManager(st, fs, map[string]string{"s1": "SPB-1"}, []int64{10, 20})
	return m, fs, st
}

func TestServerDownCreatesCardPerAdmin(t *testing.T) {
	m, fs, st := newMgr(t)
	ctx := context.Background()
	m.ServerDown(ctx, "s1")
	if len(fs.sent) != 2 {
		t.Fatalf("sent = %d, want 2", len(fs.sent))
	}
	for _, admin := range []int64{10, 20} {
		sm, err := st.GetStatusMessage(ctx, "s1", admin)
		if err != nil {
			t.Fatalf("admin %d: %v", admin, err)
		}
		if sm.MessageID == 0 {
			t.Fatal("message id must be stored")
		}
	}
}

func TestNewIncidentSendsFreshMessage(t *testing.T) {
	m, fs, st := newMgr(t)
	ctx := context.Background()
	m.ServerDown(ctx, "s1")
	first, _ := st.GetStatusMessage(ctx, "s1", 10)
	m.ServerUp(ctx, "s1")
	m.ServerDown(ctx, "s1")
	second, _ := st.GetStatusMessage(ctx, "s1", 10)
	if first.MessageID == second.MessageID {
		t.Fatalf("new incident must send a new message, id stayed %d", first.MessageID)
	}
	if n := len(fs.sent); n != 4 {
		t.Fatalf("sent = %d, want 4 (2 admins × 2 incidents)", n)
	}
}

func TestRepeatDownWithinIncidentEditsCard(t *testing.T) {
	m, fs, st := newMgr(t)
	ctx := context.Background()
	m.ServerDown(ctx, "s1")
	first, _ := st.GetStatusMessage(ctx, "s1", 10)
	before := len(fs.sent)
	m.ServerDown(ctx, "s1")
	after := len(fs.sent)
	if before != after {
		t.Fatalf("repeat down within incident must edit, sent grew: %d -> %d", before, after)
	}
	second, _ := st.GetStatusMessage(ctx, "s1", 10)
	if first.MessageID != second.MessageID {
		t.Fatal("card id must not change within incident")
	}
	if len(fs.edited) == 0 {
		t.Fatal("edit expected")
	}
}

func TestComplaintSendsDedicatedMessage(t *testing.T) {
	m, fs, _ := newMgr(t)
	ctx := context.Background()
	before := len(fs.sent)
	m.Complaint(ctx, "s1", Complaint{AuthorID: 100, FirstName: "Арсений", Username: "resensisaw", Text: "не работает", At: time.Now()})
	if got := len(fs.sent) - before; got != 2 {
		t.Fatalf("complaint must be sent to every admin, sent=%d", got)
	}
	for _, s := range fs.sent[before:] {
		if !strings.Contains(s, "resensisaw") || !strings.Contains(s, "не работает") {
			t.Fatalf("complaint text incomplete: %q", s)
		}
	}
	if len(fs.edited) != 0 {
		t.Fatalf("complaint must not touch status cards: %v", fs.edited)
	}
}

func TestCardContents(t *testing.T) {
	m, fs, _ := newMgr(t)
	ctx := context.Background()
	m.ServerDown(ctx, "s1")
	if !strings.Contains(fs.sent[0], "SPB-1") || !strings.Contains(fs.sent[0], "недоступен") {
		t.Fatalf("down card: %q", fs.sent[0])
	}
	m.ServerUp(ctx, "s1")
	found := false
	for _, txt := range fs.edited {
		if strings.Contains(txt, "работает") && strings.Contains(txt, "SPB-1") {
			found = true
		}
	}
	if !found {
		t.Fatalf("up card not found in %v", fs.edited)
	}
}

func TestEditFailureSendsNewMessage(t *testing.T) {
	m, fs, st := newMgr(t)
	ctx := context.Background()
	m.ServerDown(ctx, "s1")
	fs.mu.Lock()
	fs.failEd = true
	fs.mu.Unlock()
	before := len(fs.sent)
	m.ServerUp(ctx, "s1")
	if len(fs.sent) <= before {
		t.Fatal("expected new message on edit failure")
	}
	sm, err := st.GetStatusMessage(ctx, "s1", 10)
	if err != nil || sm.MessageID == 0 {
		t.Fatalf("stored: %v %+v", err, sm)
	}
}

var _ store.Store = (*memory.MemoryStore)(nil)
