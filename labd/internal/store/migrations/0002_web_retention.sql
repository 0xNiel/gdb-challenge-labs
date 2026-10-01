-- 0002: web's retention command (Phase 7, task 7.5) deletes samples older than 7 days and
-- events older than 90 days, so web may delete from both (ADR 0003, amended). The rollup
-- tables are web's own (Django migrations); labd never touches them.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'web') THEN
        GRANT DELETE ON samples, events TO web;
    END IF;
END
$$;
