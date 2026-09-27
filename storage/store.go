package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrNilDB = errors.New("chainindex: storage DB is required")

const (
	StatusReady         = "ready"
	StatusReorgDetected = "reorg_detected"
)

// DB is the only database capability required to construct a Store.
// *sql.DB satisfies it directly, while tests and applications may supply a wrapper.
type DB interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

// Store persists normalized events and per-job chain cursors.
type Store struct {
	db      DB
	dialect Dialect
}

// CursorKey identifies one independently checkpointed index job.
type CursorKey struct {
	ChainNamespace string
	ChainID        string
	JobID          string
}

// CursorState is the durable checkpoint and its safety status.
type CursorState struct {
	Cursor        string
	CanonicalHash string
	Status        string
	LastError     string
}

// Event is the storage representation of a normalized chain event.
type Event struct {
	ChainNamespace string
	ChainID        string
	JobID          string
	TransactionID  string
	EventIndex     string
	Emitter        string
	EventType      string
	Cursor         string
	CanonicalHash  string
	OccurredAt     time.Time
	Payload        []byte
	Arguments      []byte
}

// New creates a Store that emits SQL for dialect.
func New(db DB, dialect Dialect) (*Store, error) {
	if db == nil {
		return nil, ErrNilDB
	}
	if err := dialect.Validate(); err != nil {
		return nil, err
	}
	return &Store{db: db, dialect: dialect}, nil
}

// Begin starts a transaction owned by the caller. Store never commits or rolls it back.
func (s *Store) Begin(ctx context.Context) (*sql.Tx, error) {
	return s.db.BeginTx(ctx, nil)
}

// LoadCursor obtains an existing checkpoint. found is false when no row exists.
func (s *Store) LoadCursor(ctx context.Context, tx *sql.Tx, key CursorKey) (state CursorState, found bool, err error) {
	row := tx.QueryRowContext(ctx, fmt.Sprintf(`SELECT cursor, canonical_hash, status, last_error FROM chainindex_cursors WHERE chain_namespace = %s AND chain_id = %s AND job_id = %s`, s.bind(1), s.bind(2), s.bind(3)), key.ChainNamespace, key.ChainID, key.JobID)
	var lastError sql.NullString
	if err := row.Scan(&state.Cursor, &state.CanonicalHash, &state.Status, &lastError); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return CursorState{}, false, nil
		}
		return CursorState{}, false, err
	}
	state.LastError = lastError.String
	return state, true, nil
}

// InsertEvent records event exactly once. A true result means a handler should run.
func (s *Store) InsertEvent(ctx context.Context, tx *sql.Tx, event Event) (bool, error) {
	query := fmt.Sprintf(`INSERT INTO chainindex_events (chain_namespace, chain_id, job_id, transaction_id, event_index, emitter, event_type, cursor, canonical_hash, occurred_at, payload, arguments) VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s) %s`, s.bind(1), s.bind(2), s.bind(3), s.bind(4), s.bind(5), s.bind(6), s.bind(7), s.bind(8), s.bind(9), s.bind(10), s.bind(11), s.bind(12), s.eventDuplicateClause())
	result, err := tx.ExecContext(ctx, query, event.ChainNamespace, event.ChainID, event.JobID, event.TransactionID, event.EventIndex, event.Emitter, event.EventType, event.Cursor, event.CanonicalHash, event.OccurredAt, event.Payload, event.Arguments)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// CreateCursor stores the first durable checkpoint for key.
func (s *Store) CreateCursor(ctx context.Context, tx *sql.Tx, key CursorKey, state CursorState) error {
	_, err := tx.ExecContext(ctx, fmt.Sprintf(`INSERT INTO chainindex_cursors (chain_namespace, chain_id, job_id, cursor, canonical_hash, status, last_error, updated_at) VALUES (%s, %s, %s, %s, %s, %s, %s, %s)`, s.bind(1), s.bind(2), s.bind(3), s.bind(4), s.bind(5), s.bind(6), s.bind(7), s.bind(8)), key.ChainNamespace, key.ChainID, key.JobID, state.Cursor, state.CanonicalHash, readyStatus(state.Status), nullableString(state.LastError), time.Now().UTC())
	return err
}

// AdvanceCursor updates a checkpoint only when it still equals expectedCursor.
// It returns false when another transaction has already advanced or rewound it.
func (s *Store) AdvanceCursor(ctx context.Context, tx *sql.Tx, key CursorKey, expectedCursor string, state CursorState) (bool, error) {
	query := fmt.Sprintf(`UPDATE chainindex_cursors SET cursor = %s, canonical_hash = %s, status = %s, last_error = %s, updated_at = %s WHERE chain_namespace = %s AND chain_id = %s AND job_id = %s AND cursor = %s`, s.bind(1), s.bind(2), s.bind(3), s.bind(4), s.bind(5), s.bind(6), s.bind(7), s.bind(8), s.bind(9))
	result, err := tx.ExecContext(ctx, query, state.Cursor, state.CanonicalHash, readyStatus(state.Status), nullableString(state.LastError), time.Now().UTC(), key.ChainNamespace, key.ChainID, key.JobID, expectedCursor)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// RecordReorg marks a job as blocked until an explicit rewind is performed.
func (s *Store) RecordReorg(ctx context.Context, tx *sql.Tx, key CursorKey, message string) error {
	query := fmt.Sprintf(`UPDATE chainindex_cursors SET status = %s, last_error = %s, updated_at = %s WHERE chain_namespace = %s AND chain_id = %s AND job_id = %s`, s.bind(1), s.bind(2), s.bind(3), s.bind(4), s.bind(5), s.bind(6))
	_, err := tx.ExecContext(ctx, query, StatusReorgDetected, message, time.Now().UTC(), key.ChainNamespace, key.ChainID, key.JobID)
	return err
}

func (s *Store) bind(n int) string {
	if s.dialect == MySQL {
		return "?"
	}
	return fmt.Sprintf("$%d", n)
}

func (s *Store) eventDuplicateClause() string {
	if s.dialect == MySQL {
		return "ON DUPLICATE KEY UPDATE transaction_id = transaction_id"
	}
	return "ON CONFLICT (chain_namespace, chain_id, job_id, transaction_id, event_index) DO NOTHING"
}

func readyStatus(status string) string {
	if status == "" {
		return StatusReady
	}
	return status
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
