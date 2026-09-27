// Package chainindex provides chain-neutral event indexing primitives.
package chainindex

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"
)

var (
	ErrInvalidJobID          = errors.New("chainindex: job ID is required")
	ErrInvalidChainNamespace = errors.New("chainindex: chain namespace is required")
	ErrInvalidChainID        = errors.New("chainindex: chain ID is required")
	ErrInvalidStartCursor    = errors.New("chainindex: start cursor is required")
	ErrInvalidBatchLimit     = errors.New("chainindex: batch limit must be greater than zero")
	ErrInvalidPollInterval   = errors.New("chainindex: poll interval must not be negative")
)

// Cursor is an adapter-defined, canonical chain position.
type Cursor struct {
	Value string
}

// Range is an inclusive adapter-defined span of chain positions.
type Range struct {
	From Cursor
	To   Cursor
}

// Event is a normalized chain event suitable for durable storage.
type Event struct {
	ChainNamespace string
	ChainID        string
	JobID          string
	Emitter        string
	EventType      string
	TransactionID  string
	EventIndex     string
	Cursor         Cursor
	CanonicalHash  string
	OccurredAt     time.Time
	Payload        json.RawMessage
	Arguments      json.RawMessage
	// Raw EVM log fields are populated by EVM adapters. Other adapters may
	// leave them empty while still using the normalized fields above.
	BlockNumber      string
	BlockHash        string
	TransactionHash  string
	TransactionIndex string
	LogIndex         string
	Address          string
	Topic0           string
	Topics           json.RawMessage
	Data             string
	Removed          bool
	EventName        string
	DecodedArgs      json.RawMessage
	RawLog           json.RawMessage
}

// Block is a normalized, durable representation of a scanned chain block.
// Numeric EVM values are strings so no chain quantity is truncated.
type Block struct {
	ChainNamespace  string
	ChainID         string
	Number          string
	Hash            string
	ParentHash      string
	Timestamp       time.Time
	Miner           string
	GasLimit        string
	GasUsed         string
	BaseFeePerGas   string
	TransactionsRoot string
	StateRoot       string
	ReceiptsRoot    string
	LogsBloom       string
	Raw             json.RawMessage
}

// EventIdentity is the stable, normalized uniqueness key for an Event.
type EventIdentity struct {
	ChainNamespace string
	ChainID        string
	JobID          string
	TransactionID  string
	EventIndex     string
}

// Identity returns the fields that uniquely identify an event within an index job.
func (e Event) Identity() EventIdentity {
	return EventIdentity{
		ChainNamespace: e.ChainNamespace,
		ChainID:        e.ChainID,
		JobID:          e.JobID,
		TransactionID:  e.TransactionID,
		EventIndex:     e.EventIndex,
	}
}

// ConfirmationPolicy controls how an adapter selects a safe chain head.
type ConfirmationPolicy struct {
	Confirmations uint64
}

// Registration is adapter-owned registration data for a Job.
type Registration any

// Registrations holds the adapter-owned registrations configured for a Job.
type Registrations []Registration

// Handler writes application-specific data for a newly persisted event.
// It must use only tx for database writes. A nil Handler is allowed and is a
// no-op, so a Job can persist only normalized events.
type Handler func(context.Context, *gorm.DB, Event) error

// Job configures an independently checkpointed event index.
type Job struct {
	ID             string
	ChainNamespace string
	ChainID        string
	// StartCursor is used only before a cursor has been persisted and must be non-empty.
	StartCursor        Cursor
	ConfirmationPolicy ConfirmationPolicy
	BatchLimit         uint64
	PollInterval       time.Duration
	Registrations      Registrations
	Handler            Handler
}

// Validate reports invalid job configuration before indexing starts.
func (j Job) Validate() error {
	switch {
	case j.ID == "":
		return ErrInvalidJobID
	case j.ChainNamespace == "":
		return ErrInvalidChainNamespace
	case j.ChainID == "":
		return ErrInvalidChainID
	case j.StartCursor.Value == "":
		return ErrInvalidStartCursor
	case j.BatchLimit == 0:
		return ErrInvalidBatchLimit
	case j.PollInterval < 0:
		return ErrInvalidPollInterval
	default:
		return nil
	}
}

// Adapter maps a chain's native positions and events to the normalized model.
type Adapter interface {
	Namespace() string
	ChainID(context.Context) (string, error)
	SafeHead(context.Context, ConfirmationPolicy) (Cursor, error)
	NextRange(after *Cursor, start Cursor, safe Cursor, limit uint64) (Range, error)
	Events(context.Context, Range, string, Registrations) ([]Event, error)
	CanonicalHash(context.Context, Cursor) (string, error)
}

// BlockProvider is implemented by adapters that can supply complete scanned
// block records. It is optional so future non-EVM adapters can be introduced
// incrementally without weakening cursor safety.
type BlockProvider interface {
	Blocks(context.Context, Range) ([]Block, error)
}
