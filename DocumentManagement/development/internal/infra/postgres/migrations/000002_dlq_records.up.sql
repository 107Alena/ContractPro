-- DLQ records table for replay support (DM-TASK-023).
-- Persisted to PostgreSQL (not Redis) so records survive TTL expiration (BRE-011).

BEGIN;

CREATE TABLE IF NOT EXISTS dm_dlq_records (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    original_topic   TEXT        NOT NULL,
    original_message JSONB       NOT NULL,
    error_code       TEXT        NOT NULL,
    error_message    TEXT        NOT NULL,
    correlation_id   TEXT        NOT NULL DEFAULT '',
    job_id           TEXT        NOT NULL DEFAULT '',
    category         TEXT        NOT NULL,
    failed_at        TIMESTAMPTZ NOT NULL,
    replay_count     INT         NOT NULL DEFAULT 0,
    last_replayed_at TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_dlq_records_correlation ON dm_dlq_records (correlation_id) WHERE correlation_id != '';
CREATE INDEX idx_dlq_records_category    ON dm_dlq_records (category);
CREATE INDEX idx_dlq_records_created_at  ON dm_dlq_records (created_at);

COMMIT;
