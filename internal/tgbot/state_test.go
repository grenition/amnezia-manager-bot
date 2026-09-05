package tgbot

import (
	"testing"
	"time"
)

func TestStates(t *testing.T) {
	s := newStates()
	now := time.Now()
	s.now = func() time.Time { return now }

	if s.get(1) != stateNone {
		t.Fatal("default must be none")
	}
	s.set(1, stateDeviceName)
	if s.get(1) != stateDeviceName {
		t.Fatal("state not stored")
	}
	if id, name := s.target(1); id != 0 || name != "" {
		t.Fatalf("empty target: %d %q", id, name)
	}
	now = now.Add(stateTTL + time.Second)
	if s.get(1) != stateNone {
		t.Fatal("expired state must be none")
	}
	s.setTarget(1, stateAdminLimitValue, 42, "ivan")
	if s.get(1) != stateAdminLimitValue {
		t.Fatal("target state not stored")
	}
	if id, name := s.target(1); id != 42 || name != "ivan" {
		t.Fatalf("target: %d %q", id, name)
	}
	s.clear(1)
	if s.get(1) != stateNone {
		t.Fatal("cleared state must be none")
	}
	if id, name := s.target(1); id != 0 || name != "" {
		t.Fatal("cleared target must be zero")
	}
}

func TestAllButtonsRouted(t *testing.T) {
	for _, label := range []string{btnNewConfig, btnDevices, btnHelp, btnSupport, btnUsers, btnAddUser, btnDisable, btnLimit} {
		if !isButton(label) {
			t.Fatalf("label %q missing from buttonLabels", label)
		}
	}
	if isButton("случайный текст") {
		t.Fatal("free text must not be a button")
	}
}
