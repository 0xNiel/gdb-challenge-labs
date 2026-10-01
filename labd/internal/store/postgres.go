package store

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

// migrateLockID is the advisory lock key that serialises concurrent `labd migrate` runs.
const migrateLockID = 0x6c616264 // "labd"

// Postgres is the production Store (pgx pool).
type Postgres struct {
	pool *pgxpool.Pool
}

// OpenPostgres connects to dsn and checks the connection. Unknown DSN query parameters
// (for example search_path) become session settings.
func OpenPostgres(ctx context.Context, dsn string) (*Postgres, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres config: %w", err)
	}
	pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: %w", err)
	}
	return &Postgres{pool: pool}, nil
}

func (p *Postgres) Close() { p.pool.Close() }

// Migrate applies every embedded migration not yet recorded in schema_migrations, each in
// its own transaction, in file-name order. It returns the names it applied.
func (p *Postgres) Migrate(ctx context.Context) ([]string, error) {
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return nil, err
	}
	slices.Sort(names)
	conn, err := p.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrateLockID); err != nil {
		return nil, fmt.Errorf("migration lock: %w", err)
	}
	defer func() { _, _ = conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, migrateLockID) }()
	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return nil, fmt.Errorf("schema_migrations: %w", err)
	}
	var applied []string
	for _, name := range names {
		version := strings.TrimSuffix(strings.TrimPrefix(name, "migrations/"), ".sql")
		var done bool
		if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, version).Scan(&done); err != nil {
			return applied, fmt.Errorf("check %s: %w", version, err)
		}
		if done {
			continue
		}
		sql, err := migrations.ReadFile(name)
		if err != nil {
			return applied, err
		}
		err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(sql)); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version)
			return err
		})
		if err != nil {
			return applied, fmt.Errorf("migration %s: %w", version, err)
		}
		applied = append(applied, version)
	}
	return applied, nil
}

func (p *Postgres) UpsertSession(ctx context.Context, s Session) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO sessions (id, user_id, challenge_slug, image, state, created_at, started_at,
		                      ended_at, end_reason, container_id, extended, commands, peak_rss_mb)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (id) DO UPDATE SET
		    state = EXCLUDED.state, started_at = EXCLUDED.started_at, ended_at = EXCLUDED.ended_at,
		    end_reason = EXCLUDED.end_reason, container_id = EXCLUDED.container_id,
		    extended = EXCLUDED.extended, commands = EXCLUDED.commands,
		    -- GREATEST ignores NULL: an adopted session's lower reading never lowers the peak.
		    peak_rss_mb = GREATEST(sessions.peak_rss_mb, EXCLUDED.peak_rss_mb)`,
		s.ID, s.UserID, s.ChallengeSlug, s.Image, s.State, s.CreatedAt, nullTime(s.StartedAt),
		nullTime(s.EndedAt), s.EndReason, s.ContainerID, s.Extended, s.Commands, nullFloat(s.PeakRSSMB))
	if err != nil {
		return fmt.Errorf("upsert session %s: %w", s.ID, err)
	}
	return nil
}

func (p *Postgres) OpenSessions(ctx context.Context) ([]Session, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id::text, user_id, challenge_slug, image, state, created_at, started_at, ended_at,
		       end_reason, container_id, extended, coalesce(peak_rss_mb, 0), commands
		FROM sessions WHERE state IN ('queued', 'creating', 'running', 'ending')
		ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("open sessions: %w", err)
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var s Session
		var started, ended *time.Time
		if err := rows.Scan(&s.ID, &s.UserID, &s.ChallengeSlug, &s.Image, &s.State, &s.CreatedAt,
			&started, &ended, &s.EndReason, &s.ContainerID, &s.Extended, &s.PeakRSSMB, &s.Commands); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		if started != nil {
			s.StartedAt = *started
		}
		if ended != nil {
			s.EndedAt = *ended
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (p *Postgres) InsertEvents(ctx context.Context, evs ...Event) error {
	if len(evs) == 0 {
		return nil
	}
	b := &pgx.Batch{}
	for _, e := range evs {
		data := e.Data
		if data == nil {
			data = map[string]any{}
		}
		b.Queue(`INSERT INTO events (ts, type, user_id, session_id, challenge_slug, data)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			e.TS, e.Type, nullInt(e.UserID), nullString(e.SessionID), nullString(e.ChallengeSlug), data)
	}
	if err := p.pool.SendBatch(ctx, b).Close(); err != nil {
		return fmt.Errorf("insert events: %w", err)
	}
	return nil
}

// InsertSamples writes samples with COPY: one round trip per flush, however many labs.
func (p *Postgres) InsertSamples(ctx context.Context, ss ...Sample) error {
	if len(ss) == 0 {
		return nil
	}
	_, err := p.pool.CopyFrom(ctx, pgx.Identifier{"samples"}, []string{"ts", "session_id", "metric", "value"},
		pgx.CopyFromSlice(len(ss), func(i int) ([]any, error) {
			s := ss[i]
			return []any{s.TS, nullString(s.SessionID), s.Metric, s.Value}, nil
		}))
	if err != nil {
		return fmt.Errorf("insert samples: %w", err)
	}
	return nil
}

// Session reads one row by id (tests and tools).
func (p *Postgres) Session(ctx context.Context, id string) (Session, error) {
	var s Session
	var started, ended *time.Time
	err := p.pool.QueryRow(ctx, `
		SELECT id::text, user_id, challenge_slug, image, state, created_at, started_at, ended_at,
		       end_reason, container_id, extended, coalesce(peak_rss_mb, 0), commands
		FROM sessions WHERE id = $1`, id).Scan(&s.ID, &s.UserID, &s.ChallengeSlug, &s.Image,
		&s.State, &s.CreatedAt, &started, &ended, &s.EndReason, &s.ContainerID, &s.Extended,
		&s.PeakRSSMB, &s.Commands)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, fmt.Errorf("session %s: not found", id)
	}
	if err != nil {
		return s, err
	}
	if started != nil {
		s.StartedAt = *started
	}
	if ended != nil {
		s.EndedAt = *ended
	}
	return s, nil
}

// QueryInt runs a query returning one integer (tests and tools).
func (p *Postgres) QueryInt(ctx context.Context, sql string, args ...any) (int64, error) {
	var n int64
	err := p.pool.QueryRow(ctx, sql, args...).Scan(&n)
	return n, err
}

// Exec runs one statement (tests and tools).
func (p *Postgres) Exec(ctx context.Context, sql string, args ...any) error {
	_, err := p.pool.Exec(ctx, sql, args...)
	return err
}

func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func nullInt(v int64) *int64 {
	if v == 0 {
		return nil
	}
	return &v
}

func nullFloat(v float64) *float64 {
	if v == 0 {
		return nil
	}
	return &v
}

func nullString(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}
