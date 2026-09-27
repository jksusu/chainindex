-- Run this migration manually against a PostgreSQL database. It is safe to rerun.
-- Identity columns use the C collation so chain-specific identifiers stay case-sensitive.

CREATE TABLE IF NOT EXISTS chainindex_cursors (
    chain_namespace VARCHAR(32) COLLATE "C" NOT NULL,
    chain_id VARCHAR(128) COLLATE "C" NOT NULL,
    job_id VARCHAR(128) COLLATE "C" NOT NULL,
    cursor VARCHAR(256) NOT NULL,
    canonical_hash VARCHAR(256) NOT NULL,
    status VARCHAR(32) NOT NULL,
    last_error TEXT,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (chain_namespace, chain_id, job_id)
);

CREATE TABLE IF NOT EXISTS chainindex_events (
    chain_namespace VARCHAR(32) COLLATE "C" NOT NULL,
    chain_id VARCHAR(128) COLLATE "C" NOT NULL,
    job_id VARCHAR(128) COLLATE "C" NOT NULL,
    transaction_id VARCHAR(256) COLLATE "C" NOT NULL,
    event_index VARCHAR(128) COLLATE "C" NOT NULL,
    emitter VARCHAR(256) NOT NULL,
    event_type VARCHAR(256) NOT NULL,
    cursor VARCHAR(256) NOT NULL,
    canonical_hash VARCHAR(256) NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    payload JSONB NOT NULL,
    arguments JSONB NOT NULL,
    UNIQUE (chain_namespace, chain_id, job_id, transaction_id, event_index)
);
