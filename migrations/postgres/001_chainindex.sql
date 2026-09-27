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
    block_number BIGINT NOT NULL,
    block_hash VARCHAR(66) COLLATE "C" NOT NULL,
    transaction_hash VARCHAR(66) COLLATE "C" NOT NULL,
    transaction_index BIGINT NOT NULL,
    log_index BIGINT NOT NULL,
    address VARCHAR(42) COLLATE "C" NOT NULL,
    topic0 VARCHAR(66) COLLATE "C" NOT NULL,
    topics JSONB NOT NULL,
    data TEXT NOT NULL,
    removed BOOLEAN NOT NULL DEFAULT FALSE,
    event_name VARCHAR(256),
    decoded_args JSONB NOT NULL,
    raw_log JSONB NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    UNIQUE (chain_namespace, chain_id, job_id, transaction_hash, log_index)
);

CREATE TABLE IF NOT EXISTS chainindex_blocks (
    chain_namespace VARCHAR(32) COLLATE "C" NOT NULL,
    chain_id VARCHAR(128) COLLATE "C" NOT NULL,
    block_number BIGINT NOT NULL,
    block_hash VARCHAR(66) COLLATE "C" NOT NULL,
    parent_hash VARCHAR(66) COLLATE "C" NOT NULL,
    timestamp TIMESTAMPTZ NOT NULL,
    miner VARCHAR(42) COLLATE "C" NOT NULL,
    gas_limit NUMERIC(78,0) NOT NULL,
    gas_used NUMERIC(78,0) NOT NULL,
    base_fee_per_gas NUMERIC(78,0),
    transactions_root VARCHAR(66) COLLATE "C" NOT NULL,
    state_root VARCHAR(66) COLLATE "C" NOT NULL,
    receipts_root VARCHAR(66) COLLATE "C" NOT NULL,
    logs_bloom TEXT NOT NULL,
    raw_block JSONB NOT NULL,
    PRIMARY KEY (chain_namespace, chain_id, block_number)
);
