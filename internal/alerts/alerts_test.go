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
	f.sent = append(f.sent, text)
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

func TestRecoveryAndNewIncidentEditSameMessage(t *testing.T) {
	m, fs, st := newMgr(t)
	ctx := context.Background()
	m.ServerDown(ctx, "s1")
	first, _ := st.GetStatusMessage(ctx, "s1", 10)
	m.ServerUp(ctx, "s1")
	m.ServerDown(ctx, "s1") // новый инцидент
	second, _ := st.GetStatusMessage(ctx, "s1", 10)
	if first.MessageID != second.MessageID {
		t.Fatalf("message id changed: %d -> %d", first.MessageID, second.MessageID)
	}
	if len(fs.sent) != 2 {
		t.Fatalf("no new messages expected after first, sent=%d", len(fs.sent))
	}
	key := fmt.Sprintf("10:%d", second.MessageID)
	if _, ok := fs.edited[key]; !ok {
		t.Fatalf("no edit recorded: %v", fs.edited)
	}
}

func TestComplaintUpdatesCard(t *testing.T) {
	m, fs, _ := newMgr(t)
	ctx := context.Background()
	m.Complaint(ctx, "s1", Complaint{AuthorID: 100, Username: "u100", Text: "не работает", At: time.Now()})
	if len(fs.sent) != 2 {
		t.Fatalf("complaint must create card if missing, sent=%d", len(fs.sent))
	}
	m.Complaint(ctx, "s1", Complaint{AuthorID: 101, Username: "u101", Text: "тоже не работает", At: time.Now()})
	if len(fs.sent) != 2 {
		t.Fatalf("second complaint must edit existing card, sent=%d", len(fs.sent))
	}
	found := false
	for _, txt := range fs.edited {
		if strings.Contains(txt, "u101") && strings.Contains(txt, "тоже не работает") {
			found = true
		}
	}
	if !found {
		t.Fatalf("complaint text not in card edits: %v", fs.edited)
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
	m.ServerUp(ctx, "s1") // edit упадёт → отправит новое сообщение и перепишет id
	if len(fs.sent) <= before {
		t.Fatal("expected new message on edit failure")
	}
	sm, err := st.GetStatusMessage(ctx, "s1", 10)
	if err != nil || sm.MessageID == 0 {
		t.Fatalf("stored: %v %+v", err, sm)
	}
}

var _ store.Store = (*memory.MemoryStore)(nil)
