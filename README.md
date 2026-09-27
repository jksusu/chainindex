# ChainIndex

ChainIndex is a Go library for durably indexing blockchain events into an
application-owned MySQL or PostgreSQL database. It keeps a separate checkpoint
for every job and persists normalized events before calling your handler in the
same transaction.

The current production adapter is for EVM-compatible chains. The core package
is chain-neutral: an adapter owns chain cursor semantics, safe-head selection,
range construction, event collection, and canonical-hash lookup.

## Install

```sh
go get github.com/jksusu/chainindex
```

Bring a driver for the database you use; ChainIndex intentionally does not
choose or open database drivers for you.

## Prepare the database

Run one migration yourself, before creating an indexer:

```sh
# PostgreSQL
psql "$DATABASE_URL" -f migrations/postgres/001_chainindex.sql

# MySQL
mysql --defaults-extra-file=... < migrations/mysql/001_chainindex.sql
```

The migrations are idempotent, but ChainIndex never runs migrations on your
behalf. Apply the file that matches your database from the version of the
library you deploy, as part of your normal migration workflow. The tables are
`chainindex_cursors` (per-job checkpoints and reorganization state) and
`chainindex_events` (deduplicated normalized events).

## Index EVM events

Open and configure a `*sql.DB` in your application, then pass it to
`chainindex.New` with the matching `storage.Dialect`. The library only requires
the transaction-starting capability of the supplied handle; it does not close
the database or manage its connection pool.

`example_test.go` contains a complete compiling example. In short:

```go
client, err := evm.Dial(ctx, rpcURL, "1") // the client owns this RPC connection
if err != nil { /* handle error */ }
defer client.Close()

token, err := evm.NewContract(tokenAddress, tokenABI)
governor, err := evm.NewContract(governorAddress, governorABI)
registrations, err := evm.Registrations(token, governor)

indexer, err := chainindex.New(db, storage.Postgres, []chainindex.Job{{
	ID: "mainnet-events", ChainNamespace: "evm", ChainID: "1",
	StartCursor: chainindex.Cursor{Value: "19000000"},
	ConfirmationPolicy: chainindex.ConfirmationPolicy{Confirmations: 12},
	BatchLimit: 500, Registrations: registrations,
}}, []chainindex.Adapter{client})
if err := indexer.SyncOnce(ctx); err != nil { /* handle error */ }
```

Each `evm.Contract` has its own address and ABI. `evm.Registrations` combines
multiple contracts, including contracts with different ABIs, and dispatches
logs by the `(address, topic0)` pair. Every non-anonymous event in a supplied
ABI is registered. Duplicate address/topic pairs and anonymous events are
rejected.

`SyncOnce` advances each configured job by at most one adapter-defined range;
call it from your scheduler for controlled runs. `Run(ctx)` repeatedly calls
`SyncOnce` using the smallest job poll interval until the context is cancelled.

## Transactional handlers and duplicates

For each newly inserted normalized event, ChainIndex invokes the job's
`Handler` with the same `*sql.Tx` used for the event and cursor. Use that
transaction for application writes. If the handler, persistence, or checkpoint
advance fails, the transaction is rolled back. A duplicate normalized event is
not dispatched to the handler again.

## Reorganizations

Before continuing from a stored cursor, ChainIndex checks its canonical hash.
If that hash has changed, the job is marked `reorg_detected`, `SyncOnce`
returns `ErrReorgDetected`, and later syncs remain blocked. Choose a canonical
cursor after investigating the reorganization, then call `Rewind` to clear the
blocked state and resume:

```go
err := indexer.Rewind(ctx, "mainnet-events", chainindex.Cursor{Value: "18999900"})
```

This is deliberately conservative. ChainIndex does not automatically delete or
compensate application records created for orphaned events, and confirmations
reduce but do not eliminate reorganization risk. Applications need a domain
specific reconciliation or compensation strategy before rewinding.

## Other chains: Solana and Sui

Solana and Sui are not built-in adapters. Implement `chainindex.Adapter` for
either chain and register it with `chainindex.New`. The adapter defines its
namespace and chain ID, turns its native checkpoint into `Cursor`, chooses a
safe head from `ConfirmationPolicy`, returns bounded `Range` values, emits
normalized `Event` records, and resolves `CanonicalHash` for a cursor. The
core indexer will then provide the same transactions, deduplication,
checkpoints, and reorganization blocking used for EVM.

Because Solana slots and Sui checkpoints have different finality and fork
semantics from EVM blocks, their adapters must make those choices explicit;
they should not reuse EVM cursor or confirmation assumptions.
