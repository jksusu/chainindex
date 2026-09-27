package evm

import (
	"context"
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	chainindex "github.com/jksusu/chainindex"
)

func TestEventsFiltersMultipleContractsAndDecodesCanonicalLog(t *testing.T) {
	first, err := NewContract("0x0000000000000000000000000000000000000001", transferABI)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewContract("0x0000000000000000000000000000000000000002", `[{"anonymous":false,"inputs":[{"indexed":false,"name":"ok","type":"bool"}],"name":"Changed","type":"event"}]`)
	if err != nil {
		t.Fatal(err)
	}
	registrations, err := Registrations(first, second)
	if err != nil {
		t.Fatal(err)
	}
	data, err := first.Events[0].Inputs.NonIndexed().Pack(big.NewInt(42))
	if err != nil {
		t.Fatal(err)
	}
	header := &types.Header{Number: big.NewInt(7), Time: 123, Extra: []byte{1}, Difficulty: big.NewInt(1), GasLimit: 1, BaseFee: big.NewInt(1)}
	blockHash := header.Hash()
	reader := &fakeReader{chainID: big.NewInt(1), logs: []types.Log{{Address: first.Address, Topics: []common.Hash{first.Events[0].ID, common.BytesToHash(common.HexToAddress("0x3").Bytes()), common.BytesToHash(common.HexToAddress("0x4").Bytes())}, Data: data, BlockNumber: 7, BlockHash: blockHash, TxHash: common.HexToHash("0xdef"), Index: 2}}, headers: map[uint64]*types.Header{7: header}}
	client, err := New(reader, "1")
	if err != nil {
		t.Fatal(err)
	}
	events, err := client.Events(context.Background(), chainindex.Range{From: chainindex.Cursor{Value: "7"}, To: chainindex.Cursor{Value: "7"}}, "transfers", registrations)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("event count = %d, want 1", len(events))
	}
	e := events[0]
	if e.JobID != "transfers" || e.Emitter != first.Address.Hex() || e.EventType != first.Events[0].ID.Hex() || e.TransactionID != common.HexToHash("0xdef").Hex() || e.EventIndex != "2" || e.Cursor.Value != "7" || e.CanonicalHash != blockHash.Hex() || !e.OccurredAt.Equal(time.Unix(123, 0).UTC()) {
		t.Fatalf("event = %+v", e)
	}
	var args map[string]any
	if err := json.Unmarshal(e.Arguments, &args); err != nil {
		t.Fatal(err)
	}
	if args["from"] != common.HexToAddress("0x3").Hex() || args["to"] != common.HexToAddress("0x4").Hex() || args["value"] != "42" {
		t.Fatalf("args = %#v", args)
	}
	var payload map[string]any
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["data"] == nil || payload["topics"] == nil {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestEventsRejectsNonCanonicalLog(t *testing.T) {
	c, err := NewContract("0x0000000000000000000000000000000000000001", transferABI)
	if err != nil {
		t.Fatal(err)
	}
	registrations, _ := Registrations(c)
	reader := &fakeReader{chainID: big.NewInt(1), logs: []types.Log{{Address: c.Address, Topics: []common.Hash{c.Events[0].ID}, BlockNumber: 2, BlockHash: common.HexToHash("0x1")}}, headers: map[uint64]*types.Header{2: {Number: big.NewInt(2)}}}
	client, _ := New(reader, "1")
	if _, err := client.Events(context.Background(), chainindex.Range{From: chainindex.Cursor{Value: "2"}, To: chainindex.Cursor{Value: "2"}}, "transfers", registrations); err == nil {
		t.Fatal("Events() error = nil, want canonical mismatch")
	}
}

type fakeReader struct {
	chainID *big.Int
	logs    []types.Log
	headers map[uint64]*types.Header
}

func (f *fakeReader) ChainID(context.Context) (*big.Int, error)   { return f.chainID, nil }
func (f *fakeReader) BlockNumber(context.Context) (uint64, error) { return 10, nil }
func (f *fakeReader) FilterLogs(context.Context, ethereum.FilterQuery) ([]types.Log, error) {
	return f.logs, nil
}
func (f *fakeReader) HeaderByNumber(_ context.Context, n *big.Int) (*types.Header, error) {
	h := f.headers[n.Uint64()]
	if h == nil {
		return nil, nil
	}
	return types.CopyHeader(h), nil
}
