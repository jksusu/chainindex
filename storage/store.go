// Package storage persists chainindex's chain-neutral records through GORM.
package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
)

var ErrNilDB = errors.New("chainindex: storage DB is required")

const (
	StatusReady         = "ready"
	StatusReorgDetected = "reorg_detected"
)

// Store never opens, closes, migrates, or otherwise owns the caller's DB.
type Store struct{ db *gorm.DB }

func New(database any, legacyDialect ...Dialect) (*Store, error) {
	if database == nil {
		return nil, ErrNilDB
	}
	var db *gorm.DB
	switch value := database.(type) {
	case *gorm.DB:
		db = value
	case *sql.DB:
		if len(legacyDialect) != 1 { return nil, ErrNilDB }
		var err error
		if legacyDialect[0] == Postgres { db, err = gorm.Open(postgres.New(postgres.Config{Conn: value}), &gorm.Config{}) } else { db, err = gorm.Open(mysql.New(mysql.Config{Conn: value, SkipInitializeWithVersion: true}), &gorm.Config{}) }
		if err != nil { return nil, err }
	default:
		return nil, ErrNilDB
	}
	return &Store{db: db}, nil
}

// Dialect is the GORM driver's dialect name (for example "postgres" or
// "mysql"). It is informational; GORM generates portable conflict clauses.
func (s *Store) Dialect() string { return s.db.Dialector.Name() }

func (s *Store) Begin(ctx context.Context) (*gorm.DB, error) {
	tx := s.db.WithContext(ctx).Begin()
	return tx, tx.Error
}

type CursorKey struct {
	ChainNamespace string
	ChainID        string
	JobID          string
}

type CursorState struct {
	Cursor        string
	CanonicalHash string
	Status        string
	LastError     string
}

// Event contains stable chain-neutral identifiers and lossless EVM log fields.
type Event struct {
	ChainNamespace  string
	ChainID         string
	JobID           string
	BlockNumber     string
	BlockHash       string
	TransactionHash string
	TransactionIndex string
	LogIndex        string
	Address         string
	Topic0          string
	Topics          []byte
	Data            string
	Removed         bool
	EventName       string
	DecodedArgs     []byte
	RawLog          []byte
	OccurredAt      time.Time
	// Deprecated normalized aliases retained for callers upgrading from v0.
	TransactionID string
	EventIndex    string
	Emitter       string
	EventType     string
	Cursor        string
	CanonicalHash string
	Payload       []byte
	Arguments     []byte
}

// Block is an adapter-normalized scanned block. All quantities are strings.
type Block struct {
	ChainNamespace   string
	ChainID          string
	BlockNumber      string
	BlockHash        string
	ParentHash       string
	Timestamp        time.Time
	Miner            string
	GasLimit         string
	GasUsed          string
	BaseFeePerGas    string
	TransactionsRoot string
	StateRoot        string
	ReceiptsRoot     string
	LogsBloom        string
	RawBlock         []byte
}

type cursorRecord struct {
	ChainNamespace string    `gorm:"column:chain_namespace;primaryKey"`
	ChainID        string    `gorm:"column:chain_id;primaryKey"`
	JobID          string    `gorm:"column:job_id;primaryKey"`
	Cursor         string    `gorm:"column:cursor"`
	CanonicalHash  string    `gorm:"column:canonical_hash"`
	Status         string    `gorm:"column:status"`
	LastError      *string   `gorm:"column:last_error"`
	UpdatedAt      time.Time `gorm:"column:updated_at"`
}

func (cursorRecord) TableName() string { return "chainindex_cursors" }

type eventRecord struct {
	ChainNamespace   string    `gorm:"column:chain_namespace"`
	ChainID          string    `gorm:"column:chain_id"`
	JobID            string    `gorm:"column:job_id"`
	BlockNumber      string    `gorm:"column:block_number"`
	BlockHash        string    `gorm:"column:block_hash"`
	TransactionHash  string    `gorm:"column:transaction_hash"`
	TransactionIndex string    `gorm:"column:transaction_index"`
	LogIndex         string    `gorm:"column:log_index"`
	Address          string    `gorm:"column:address"`
	Topic0           string    `gorm:"column:topic0"`
	Topics           []byte    `gorm:"column:topics"`
	Data             string    `gorm:"column:data"`
	Removed          bool      `gorm:"column:removed"`
	EventName        string    `gorm:"column:event_name"`
	DecodedArgs      []byte    `gorm:"column:decoded_args"`
	RawLog           []byte    `gorm:"column:raw_log"`
	OccurredAt       time.Time `gorm:"column:occurred_at"`
}

func (eventRecord) TableName() string { return "chainindex_events" }

type blockRecord struct {
	ChainNamespace   string    `gorm:"column:chain_namespace"`
	ChainID          string    `gorm:"column:chain_id"`
	BlockNumber      string    `gorm:"column:block_number"`
	BlockHash        string    `gorm:"column:block_hash"`
	ParentHash       string    `gorm:"column:parent_hash"`
	Timestamp        time.Time `gorm:"column:timestamp"`
	Miner            string    `gorm:"column:miner"`
	GasLimit         string    `gorm:"column:gas_limit"`
	GasUsed          string    `gorm:"column:gas_used"`
	BaseFeePerGas    string    `gorm:"column:base_fee_per_gas"`
	TransactionsRoot string    `gorm:"column:transactions_root"`
	StateRoot        string    `gorm:"column:state_root"`
	ReceiptsRoot     string    `gorm:"column:receipts_root"`
	LogsBloom        string    `gorm:"column:logs_bloom"`
	RawBlock         []byte    `gorm:"column:raw_block"`
}

func (blockRecord) TableName() string { return "chainindex_blocks" }

func (s *Store) LoadCursor(ctx context.Context, tx *gorm.DB, key CursorKey) (CursorState, bool, error) {
	var row cursorRecord
	err := tx.WithContext(ctx).Where("chain_namespace = ? AND chain_id = ? AND job_id = ?", key.ChainNamespace, key.ChainID, key.JobID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) { return CursorState{}, false, nil }
	if err != nil { return CursorState{}, false, err }
	state := CursorState{Cursor: row.Cursor, CanonicalHash: row.CanonicalHash, Status: row.Status}
	if row.LastError != nil { state.LastError = *row.LastError }
	return state, true, nil
}

func (s *Store) InsertBlock(ctx context.Context, tx *gorm.DB, block Block) error {
	row := blockRecord{ChainNamespace: block.ChainNamespace, ChainID: block.ChainID, BlockNumber: block.BlockNumber, BlockHash: block.BlockHash, ParentHash: block.ParentHash, Timestamp: block.Timestamp, Miner: block.Miner, GasLimit: block.GasLimit, GasUsed: block.GasUsed, BaseFeePerGas: block.BaseFeePerGas, TransactionsRoot: block.TransactionsRoot, StateRoot: block.StateRoot, ReceiptsRoot: block.ReceiptsRoot, LogsBloom: block.LogsBloom, RawBlock: block.RawBlock}
	return tx.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "chain_namespace"}, {Name: "chain_id"}, {Name: "block_number"}}, DoUpdates: clause.AssignmentColumns([]string{"block_hash", "parent_hash", "timestamp", "miner", "gas_limit", "gas_used", "base_fee_per_gas", "transactions_root", "state_root", "receipts_root", "logs_bloom", "raw_block"})}).Create(&row).Error
}

// InsertEvent records an event once. True means a user handler must run.
func (s *Store) InsertEvent(ctx context.Context, tx *gorm.DB, event Event) (bool, error) {
	if event.BlockNumber == "" { event.BlockNumber = event.Cursor }
	if event.BlockHash == "" { event.BlockHash = event.CanonicalHash }
	if event.TransactionHash == "" { event.TransactionHash = event.TransactionID }
	if event.LogIndex == "" { event.LogIndex = event.EventIndex }
	if event.Address == "" { event.Address = event.Emitter }
	if event.Topic0 == "" { event.Topic0 = event.EventType }
	if len(event.DecodedArgs) == 0 { event.DecodedArgs = event.Arguments }
	if len(event.RawLog) == 0 { event.RawLog = event.Payload }
	row := eventRecord{ChainNamespace: event.ChainNamespace, ChainID: event.ChainID, JobID: event.JobID, BlockNumber: event.BlockNumber, BlockHash: event.BlockHash, TransactionHash: event.TransactionHash, TransactionIndex: event.TransactionIndex, LogIndex: event.LogIndex, Address: event.Address, Topic0: event.Topic0, Topics: event.Topics, Data: event.Data, Removed: event.Removed, EventName: event.EventName, DecodedArgs: event.DecodedArgs, RawLog: event.RawLog, OccurredAt: event.OccurredAt}
	result := tx.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "chain_namespace"}, {Name: "chain_id"}, {Name: "job_id"}, {Name: "transaction_hash"}, {Name: "log_index"}}, DoNothing: true}).Create(&row)
	return result.RowsAffected == 1, result.Error
}

func (s *Store) CreateCursor(ctx context.Context, tx *gorm.DB, key CursorKey, state CursorState) error {
	row := cursorRecord{ChainNamespace: key.ChainNamespace, ChainID: key.ChainID, JobID: key.JobID, Cursor: state.Cursor, CanonicalHash: state.CanonicalHash, Status: readyStatus(state.Status), LastError: nullableString(state.LastError), UpdatedAt: time.Now().UTC()}
	return tx.WithContext(ctx).Create(&row).Error
}

func (s *Store) AdvanceCursor(ctx context.Context, tx *gorm.DB, key CursorKey, expectedCursor string, state CursorState) (bool, error) {
	updates := map[string]any{"cursor": state.Cursor, "canonical_hash": state.CanonicalHash, "status": readyStatus(state.Status), "last_error": nullableString(state.LastError), "updated_at": time.Now().UTC()}
	result := tx.WithContext(ctx).Model(&cursorRecord{}).Where("chain_namespace = ? AND chain_id = ? AND job_id = ? AND cursor = ?", key.ChainNamespace, key.ChainID, key.JobID, expectedCursor).Updates(updates)
	return result.RowsAffected == 1, result.Error
}

func (s *Store) RecordReorg(ctx context.Context, tx *gorm.DB, key CursorKey, message string) error {
	return tx.WithContext(ctx).Model(&cursorRecord{}).Where("chain_namespace = ? AND chain_id = ? AND job_id = ?", key.ChainNamespace, key.ChainID, key.JobID).Updates(map[string]any{"status": StatusReorgDetected, "last_error": message, "updated_at": time.Now().UTC()}).Error
}

func readyStatus(status string) string { if status == "" { return StatusReady }; return status }
func nullableString(value string) *string { if value == "" { return nil }; return &value }
