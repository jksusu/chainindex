package chainindex

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestNewRejectsMismatchedAdapter(t *testing.T) {
	_, err := New(newIndexerDB(t, nil),
		[]Job{testJob()}, []Adapter{&fakeAdapter{namespace: "solana", chainID: "1"}})
	if !errors.Is(err, ErrAdapterNotFound) {
		t.Fatalf("New() error = %v, want ErrAdapterNotFound", err)
	}
}

func TestNewSupportsAdaptersForSameNamespaceOnDifferentChains(t *testing.T) {
	first := &fakeAdapter{namespace: "evm", chainID: "1"}
	second := &fakeAdapter{namespace: "evm", chainID: "10"}
	other := testJob()
	other.ID, other.ChainID = "optimism", "10"
	if _, err := New(newIndexerDB(t, nil), []Job{testJob(), other}, []Adapter{first, second}); err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
}

func TestNewRejectsDuplicateJobIDsAcrossChains(t *testing.T) {
	other := testJob()
	other.ChainID = "10"
	if _, err := New(newIndexerDB(t, nil), []Job{testJob(), other}, []Adapter{&fakeAdapter{namespace: "evm", chainID: "1"}, &fakeAdapter{namespace: "evm", chainID: "10"}}); !errors.Is(err, ErrDuplicateJobID) {
		t.Fatalf("New() error = %v, want ErrDuplicateJobID", err)
	}
}

func TestSyncOnceForwardsConfirmationAndUsesFirstStartCursor(t *testing.T) {
	state := &indexerDBState{}
	adapter := &fakeAdapter{namespace: "evm", chainID: "1", safe: Cursor{Value: "10"}, next: Range{From: Cursor{Value: "1"}, To: Cursor{Value: "10"}}, hash: map[string]string{"10": "h10"}}
	job := testJob()
	job.StartCursor = Cursor{Value: "0"}
	job.ConfirmationPolicy = ConfirmationPolicy{Confirmations: 8}
	indexer, err := New(newIndexerDB(t, state), []Job{job}, []Adapter{adapter})
	if err != nil {
		t.Fatal(err)
	}
	if err := indexer.SyncOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if adapter.gotPolicy != job.ConfirmationPolicy {
		t.Fatalf("SafeHead policy = %+v, want %+v", adapter.gotPolicy, job.ConfirmationPolicy)
	}
	if adapter.gotAfter != nil {
		t.Fatalf("NextRange after = %+v, want nil", adapter.gotAfter)
	}
	if adapter.gotStart != job.StartCursor {
		t.Fatalf("NextRange start = %+v, want %+v", adapter.gotStart, job.StartCursor)
	}
	if !state.containsExec("chainindex_cursors") {
		t.Fatal("first sync did not create cursor")
	}
}

func TestSyncOnceChecksPersistedCursorHashBeforeScanning(t *testing.T) {
	state := &indexerDBState{rows: [][]driver.Value{{"5", "stored", "ready", nil}}}
	adapter := &fakeAdapter{namespace: "evm", chainID: "1", hash: map[string]string{"5": "different"}}
	indexer, err := New(newIndexerDB(t, state), []Job{testJob()}, []Adapter{adapter})
	if err != nil {
		t.Fatal(err)
	}
	err = indexer.SyncOnce(context.Background())
	if !errors.Is(err, ErrReorgDetected) {
		t.Fatalf("SyncOnce() error = %v, want ErrReorgDetected", err)
	}
	if adapter.nextCalls != 0 {
		t.Fatal("must not scan after reorg detection")
	}
	if !state.containsExec("chainindex_cursors") {
		t.Fatal("reorg status was not persisted")
	}
}

func TestSyncOnceSuppressesDuplicateHandlersAndAdvancesCursor(t *testing.T) {
	state := &indexerDBState{rows: [][]driver.Value{{"5", "h5", "ready", nil}}, execRows: []int64{0, 1}}
	adapter := &fakeAdapter{namespace: "evm", chainID: "1", safe: Cursor{Value: "10"}, next: Range{From: Cursor{Value: "6"}, To: Cursor{Value: "10"}}, hash: map[string]string{"5": "h5", "10": "h10"}, events: []Event{testEventForIndexer()}}
	handled := 0
	job := testJob()
	job.Handler = func(context.Context, *gorm.DB, Event) error { handled++; return nil }
	indexer, err := New(newIndexerDB(t, state), []Job{job}, []Adapter{adapter})
	if err != nil {
		t.Fatal(err)
	}
	if err := indexer.SyncOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if handled != 0 {
		t.Fatalf("handler calls = %d, want 0 for duplicate", handled)
	}
	if !state.containsExec("chainindex_cursors") {
		t.Fatal("cursor did not advance")
	}
}

func TestSyncOncePassesJobIDToAdapterEvents(t *testing.T) {
	state := &indexerDBState{}
	adapter := &fakeAdapter{namespace: "evm", chainID: "1", safe: Cursor{Value: "1"}, next: Range{From: Cursor{Value: "1"}, To: Cursor{Value: "1"}}, hash: map[string]string{"1": "h1"}}
	indexer, err := New(newIndexerDB(t, state), []Job{testJob()}, []Adapter{adapter})
	if err != nil {
		t.Fatal(err)
	}
	if err := indexer.SyncOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if adapter.gotJobID != "transfers" {
		t.Fatalf("Events job ID = %q, want transfers", adapter.gotJobID)
	}
}

func TestSyncOnceRollsBackWhenHandlerFails(t *testing.T) {
	state := &indexerDBState{execRows: []int64{1}}
	adapter := &fakeAdapter{namespace: "evm", chainID: "1", safe: Cursor{Value: "1"}, next: Range{From: Cursor{Value: "1"}, To: Cursor{Value: "1"}}, hash: map[string]string{"1": "h1"}, events: []Event{testEventForIndexer()}}
	job := testJob()
	job.Handler = func(context.Context, *gorm.DB, Event) error { return errors.New("handler failed") }
	indexer, err := New(newIndexerDB(t, state), []Job{job}, []Adapter{adapter})
	if err != nil {
		t.Fatal(err)
	}
	if err := indexer.SyncOnce(context.Background()); err == nil {
		t.Fatal("SyncOnce() error = nil, want handler error")
	}
	if state.rollbacks == 0 {
		t.Fatal("failed handler did not roll back transaction")
	}
}

func TestRunPollsUntilCancellation(t *testing.T) {
	state := &indexerDBState{}
	adapter := &fakeAdapter{namespace: "evm", chainID: "1", safe: Cursor{Value: "0"}, hash: map[string]string{"0": "h0"}}
	job := testJob()
	job.PollInterval = time.Millisecond
	indexer, err := New(newIndexerDB(t, state), []Job{job}, []Adapter{adapter})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for {
			if adapter.safeCalls >= 2 {
				cancel()
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
	if err := indexer.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
}

func TestRewindClearsReorgStatusAndAllowsSync(t *testing.T) {
	state := &indexerDBState{rows: [][]driver.Value{{"5", "old", "reorg_detected", "bad hash"}}, execRows: []int64{1}}
	adapter := &fakeAdapter{namespace: "evm", chainID: "1", safe: Cursor{Value: "5"}, hash: map[string]string{"3": "h3"}, next: Range{}}
	indexer, err := New(newIndexerDB(t, state), []Job{testJob()}, []Adapter{adapter})
	if err != nil {
		t.Fatal(err)
	}
	if err := indexer.Rewind(context.Background(), "transfers", Cursor{Value: "3"}); err != nil {
		t.Fatal(err)
	}
	if !state.containsExec("chainindex_cursors") {
		t.Fatal("rewind did not write cursor")
	}
	state.rows = [][]driver.Value{{"3", "h3", "ready", nil}}
	if err := indexer.SyncOnce(context.Background()); err != nil {
		t.Fatalf("sync after rewind: %v", err)
	}
}

func TestRewindRejectsLostCursorCompareAndSwap(t *testing.T) {
	state := &indexerDBState{rows: [][]driver.Value{{"5", "old", "reorg_detected", "bad hash"}}, execRows: []int64{0}}
	adapter := &fakeAdapter{namespace: "evm", chainID: "1", hash: map[string]string{"3": "h3"}}
	indexer, err := New(newIndexerDB(t, state), []Job{testJob()}, []Adapter{adapter})
	if err != nil {
		t.Fatal(err)
	}
	if err := indexer.Rewind(context.Background(), "transfers", Cursor{Value: "3"}); !errors.Is(err, ErrCursorChanged) {
		t.Fatalf("Rewind() error = %v, want ErrCursorChanged", err)
	}
}

func testJob() Job {
	return Job{ID: "transfers", ChainNamespace: "evm", ChainID: "1", StartCursor: Cursor{Value: "0"}, BatchLimit: 100}
}
func testEventForIndexer() Event {
	return Event{ChainNamespace: "evm", ChainID: "1", JobID: "transfers", TransactionID: "tx", EventIndex: "0", Cursor: Cursor{Value: "10"}, CanonicalHash: "h10", OccurredAt: time.Now(), Payload: []byte(`{}`), Arguments: []byte(`{}`)}
}

type fakeAdapter struct {
	namespace, chainID   string
	safe                 Cursor
	next                 Range
	events               []Event
	hash                 map[string]string
	gotPolicy            ConfirmationPolicy
	gotAfter             *Cursor
	gotStart             Cursor
	nextCalls, safeCalls int
	gotJobID             string
}

func (a *fakeAdapter) Namespace() string                       { return a.namespace }
func (a *fakeAdapter) ChainID(context.Context) (string, error) { return a.chainID, nil }
func (a *fakeAdapter) SafeHead(_ context.Context, p ConfirmationPolicy) (Cursor, error) {
	a.safeCalls++
	a.gotPolicy = p
	return a.safe, nil
}
func (a *fakeAdapter) NextRange(after *Cursor, start Cursor, _ Cursor, _ uint64) (Range, error) {
	a.nextCalls++
	if after != nil {
		c := *after
		a.gotAfter = &c
	}
	a.gotStart = start
	return a.next, nil
}
func (a *fakeAdapter) Events(_ context.Context, _ Range, jobID string, _ Registrations) ([]Event, error) {
	a.gotJobID = jobID
	return a.events, nil
}
func (a *fakeAdapter) CanonicalHash(_ context.Context, c Cursor) (string, error) {
	return a.hash[c.Value], nil
}

type indexerDBState struct {
	mu        sync.Mutex
	rows      [][]driver.Value
	execRows  []int64
	queries   []string
	rollbacks int
}

func (s *indexerDBState) containsExec(part string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, q := range s.queries {
		if strings.Contains(q, part) {
			return true
		}
	}
	return false
}
func newIndexerDB(t *testing.T, state *indexerDBState) *gorm.DB {
	t.Helper()
	if state == nil {
		state = &indexerDBState{}
	}
	name := "chainindex-indexer-" + strings.ReplaceAll(t.Name(), "/", "-")
	sql.Register(name, indexerDriver{state})
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	gormDB, err := gorm.Open(mysql.New(mysql.Config{Conn: db, SkipInitializeWithVersion: true}), &gorm.Config{})
	if err != nil { t.Fatal(err) }
	return gormDB
}

type indexerDriver struct{ state *indexerDBState }

func (d indexerDriver) Open(string) (driver.Conn, error) { return &indexerConn{d.state}, nil }

type indexerConn struct{ state *indexerDBState }

func (*indexerConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unsupported") }
func (*indexerConn) Close() error                        { return nil }
func (c *indexerConn) Begin() (driver.Tx, error)         { return indexerTx{c.state}, nil }
func (c *indexerConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return indexerTx{c.state}, nil
}
func (c *indexerConn) ExecContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	c.state.queries = append(c.state.queries, q)
	n := int64(1)
	if len(c.state.execRows) > 0 {
		n = c.state.execRows[0]
		c.state.execRows = c.state.execRows[1:]
	}
	return driver.RowsAffected(n), nil
}
func (c *indexerConn) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	c.state.queries = append(c.state.queries, q)
	return &indexerRows{rows: c.state.rows}, nil
}

type indexerTx struct{ state *indexerDBState }

func (t indexerTx) Commit() error { return nil }
func (t indexerTx) Rollback() error {
	t.state.mu.Lock()
	defer t.state.mu.Unlock()
	t.state.rollbacks++
	return nil
}

type indexerRows struct {
	rows [][]driver.Value
	pos  int
}

func (*indexerRows) Columns() []string {
	return []string{"cursor", "canonical_hash", "status", "last_error"}
}
func (*indexerRows) Close() error { return nil }
func (r *indexerRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.pos])
	r.pos++
	return nil
}
