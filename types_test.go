package chainindex

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jksusu/chainindex/storage"
	"gorm.io/gorm"
)

func TestEventIdentityIsStableAndChainNeutral(t *testing.T) {
	occurredAt := time.Date(2026, time.September, 27, 1, 2, 3, 0, time.UTC)
	event := Event{
		ChainNamespace: "evm",
		ChainID:        "1",
		JobID:          "transfers",
		Emitter:        "0xabc",
		EventType:      "0xdef",
		TransactionID:  "0x123",
		EventIndex:     "7",
		Cursor:         Cursor{Value: "42"},
		CanonicalHash:  "0x456",
		OccurredAt:     occurredAt,
		Payload:        json.RawMessage(`{"topics":[],"data":"0x"}`),
		Arguments:      json.RawMessage(`{"from":"0xabc"}`),
	}

	if got, want := event.Identity(), (EventIdentity{
		ChainNamespace: "evm",
		ChainID:        "1",
		JobID:          "transfers",
		TransactionID:  "0x123",
		EventIndex:     "7",
	}); got != want {
		t.Fatalf("Identity() = %#v, want %#v", got, want)
	}
}

func TestJobValidateRejectsMissingIdentityAndInvalidConfiguration(t *testing.T) {
	valid := Job{
		ID:             "transfers",
		ChainNamespace: "evm",
		ChainID:        "1",
		StartCursor:    Cursor{Value: "0"},
		BatchLimit:     100,
		Handler:        func(context.Context, *gorm.DB, Event) error { return nil },
	}

	for _, tc := range []struct {
		name string
		job  Job
		want error
	}{
		{name: "missing job ID", job: Job{ChainNamespace: "evm", ChainID: "1"}, want: ErrInvalidJobID},
		{name: "missing namespace", job: Job{ID: "transfers", ChainID: "1"}, want: ErrInvalidChainNamespace},
		{name: "missing chain ID", job: Job{ID: "transfers", ChainNamespace: "evm"}, want: ErrInvalidChainID},
		{name: "empty start cursor", job: Job{ID: "transfers", ChainNamespace: "evm", ChainID: "1", BatchLimit: 100}, want: ErrInvalidStartCursor},
		{name: "zero batch limit", job: Job{ID: "transfers", ChainNamespace: "evm", ChainID: "1", StartCursor: Cursor{Value: "0"}}, want: ErrInvalidBatchLimit},
		{name: "negative polling interval", job: func() Job { j := valid; j.PollInterval = -time.Second; return j }(), want: ErrInvalidPollInterval},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.job.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("Validate() error = %v, want errors.Is(_, %v)", err, tc.want)
			}
		})
	}

	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestJobValidateAllowsNilHandlerAndZeroPollInterval(t *testing.T) {
	job := Job{
		ID:             "transfers",
		ChainNamespace: "evm",
		ChainID:        "1",
		StartCursor:    Cursor{Value: "0"},
		BatchLimit:     100,
		// A nil handler deliberately means that only the normalized event is stored.
		Handler: nil,
		// A zero interval is valid for callers that use SyncOnce rather than Run.
		PollInterval: 0,
	}

	if err := job.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestAdapterContractAndDialectValidation(t *testing.T) {
	var _ Adapter = adapterStub{}

	if err := storage.Postgres.Validate(); err != nil {
		t.Fatalf("Postgres.Validate() error = %v, want nil", err)
	}
	if err := storage.MySQL.Validate(); err != nil {
		t.Fatalf("MySQL.Validate() error = %v, want nil", err)
	}
	if err := storage.Dialect("sqlite").Validate(); !errors.Is(err, storage.ErrInvalidDialect) {
		t.Fatalf("invalid dialect error = %v, want errors.Is(_, ErrInvalidDialect)", err)
	}
}

type adapterStub struct{}

func (adapterStub) Namespace() string                       { return "test" }
func (adapterStub) ChainID(context.Context) (string, error) { return "chain", nil }
func (adapterStub) SafeHead(context.Context, ConfirmationPolicy) (Cursor, error) {
	return Cursor{}, nil
}
func (adapterStub) NextRange(*Cursor, Cursor, Cursor, uint64) (Range, error) { return Range{}, nil }
func (adapterStub) Events(context.Context, Range, string, Registrations) ([]Event, error) {
	return nil, nil
}
func (adapterStub) CanonicalHash(context.Context, Cursor) (string, error) { return "", nil }
