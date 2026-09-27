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
    block_number BIGINT UNSIGNED NOT NULL,
    block_hash VARCHAR(66) NOT NULL,
    transaction_hash VARCHAR(66) NOT NULL,
    transaction_index BIGINT UNSIGNED NOT NULL,
    log_index BIGINT UNSIGNED NOT NULL,
    address VARCHAR(42) NOT NULL,
    topic0 VARCHAR(66) NOT NULL,
    topics JSON NOT NULL,
    data LONGTEXT NOT NULL,
    removed BOOLEAN NOT NULL DEFAULT FALSE,
    event_name VARCHAR(256) NULL,
    decoded_args JSON NOT NULL,
    raw_log JSON NOT NULL,
    occurred_at TIMESTAMP(6) NOT NULL,
    UNIQUE KEY chainindex_events_identity (chain_namespace, chain_id, job_id, transaction_hash, log_index)
) DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE TABLE IF NOT EXISTS chainindex_blocks (
    chain_namespace VARCHAR(32) NOT NULL,
    chain_id VARCHAR(128) NOT NULL,
    block_number BIGINT UNSIGNED NOT NULL,
    block_hash VARCHAR(66) NOT NULL,
    parent_hash VARCHAR(66) NOT NULL,
    timestamp TIMESTAMP(6) NOT NULL,
    miner VARCHAR(42) NOT NULL,
    gas_limit DECIMAL(78,0) NOT NULL,
    gas_used DECIMAL(78,0) NOT NULL,
    base_fee_per_gas DECIMAL(78,0) NULL,
    transactions_root VARCHAR(66) NOT NULL,
    state_root VARCHAR(66) NOT NULL,
    receipts_root VARCHAR(66) NOT NULL,
    logs_bloom LONGTEXT NOT NULL,
    raw_block JSON NOT NULL,
    PRIMARY KEY (chain_namespace, chain_id, block_number)
) DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;
