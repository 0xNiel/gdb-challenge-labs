-- 0001: labd's tables (spec "Data model"; column choices in ADR 0011).
-- Applied once by `labd migrate` or at boot; recorded in schema_migrations.

CREATE TABLE sessions (
    id             uuid PRIMARY KEY,
    user_id        bigint NOT NULL,
    challenge_slug text NOT NULL,
    image          text NOT NULL DEFAULT '',
    state          text NOT NULL CHECK (state IN
                     ('queued', 'creating', 'running', 'ending', 'ended', 'failed', 'abandoned')),
    created_at     timestamptz NOT NULL,
    started_at     timestamptz,
    ended_at       timestamptz,
    end_reason     text NOT NULL DEFAULT '',
    container_id   text NOT NULL DEFAULT '',
    extended       boolean NOT NULL DEFAULT false,
    peak_rss_mb    double precision,
    commands       integer NOT NULL DEFAULT 0
);
CREATE INDEX sessions_user_id_state_idx ON sessions (user_id, state);

CREATE TABLE events (
    id             bigserial PRIMARY KEY,
    ts             timestamptz NOT NULL DEFAULT now(),
    type           text NOT NULL,
    user_id        bigint,
    session_id     uuid,
    challenge_slug text,
    data           jsonb NOT NULL DEFAULT '{}'
);
CREATE INDEX events_ts_idx ON events (ts);
CREATE INDEX events_type_ts_idx ON events (type, ts);
CREATE INDEX events_session_id_idx ON events (session_id);

CREATE TABLE samples (
    ts         timestamptz NOT NULL,
    session_id uuid,
    metric     text NOT NULL,
    value      double precision NOT NULL
);
CREATE INDEX samples_ts_idx ON samples (ts);
CREATE INDEX samples_session_id_metric_ts_idx ON samples (session_id, metric, ts);

-- web reads sessions and samples and appends events (spec: events has two writers).
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'web') THEN
        GRANT SELECT ON sessions, samples TO web;
        GRANT SELECT, INSERT ON events TO web;
        GRANT USAGE, SELECT ON SEQUENCE events_id_seq TO web;
    END IF;
END
$$;
