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
	now = now.Add(stateTTL + time.Second)
	if s.get(1) != stateNone {
		t.Fatal("expired state must be none")
	}
	s.set(1, stateComplaint)
	s.clear(1)
	if s.get(1) != stateNone {
		t.Fatal("cleared state must be none")
	}
}
