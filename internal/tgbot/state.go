package tgbot

import (
	"sync"
	"time"
)

type stateKind int

const (
	stateNone stateKind = iota
	stateDeviceName
	stateComplaint
)

const stateTTL = 10 * time.Minute

type userState struct {
	kind    stateKind
	expires time.Time
}

type states struct {
	mu  sync.Mutex
	m   map[int64]*userState
	now func() time.Time
}

func newStates() *states {
	return &states{m: map[int64]*userState{}, now: time.Now}
}

func (s *states) set(userID int64, k stateKind) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[userID] = &userState{kind: k, expires: s.now().Add(stateTTL)}
}

func (s *states) get(userID int64) stateKind {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.m[userID]
	if !ok || s.now().After(st.expires) {
		delete(s.m, userID)
		return stateNone
	}
	return st.kind
}

func (s *states) clear(userID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, userID)
}
