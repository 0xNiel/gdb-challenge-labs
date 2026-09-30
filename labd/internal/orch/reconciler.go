package orch

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"gdblabs/labd/internal/config"
	"gdblabs/labd/internal/store"
)

// ReconcileReport says what Reconcile did.
type ReconcileReport struct {
	Adopted    int `json:"adopted"`     // live containers with an open row, now managed again
	Removed    int `json:"removed"`     // containers killed and deleted
	ClosedRows int `json:"closed_rows"` // open rows closed with reason reconciled
}

// Reconcile makes labd's state match containerd's after a restart (spec "Reconciliation on
// boot", invariant S18). Call it once, before serving requests:
//
//   - a container without an open session row, or past its hard TTL, is killed and deleted;
//   - a container with an open row and a live task is adopted: its terminal is re-attached,
//     the slot is taken again, the hard TTL keeps its original deadline and the idle timer
//     starts afresh (labd cannot know when the last keystroke was);
//   - a container whose task is dead, or whose row says it was ending, is deleted;
//   - every open row left without a container is closed with reason reconciled.
func (m *Manager) Reconcile(ctx context.Context) (ReconcileReport, error) {
	var rep ReconcileReport
	rows, err := m.st.OpenSessions(ctx)
	if err != nil {
		return rep, fmt.Errorf("read open sessions: %w", err)
	}
	open := make(map[string]store.Session, len(rows))
	for _, r := range rows {
		open[r.ID] = r
	}
	infos, err := m.rt.List(ctx)
	if err != nil {
		return rep, fmt.Errorf("list containers: %w", err)
	}

	now := m.clk.Now()
	var errs []error
	for _, info := range infos {
		sid := info.Labels[LabelSessionID]
		row, hasRow := open[sid]
		delete(open, sid)
		created, ttl, idle := containerTimes(info.Labels, m.defaultsCopy())
		expired := !created.IsZero() && !now.Before(created.Add(ttl))

		adopt := hasRow && !expired && row.State != string(StateEnding)
		var c Container
		if adopt {
			c, err = m.rt.Attach(ctx, info.ID)
			if err != nil {
				if !errors.Is(err, ErrNoTask) {
					m.log.Warn("reconcile: attach failed; removing", "container", info.ID, "err", err)
				}
				adopt = false
			}
		}
		if !adopt {
			reason := "no open session row"
			switch {
			case expired:
				reason = "past its hard TTL"
			case hasRow && row.State == string(StateEnding):
				reason = "was ending"
			case hasRow:
				reason = "task not running"
			}
			m.log.Info("reconcile: removing container", "container", info.ID, "session_id", sid, "why", reason)
			if err := m.rt.Remove(ctx, info.ID); err != nil {
				errs = append(errs, err)
				continue
			}
			rep.Removed++
			if hasRow {
				m.closeRow(ctx, row, now)
				rep.ClosedRows++
			}
			continue
		}

		m.adopt(row, info, c, created, ttl, idle, now)
		rep.Adopted++
	}
	for _, row := range open {
		m.log.Info("reconcile: closing row without a container", "session_id", row.ID, "state", row.State)
		m.closeRow(ctx, row, now)
		rep.ClosedRows++
	}
	m.log.Info("reconciled", "adopted", rep.Adopted, "removed", rep.Removed, "closed_rows", rep.ClosedRows)
	return rep, errors.Join(errs...)
}

func (m *Manager) defaultsCopy() config.Limits {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.defaults
}

// adopt registers a live container as a running session.
func (m *Manager) adopt(row store.Session, info ContainerInfo, c Container, created time.Time,
	ttl, idle time.Duration, now time.Time) {
	m.mu.Lock()
	lim := mergeLimits(m.challenges[row.ChallengeSlug].Limits, m.defaults)
	lim.TTLMinutes, lim.IdleMinutes = int(ttl/time.Minute), int(idle/time.Minute)
	if created.IsZero() {
		created = row.CreatedAt
	}
	started := row.StartedAt
	if started.IsZero() {
		started = now
	}
	s := &Session{
		ID: row.ID, ContainerID: info.ID, UserID: row.UserID, ChallengeSlug: row.ChallengeSlug,
		Image: row.Image, Limits: lim, State: StateRunning, Extended: row.Extended,
		CreatedAt: row.CreatedAt, AdmittedAt: created, StartedAt: started, ctr: c, out: newOutput(),
		idleWindow: idle, ended: make(chan struct{}), commands: row.Commands,
	}
	if s.Extended {
		s.idleWindow += minutes(lim.ExtendMinutes)
	}
	m.armIdleLocked(s)
	s.hardDeadline = created.Add(ttl)
	id := s.ID
	s.hard = m.clk.AfterFunc(s.hardDeadline.Sub(now), func() { m.expire(id, ReasonHardTTL) })
	m.inUse++
	m.sessions[s.ID] = s
	m.byUser[s.UserID] = s
	m.mu.Unlock()
	// Nobody is attached after a restart: the browser gets the usual grace to reconnect.
	m.ClientDetached(s.ID, 0)

	go m.pump(s, c)
	go m.watchExit(s, c)
	m.persist(s)
	m.log.Info("reconcile: adopted", "session_id", s.ID, "container", info.ID,
		"hard_deadline", s.hardDeadline.Format(time.RFC3339))
}

// closeRow marks an open row finished by the reconciler.
func (m *Manager) closeRow(ctx context.Context, row store.Session, now time.Time) {
	prev := row.State
	row.State, row.EndReason, row.EndedAt = string(StateEnded), ReasonReconciled, now
	if prev == string(StateQueued) {
		row.State = string(StateAbandoned)
	}
	if err := m.st.UpsertSession(ctx, row); err != nil {
		m.log.Error("reconcile: close row", "session_id", row.ID, "err", err)
	}
	ev := store.Event{TS: now, Type: "lab_ended", UserID: row.UserID, SessionID: row.ID,
		ChallengeSlug: row.ChallengeSlug, Data: map[string]any{"reason": ReasonReconciled, "previous_state": prev}}
	if err := m.st.InsertEvents(ctx, ev); err != nil {
		m.log.Error("reconcile: event", "session_id", row.ID, "err", err)
	}
}

// containerTimes reads lab.created_at and the TTL and idle limits from a container's labels,
// falling back to the defaults. created is zero when the label is missing or malformed.
func containerTimes(labels map[string]string, d config.Limits) (created time.Time, ttl, idle time.Duration) {
	created, _ = time.Parse(time.RFC3339Nano, labels[LabelCreatedAt])
	ttl, idle = minutes(d.TTLMinutes), minutes(d.IdleMinutes)
	if n, err := strconv.Atoi(labels[LabelTTLMinutes]); err == nil && n > 0 {
		ttl = minutes(n)
	}
	if n, err := strconv.Atoi(labels[LabelIdleMinutes]); err == nil && n > 0 {
		idle = minutes(n)
	}
	return created, ttl, idle
}
