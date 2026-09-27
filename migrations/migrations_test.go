package migrations_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMySQLMigrationDeclaresCaseSensitiveIdempotentSchema(t *testing.T) {
	sql := readMigration(t, "mysql", "001_chainindex.sql")
	requireContains(t, sql,
		"CREATE TABLE IF NOT EXISTS chainindex_cursors",
		"CREATE TABLE IF NOT EXISTS chainindex_events",
		"DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_bin",
		"chain_namespace VARCHAR(32) NOT NULL",
		"chain_id VARCHAR(128) NOT NULL",
		"job_id VARCHAR(128) NOT NULL",
		"cursor VARCHAR(256) NOT NULL",
		"canonical_hash VARCHAR(256) NOT NULL",
		"status VARCHAR(32) NOT NULL",
		"last_error TEXT",
		"updated_at TIMESTAMP(6) NOT NULL",
		"PRIMARY KEY (chain_namespace, chain_id, job_id)",
		"transaction_id VARCHAR(256) NOT NULL",
		"event_index VARCHAR(128) NOT NULL",
		"emitter VARCHAR(256) NOT NULL",
		"event_type VARCHAR(256) NOT NULL",
		"occurred_at TIMESTAMP(6) NOT NULL",
		"payload JSON NOT NULL",
		"arguments JSON NOT NULL",
		"UNIQUE KEY chainindex_events_identity (chain_namespace, chain_id, job_id, transaction_id, event_index)",
	)
}

func TestPostgresMigrationDeclaresCaseSensitiveIdempotentSchema(t *testing.T) {
	sql := readMigration(t, "postgres", "001_chainindex.sql")
	requireContains(t, sql,
		"CREATE TABLE IF NOT EXISTS chainindex_cursors",
		"CREATE TABLE IF NOT EXISTS chainindex_events",
		"chain_namespace VARCHAR(32) COLLATE \"C\" NOT NULL",
		"chain_id VARCHAR(128) COLLATE \"C\" NOT NULL",
		"job_id VARCHAR(128) COLLATE \"C\" NOT NULL",
		"cursor VARCHAR(256) NOT NULL",
		"canonical_hash VARCHAR(256) NOT NULL",
		"status VARCHAR(32) NOT NULL",
		"last_error TEXT",
		"updated_at TIMESTAMPTZ NOT NULL",
		"PRIMARY KEY (chain_namespace, chain_id, job_id)",
		"transaction_id VARCHAR(256) COLLATE \"C\" NOT NULL",
		"event_index VARCHAR(128) COLLATE \"C\" NOT NULL",
		"emitter VARCHAR(256) NOT NULL",
		"event_type VARCHAR(256) NOT NULL",
		"occurred_at TIMESTAMPTZ NOT NULL",
		"payload JSONB NOT NULL",
		"arguments JSONB NOT NULL",
		"UNIQUE (chain_namespace, chain_id, job_id, transaction_id, event_index)",
	)
}

func readMigration(t *testing.T, dialect, name string) string {
	t.Helper()
	path := filepath.Join(dialect, name)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration %s: %v", path, err)
	}
	return string(contents)
}

func requireContains(t *testing.T, sql string, fragments ...string) {
	t.Helper()
	for _, fragment := range fragments {
		if !strings.Contains(sql, fragment) {
			t.Errorf("migration is missing %q", fragment)
		}
	}
}
