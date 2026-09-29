package store

import (
	"context"
	"maps"
	"slices"
	"sync"
)

// Memory is an in-process Store for unit tests.
type Memory struct {
	mu       sync.Mutex
	sessions map[string]Session
	events   []Event
	fail     error
}

// NewMemory returns an empty store.
func NewMemory() *Memory { return &Memory{sessions: map[string]Session{}} }

// FailWrites makes every later write return err (nil restores normal behaviour).
func (m *Memory) FailWrites(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fail = err
}

func (m *Memory) UpsertSession(_ context.Context, s Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	m.sessions[s.ID] = s
	return nil
}

func (m *Memory) OpenSessions(context.Context) ([]Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Session
	for _, s := range m.sessions {
		if s.Open() {
			out = append(out, s)
		}
	}
	slices.SortFunc(out, func(a, b Session) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return out, nil
}

func (m *Memory) InsertEvents(_ context.Context, evs ...Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	for _, e := range evs {
		e.Data = maps.Clone(e.Data)
		m.events = append(m.events, e)
	}
	return nil
}

func (m *Memory) Close() {}

// Session returns the stored row for id.
func (m *Memory) Session(id string) (Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	return s, ok
}

// Events returns a copy of every event, oldest first.
func (m *Memory) Events() []Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.events)
}
