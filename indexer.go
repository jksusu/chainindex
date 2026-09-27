package chainindex

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jksusu/chainindex/storage"
	"gorm.io/gorm"
)

var (
	ErrAdapterNotFound  = errors.New("chainindex: no matching adapter for job")
	ErrDuplicateAdapter = errors.New("chainindex: duplicate adapter for namespace and chain")
	ErrDuplicateJobID   = errors.New("chainindex: duplicate job ID")
	ErrReorgDetected    = errors.New("chainindex: chain reorganization detected")
	ErrInvalidEvent     = errors.New("chainindex: adapter returned event for another job or chain")
	ErrCursorChanged    = errors.New("chainindex: cursor changed concurrently")
)

// Indexer synchronizes one or more independently checkpointed jobs.
type Indexer struct {
	store    *storage.Store
	jobs     []Job
	adapters map[string]Adapter
}

// New constructs an Indexer using the caller-owned database capability.
func New(db *gorm.DB, jobs []Job, adapters []Adapter) (*Indexer, error) {
	store, err := storage.New(db)
	if err != nil {
		return nil, err
	}
	byChain := make(map[string]Adapter, len(adapters))
	for _, adapter := range adapters {
		if adapter == nil || adapter.Namespace() == "" {
			continue
		}
		chainID, err := adapter.ChainID(context.Background())
		if err != nil {
			return nil, err
		}
		key := adapterKey(adapter.Namespace(), chainID)
		if _, duplicate := byChain[key]; duplicate {
			return nil, fmt.Errorf("%w: %s/%s", ErrDuplicateAdapter, adapter.Namespace(), chainID)
		}
		byChain[key] = adapter
	}
	seen := make(map[string]struct{}, len(jobs))
	for _, job := range jobs {
		if err := job.Validate(); err != nil {
			return nil, err
		}
		if _, duplicate := seen[job.ID]; duplicate {
			return nil, ErrDuplicateJobID
		}
		seen[job.ID] = struct{}{}
		adapter := byChain[adapterKey(job.ChainNamespace, job.ChainID)]
		if adapter == nil {
			return nil, fmt.Errorf("%w: %s/%s", ErrAdapterNotFound, job.ChainNamespace, job.ChainID)
		}
	}
	return &Indexer{store: store, jobs: append([]Job(nil), jobs...), adapters: byChain}, nil
}

func adapterKey(namespace, chainID string) string { return namespace + "\x00" + chainID }

// SyncOnce advances every configured job by at most one adapter-defined range.
func (i *Indexer) SyncOnce(ctx context.Context) error {
	for _, job := range i.jobs {
		if err := i.syncJob(ctx, job, i.adapters[adapterKey(job.ChainNamespace, job.ChainID)]); err != nil {
			return err
		}
	}
	return nil
}

func (i *Indexer) syncJob(ctx context.Context, job Job, adapter Adapter) (err error) {
	tx, err := i.store.Begin(ctx)
	if err != nil { return err }
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	key := storage.CursorKey{ChainNamespace: job.ChainNamespace, ChainID: job.ChainID, JobID: job.ID}
	state, found, err := i.store.LoadCursor(ctx, tx, key)
	if err != nil {
		return err
	}
	if found && state.Status == storage.StatusReorgDetected {
		return ErrReorgDetected
	}
	var after *Cursor
	if found {
		after = &Cursor{Value: state.Cursor}
		actual, err := adapter.CanonicalHash(ctx, *after)
		if err != nil {
			return err
		}
		if actual != state.CanonicalHash {
			message := fmt.Sprintf("canonical hash changed at cursor %s", state.Cursor)
			if err := i.store.RecordReorg(ctx, tx, key, message); err != nil {
				return err
			}
			if err := tx.Commit().Error; err != nil { return err }
			committed = true
			return fmt.Errorf("%w: %s", ErrReorgDetected, message)
		}
	}

	safe, err := adapter.SafeHead(ctx, job.ConfirmationPolicy)
	if err != nil {
		return err
	}
	rangeToScan, err := adapter.NextRange(after, job.StartCursor, safe, job.BatchLimit)
	if err != nil {
		return err
	}
	if rangeToScan.From.Value == "" && rangeToScan.To.Value == "" {
		if err := tx.Commit().Error; err != nil { return err }
		committed = true
		return nil
	}

	events, err := adapter.Events(ctx, rangeToScan, job.ID, job.Registrations)
	if err != nil {
		return err
	}
	canonicalHash, err := adapter.CanonicalHash(ctx, rangeToScan.To)
	if err != nil {
		return err
	}
	if provider, ok := adapter.(BlockProvider); ok {
		blocks, err := provider.Blocks(ctx, rangeToScan)
		if err != nil { return err }
		for _, block := range blocks {
			if block.ChainNamespace != job.ChainNamespace || block.ChainID != job.ChainID { return ErrInvalidEvent }
			if err := i.store.InsertBlock(ctx, tx, storage.Block{ChainNamespace: block.ChainNamespace, ChainID: block.ChainID, BlockNumber: block.Number, BlockHash: block.Hash, ParentHash: block.ParentHash, Timestamp: block.Timestamp, Miner: block.Miner, GasLimit: block.GasLimit, GasUsed: block.GasUsed, BaseFeePerGas: block.BaseFeePerGas, TransactionsRoot: block.TransactionsRoot, StateRoot: block.StateRoot, ReceiptsRoot: block.ReceiptsRoot, LogsBloom: block.LogsBloom, RawBlock: block.Raw}); err != nil { return err }
		}
	}
	for _, event := range events {
		if event.ChainNamespace != job.ChainNamespace || event.ChainID != job.ChainID || event.JobID != job.ID {
			return ErrInvalidEvent
		}
		inserted, err := i.store.InsertEvent(ctx, tx, storage.Event{
			ChainNamespace: event.ChainNamespace, ChainID: event.ChainID, JobID: event.JobID,
			BlockNumber: first(event.BlockNumber, event.Cursor.Value), BlockHash: first(event.BlockHash, event.CanonicalHash), TransactionHash: first(event.TransactionHash, event.TransactionID), TransactionIndex: event.TransactionIndex, LogIndex: first(event.LogIndex, event.EventIndex), Address: first(event.Address, event.Emitter), Topic0: first(event.Topic0, event.EventType), Topics: event.Topics, Data: event.Data, Removed: event.Removed, EventName: event.EventName, DecodedArgs: firstJSON(event.DecodedArgs, event.Arguments), RawLog: firstJSON(event.RawLog, event.Payload), OccurredAt: event.OccurredAt,
		})
		if err != nil {
			return err
		}
		if inserted && job.Handler != nil {
			if err := job.Handler(ctx, tx, event); err != nil {
				return err
			}
		}
	}
	next := storage.CursorState{Cursor: rangeToScan.To.Value, CanonicalHash: canonicalHash, Status: storage.StatusReady}
	if !found {
		if err := i.store.CreateCursor(ctx, tx, key, next); err != nil {
			return err
		}
	} else {
		advanced, err := i.store.AdvanceCursor(ctx, tx, key, state.Cursor, next)
		if err != nil {
			return err
		}
		if !advanced {
			return ErrCursorChanged
		}
	}
	if err := tx.Commit().Error; err != nil { return err }
	committed = true
	return nil
}

func first(value, fallback string) string { if value != "" { return value }; return fallback }
func firstJSON(value, fallback []byte) []byte { if len(value) != 0 { return value }; return fallback }

// Run repeatedly synchronizes jobs until ctx is cancelled.
func (i *Indexer) Run(ctx context.Context) error {
	for {
		if err := i.SyncOnce(ctx); err != nil {
			return err
		}
		interval := i.minimumPollInterval()
		if interval == 0 {
			interval = time.Millisecond
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (i *Indexer) minimumPollInterval() time.Duration {
	if len(i.jobs) == 0 {
		return 0
	}
	interval := i.jobs[0].PollInterval
	for _, job := range i.jobs[1:] {
		if job.PollInterval < interval {
			interval = job.PollInterval
		}
	}
	return interval
}

// Rewind replaces a job's checkpoint with a caller-selected canonical cursor.
func (i *Indexer) Rewind(ctx context.Context, jobID string, cursor Cursor) (err error) {
	if cursor.Value == "" {
		return ErrInvalidStartCursor
	}
	for _, job := range i.jobs {
		if job.ID != jobID {
			continue
		}
		adapter := i.adapters[adapterKey(job.ChainNamespace, job.ChainID)]
		hash, err := adapter.CanonicalHash(ctx, cursor)
		if err != nil {
			return err
		}
		tx, err := i.store.Begin(ctx)
		if err != nil { return err }
		committed := false
		defer func() {
			if !committed {
				_ = tx.Rollback()
			}
		}()
		key := storage.CursorKey{ChainNamespace: job.ChainNamespace, ChainID: job.ChainID, JobID: job.ID}
		state, found, err := i.store.LoadCursor(ctx, tx, key)
		if err != nil {
			return err
		}
		next := storage.CursorState{Cursor: cursor.Value, CanonicalHash: hash, Status: storage.StatusReady}
		if !found {
			err = i.store.CreateCursor(ctx, tx, key, next)
		} else {
			advanced, advanceErr := i.store.AdvanceCursor(ctx, tx, key, state.Cursor, next)
			if advanceErr != nil {
				err = advanceErr
			} else if !advanced {
				err = ErrCursorChanged
			}
		}
		if err != nil {
			return err
		}
		if err := tx.Commit().Error; err != nil { return err }
		committed = true
		return nil
	}
	return fmt.Errorf("%w: job %q", ErrAdapterNotFound, jobID)
}
