package evm

import (
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

const transferABI = `[{"anonymous":false,"inputs":[{"indexed":true,"name":"from","type":"address"},{"indexed":true,"name":"to","type":"address"},{"indexed":false,"name":"value","type":"uint256"}],"name":"Transfer","type":"event"}]`

func TestNewContractRejectsAnonymousEvent(t *testing.T) {
	_, err := NewContract("0x0000000000000000000000000000000000000001", `[{"anonymous":true,"inputs":[],"name":"Hidden","type":"event"}]`)
	if !errors.Is(err, ErrAnonymousEvent) {
		t.Fatalf("NewContract() error = %v, want ErrAnonymousEvent", err)
	}
}

func TestRegistrationsRejectDuplicateAddressAndTopic(t *testing.T) {
	c, err := NewContract("0x0000000000000000000000000000000000000001", transferABI)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Registrations(c, c)
	if !errors.Is(err, ErrDuplicateRegistration) {
		t.Fatalf("Registrations() error = %v, want ErrDuplicateRegistration", err)
	}
}

func TestNewContractExposesAddressAndTopic(t *testing.T) {
	c, err := NewContract("0x0000000000000000000000000000000000000001", transferABI)
	if err != nil {
		t.Fatal(err)
	}
	if c.Address != common.HexToAddress("0x1") || len(c.Events) != 1 || c.Events[0].ID == (common.Hash{}) {
		t.Fatalf("contract = %+v, want parsed address and event topic", c)
	}
}
