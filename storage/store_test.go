package storage

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
)

func TestInsertEventUsesDialectSpecificDuplicateSuppression(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		dialect    Dialect
	}{
		{"postgres", "ON CONFLICT (chain_namespace, chain_id, job_id, transaction_id, event_index) DO NOTHING", Postgres},
		{"mysql", "ON DUPLICATE KEY UPDATE transaction_id = transaction_id", MySQL},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeDB{execResult: driver.RowsAffected(1)}
			store, err := New(openFakeDB(t, fake), tc.dialect)
			if err != nil {
				t.Fatal(err)
			}
			tx, err := store.Begin(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			inserted, err := store.InsertEvent(context.Background(), tx, testEvent())
			if err != nil {
				t.Fatal(err)
			}
			if !inserted {
				t.Fatal("expected inserted event")
			}
			if !strings.Contains(fake.lastExec(), tc.want) {
				t.Fatalf("query %q does not contain %q", fake.lastExec(), tc.want)
			}
		})
	}
}

func TestInsertEventSignalsDuplicate(t *testing.T) {
	fake := &fakeDB{execResult: driver.RowsAffected(0)}
	store, err := New(openFakeDB(t, fake), Postgres)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := store.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	inserted, err := store.InsertEvent(context.Background(), tx, testEvent())
	if err != nil {
		t.Fatal(err)
	}
	if inserted {
		t.Fatal("duplicate event must not signal handler dispatch")
	}
}

func TestCreateAndAdvanceCursorCompareAndSwap(t *testing.T) {
	fake := &fakeDB{execResult: driver.RowsAffected(1)}
	store, err := New(openFakeDB(t, fake), Postgres)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := store.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	key := CursorKey{ChainNamespace: "evm", ChainID: "1", JobID: "transfers"}
	if err := store.CreateCursor(context.Background(), tx, key, CursorState{Cursor: "10", CanonicalHash: "0x10", Status: StatusReady}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fake.lastExec(), "INSERT INTO chainindex_cursors") {
		t.Fatalf("unexpected query: %s", fake.lastExec())
	}
	advanced, err := store.AdvanceCursor(context.Background(), tx, key, "10", CursorState{Cursor: "20", CanonicalHash: "0x20", Status: StatusReady})
	if err != nil {
		t.Fatal(err)
	}
	if !advanced {
		t.Fatal("expected successful CAS")
	}
	if !strings.Contains(fake.lastExec(), "WHERE chain_namespace = $6") || !strings.Contains(fake.lastExec(), "AND cursor = $9") {
		t.Fatalf("CAS query missing key/expected cursor: %s", fake.lastExec())
	}
}

func TestAdvanceCursorReportsLostCompareAndSwap(t *testing.T) {
	fake := &fakeDB{execResult: driver.RowsAffected(0)}
	store, err := New(openFakeDB(t, fake), MySQL)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := store.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	advanced, err := store.AdvanceCursor(context.Background(), tx, CursorKey{"evm", "1", "transfers"}, "10", CursorState{Cursor: "20", Status: StatusReady})
	if err != nil {
		t.Fatal(err)
	}
	if advanced {
		t.Fatal("lost CAS must return false")
	}
}

func TestLoadCursor(t *testing.T) {
	fake := &fakeDB{queryRows: [][]driver.Value{{"12", "0x12", StatusReady, nil}}}
	store, err := New(openFakeDB(t, fake), MySQL)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := store.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	state, found, err := store.LoadCursor(context.Background(), tx, CursorKey{"evm", "1", "transfers"})
	if err != nil {
		t.Fatal(err)
	}
	if !found || state.Cursor != "12" || state.CanonicalHash != "0x12" || state.Status != StatusReady {
		t.Fatalf("unexpected cursor: %+v found=%v", state, found)
	}
}

func TestRecordReorgPersistsStatusAndError(t *testing.T) {
	fake := &fakeDB{execResult: driver.RowsAffected(1)}
	store, err := New(openFakeDB(t, fake), Postgres)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := store.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	err = store.RecordReorg(context.Background(), tx, CursorKey{"evm", "1", "transfers"}, "reorg at 12")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fake.lastExec(), "status = $1, last_error = $2") {
		t.Fatalf("unexpected reorg query: %s", fake.lastExec())
	}
	if got := fake.lastArgs()[0].Value; got != StatusReorgDetected {
		t.Fatalf("reorg status was not bound: %v", got)
	}
}

func testEvent() Event {
	return Event{ChainNamespace: "evm", ChainID: "1", JobID: "transfers", TransactionID: "0xtx", EventIndex: "0", Emitter: "0xcontract", EventType: "0xtopic", Cursor: "12", CanonicalHash: "0x12", OccurredAt: time.Unix(1, 0).UTC(), Payload: []byte(`{"data":"0x"}`), Arguments: []byte(`{"value":"1"}`)}
}

func openFakeDB(t *testing.T, state *fakeDB) *sql.DB {
	t.Helper()
	name := "chainindex-storage-" + strings.ReplaceAll(t.Name(), "/", "-")
	sql.Register(name, fakeDriver{state: state})
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

type fakeDriver struct{ state *fakeDB }

func (d fakeDriver) Open(string) (driver.Conn, error) { return &fakeConn{state: d.state}, nil }

type fakeDB struct {
	mu         sync.Mutex
	queries    []string
	args       [][]driver.NamedValue
	execResult driver.Result
	queryRows  [][]driver.Value
	execErr    error
}

func (f *fakeDB) lastExec() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.queries[len(f.queries)-1]
}
func (f *fakeDB) lastArgs() []driver.NamedValue {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.args[len(f.args)-1]
}

type fakeConn struct{ state *fakeDB }

func (c *fakeConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepared statements unsupported")
}
func (c *fakeConn) Close() error              { return nil }
func (c *fakeConn) Begin() (driver.Tx, error) { return fakeTx{}, nil }
func (c *fakeConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return fakeTx{}, nil
}
func (c *fakeConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	c.state.queries = append(c.state.queries, query)
	c.state.args = append(c.state.args, args)
	if c.state.execErr != nil {
		return nil, c.state.execErr
	}
	return c.state.execResult, nil
}
func (c *fakeConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	c.state.queries = append(c.state.queries, query)
	return &fakeRows{rows: c.state.queryRows}, nil
}

type fakeTx struct{}

func (fakeTx) Commit() error   { return nil }
func (fakeTx) Rollback() error { return nil }

type fakeRows struct {
	rows [][]driver.Value
	pos  int
}

func (r *fakeRows) Columns() []string {
	return []string{"cursor", "canonical_hash", "status", "last_error"}
}
func (r *fakeRows) Close() error { return nil }
func (r *fakeRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.pos])
	r.pos++
	return nil
}
