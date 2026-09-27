# Chainindex Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build an installable Go module that indexes EVM contract events into caller-supplied MySQL or PostgreSQL databases, with a chain-neutral extension boundary.

**Architecture:** The root package owns jobs, orchestration and transactional persistence. The `evm` package converts go-ethereum logs into root-level normalized events and exposes per-address/per-topic registrations. Storage uses an injected `database/sql` transaction starter plus an explicit dialect; migrations are static files for users to run manually.

**Tech Stack:** Go, `database/sql`, go-ethereum ABI/RPC APIs, PostgreSQL and MySQL SQL migrations, Go standard testing.

---

## Chunk 1: Module, data model, and manual schema

### Task 1: Create the module and core public types

**Files:**
- Create: `go.mod`
- Create: `types.go`
- Create: `storage/dialect.go`
- Create: `types_test.go`

- [ ] **Step 1: Write failing tests for stable normalized event identity and configuration validation.**
- [ ] **Step 2: Run `go test ./...` and verify the tests fail because the public types do not exist.**
- [ ] **Step 3: Add minimal root types: `Event`, `Cursor`, `Job`, handler function, adapter interfaces, and validation errors.**
- [ ] **Step 4: Add explicit `storage.Dialect` values for MySQL and PostgreSQL and validation.**
- [ ] **Step 5: Run `go test ./...` and verify all tests pass.**
- [ ] **Step 6: Commit `feat: add chain-neutral public types`.**

### Task 2: Supply manual MySQL and PostgreSQL migrations

**Files:**
- Create: `migrations/mysql/001_chainindex.sql`
- Create: `migrations/postgres/001_chainindex.sql`
- Create: `migrations/migrations_test.go`

- [ ] **Step 1: Write failing tests that require cursor/event tables, cursor status/error fields, uniqueness keys, JSON columns, PostgreSQL `COLLATE "C"`, and MySQL `utf8mb4_bin`. Add a documented CI target that executes each migration against disposable PostgreSQL and MySQL instances when Docker is available.**
- [ ] **Step 2: Run `go test ./migrations` and verify the expected failure.**
- [ ] **Step 3: Write the two idempotent, manually-run SQL scripts without application-side execution support.**
- [ ] **Step 4: Run `go test ./migrations` and the disposable-database migration target when Docker is available; verify both scripts are idempotent and create the declared constraints.**
- [ ] **Step 5: Commit `feat: add manual database migrations`.**

## Chunk 2: Transactional persistence and orchestration

### Task 3: Implement dialect-aware cursor and event persistence

**Files:**
- Create: `storage/store.go`
- Create: `storage/store_test.go`

- [ ] **Step 1: Write failing tests with a database/sql test driver for PostgreSQL/MySQL SQL selection, insert-only handler dispatch signal, initial cursor creation, conditional cursor advancement, durable reorg state update, and rollback of event/business writes on failed handler, CAS, or reorg-state update.**
- [ ] **Step 2: Run `go test ./storage` and verify failures are from missing storage behavior.**
- [ ] **Step 3: Implement the minimal store using injected `BeginTx`, transaction-scoped event insert, cursor load/create/CAS update, and reorg status recording.**
- [ ] **Step 4: Run `go test ./storage` and verify all tests pass.**
- [ ] **Step 5: Commit `feat: add transactional index persistence`.**

### Task 4: Implement generic index job execution

**Files:**
- Create: `indexer.go`
- Create: `indexer_test.go`

- [ ] **Step 1: Write failing fake-adapter tests for confirmation-policy forwarding into `SafeHead`, first-run `StartCursor`, persisted cursor, empty range, canonical-hash persistence/check before scanning, handler rollback, duplicate event suppression, `Run(ctx)` polling/cancellation, reorg blocking, and `Rewind` clearing status/error and permitting a subsequent sync.**
- [ ] **Step 2: Run `go test .` and verify the tests fail.**
- [ ] **Step 3: Implement `New`, `SyncOnce`, `Run`, registration validation, canonical-hash checks, and explicit `Rewind`.**
- [ ] **Step 4: Run `go test .` and verify all root-package tests pass.**
- [ ] **Step 5: Commit `feat: add generic indexer orchestration`.**

## Chunk 3: EVM adapter and package documentation

### Task 5: Implement EVM ABI registration, scan, and normalized decoding

**Files:**
- Create: `evm/adapter.go`
- Create: `evm/contract.go`
- Create: `evm/adapter_test.go`
- Create: `evm/contract_test.go`

- [ ] **Step 1: Write failing tests for the production RPC constructor (URL dial, chain-ID validation, invalid RPC handling, and caller-closeable client), multiple contracts, different ABIs, duplicate address/topic rejection, anonymous event rejection, canonical-header verification, and normalized JSON: indexed/non-indexed addresses, decimal big integers, bytes, nested tuples/arrays, and dynamic indexed topic hashes.**
- [ ] **Step 2: Run `go test ./evm` and verify the behavior is missing.**
- [ ] **Step 3: Implement a production EVM RPC constructor plus a narrow fakeable RPC reader, ABI decoding with stable JSON values, and a closeable client when package-owned RPC dialing is used.**
- [ ] **Step 4: Run `go test ./evm` and verify all EVM tests pass.**
- [ ] **Step 5: Commit `feat: add EVM event indexing adapter`.**

### Task 6: Document consumer integration and future-chain boundary

**Files:**
- Create: `README.md`
- Create: `example_test.go`

- [ ] **Step 1: Write a failing Go example showing a caller manually runs a migration, opens its own `*sql.DB`, constructs the indexer with a dialect, registers multiple EVM contracts/ABIs, and calls `SyncOnce`.**
- [ ] **Step 2: Run `go test ./...` and verify the example does not compile before documentation-backed API completion.**
- [ ] **Step 3: Add README installation/API instructions, manual migration directions, EVM behavior, reorg limitations, and Solana/Sui adapter notes.**
- [ ] **Step 4: Run `go test ./...`, `go vet ./...`, and `go mod tidy`; verify all return success.**
- [ ] **Step 5: Commit `docs: add chainindex usage guide`.**

## Chunk 4: Final verification and remote sync

### Task 7: Review, verify, and push

**Files:**
- Modify: all changed module files as required by review findings

- [ ] **Step 1: Inspect the full diff against the design specification and request independent spec and code-quality reviews.**
- [ ] **Step 2: Resolve every critical or important finding and rerun the affected tests.**
- [ ] **Step 3: Run fresh `go test ./...`, `go vet ./...`, `go mod tidy`, and `git status --short`; verify clean output.**
- [ ] **Step 4: Add `https://github.com/jksusu/chainindex.git` as `origin` and push the implementation branch.**
- [ ] **Step 5: Report the tested commit SHA and remote branch URL.**
