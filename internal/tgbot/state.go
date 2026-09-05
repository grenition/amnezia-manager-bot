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
	stateAdminAddUser
	stateAdminDisableUser
	stateAdminLimitUser
	stateAdminLimitValue
)

const stateTTL = 10 * time.Minute

type userState struct {
	kind       stateKind
	expires    time.Time
	targetID   int64
	targetName string
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
	s.setTarget(userID, k, 0, "")
}

func (s *states) setTarget(userID int64, k stateKind, id int64, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[userID] = &userState{kind: k, expires: s.now().Add(stateTTL), targetID: id, targetName: name}
}

func (s *states) get(userID int64) stateKind {
	st, ok := s.live(userID)
	if !ok {
		return stateNone
	}
	return st.kind
}

func (s *states) target(userID int64) (int64, string) {
	st, ok := s.live(userID)
	if !ok {
		return 0, ""
	}
	return st.targetID, st.targetName
}

func (s *states) live(userID int64) (*userState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.m[userID]
	if !ok || s.now().After(st.expires) {
		delete(s.m, userID)
		return nil, false
	}
	return st, true
}

func (s *states) clear(userID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, userID)
}
