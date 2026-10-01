// Package store persists labd's tables: sessions, events and samples (spec "Data model";
// labd owns them, ADR 0003). Postgres in production (postgres.go), in memory for tests
// (memory.go). Migrations are embedded SQL files applied by `labd migrate` and on boot.
package store

import (
	"context"
	"time"
)

// Session is one row of the sessions table.
type Session struct {
	ID            string // uuid
	UserID        int64
	ChallengeSlug string
	Image         string
	State         string // queued, creating, running, ending, ended, failed, abandoned
	CreatedAt     time.Time
	StartedAt     time.Time // zero until running
	EndedAt       time.Time // zero until ended, failed or abandoned
	EndReason     string
	ContainerID   string
	Extended      bool
	PeakRSSMB     float64 // filled by the sampler (Phase 7)
	Commands      int     // filled by command capture (Phase 3)
}

// Open reports whether a session row still describes something that may be alive.
func (s Session) Open() bool {
	switch s.State {
	case "queued", "creating", "running", "ending":
		return true
	}
	return false
}

// Event is one row of the events table.
type Event struct {
	TS            time.Time
	Type          string
	UserID        int64  // 0 for none
	SessionID     string // "" for none
	ChallengeSlug string
	Data          map[string]any
}

// Sample is one row of the samples table: a gauge or a delta at ts (spec "Sample schema").
type Sample struct {
	TS        time.Time
	SessionID string // "" for host and labd metrics
	Metric    string
	Value     float64
}

// Store is what labd needs from persistence.
type Store interface {
	// UpsertSession writes the row, inserting it on first use.
	UpsertSession(ctx context.Context, s Session) error
	// OpenSessions returns every row whose state is queued, creating, running or ending.
	OpenSessions(ctx context.Context) ([]Session, error)
	// InsertEvents appends events in one batch.
	InsertEvents(ctx context.Context, evs ...Event) error
	// InsertSamples appends samples in one batch (the sampler, every metrics_flush_s).
	InsertSamples(ctx context.Context, ss ...Sample) error
	Close()
}
