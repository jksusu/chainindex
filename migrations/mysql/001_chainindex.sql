-- Run this migration manually against a MySQL database. It is safe to rerun.
-- Identity columns use utf8mb4_bin so chain-specific identifiers stay case-sensitive.

CREATE TABLE IF NOT EXISTS chainindex_cursors (
    chain_namespace VARCHAR(32) NOT NULL,
    chain_id VARCHAR(128) NOT NULL,
    job_id VARCHAR(128) NOT NULL,
    cursor VARCHAR(256) NOT NULL,
    canonical_hash VARCHAR(256) NOT NULL,
    status VARCHAR(32) NOT NULL,
    last_error TEXT,
    updated_at TIMESTAMP(6) NOT NULL,
    PRIMARY KEY (chain_namespace, chain_id, job_id)
) DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE TABLE IF NOT EXISTS chainindex_events (
    chain_namespace VARCHAR(32) NOT NULL,
    chain_id VARCHAR(128) NOT NULL,
    job_id VARCHAR(128) NOT NULL,
    transaction_id VARCHAR(256) NOT NULL,
    event_index VARCHAR(128) NOT NULL,
    emitter VARCHAR(256) NOT NULL,
    event_type VARCHAR(256) NOT NULL,
    cursor VARCHAR(256) NOT NULL,
    canonical_hash VARCHAR(256) NOT NULL,
    occurred_at TIMESTAMP(6) NOT NULL,
    payload JSON NOT NULL,
    arguments JSON NOT NULL,
    UNIQUE KEY chainindex_events_identity (chain_namespace, chain_id, job_id, transaction_id, event_index)
) DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;
