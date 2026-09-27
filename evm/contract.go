// Package evm adapts EVM logs into chainindex events.
package evm

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	chainindex "github.com/jksusu/chainindex"
)

var (
	ErrInvalidAddress        = errors.New("chainindex/evm: invalid contract address")
	ErrInvalidABI            = errors.New("chainindex/evm: invalid contract ABI")
	ErrAnonymousEvent        = errors.New("chainindex/evm: anonymous events are unsupported")
	ErrDuplicateRegistration = errors.New("chainindex/evm: duplicate address and event topic registration")
)

// Contract is a validated EVM address and ABI. Every non-anonymous event in
// the ABI is registered; event dispatch is always by its topic0, never name.
type Contract struct {
	Address common.Address
	ABI     abi.ABI
	Events  []abi.Event
}

// NewContract parses an ABI JSON document and binds its events to address.
func NewContract(address, abiJSON string) (Contract, error) {
	if !common.IsHexAddress(address) {
		return Contract{}, ErrInvalidAddress
	}
	parsed, err := abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		return Contract{}, fmt.Errorf("%w: %v", ErrInvalidABI, err)
	}
	contract := Contract{Address: common.HexToAddress(address), ABI: parsed}
	for _, event := range parsed.Events {
		if event.Anonymous {
			return Contract{}, fmt.Errorf("%w: %s", ErrAnonymousEvent, event.Name)
		}
		contract.Events = append(contract.Events, event)
	}
	return contract, nil
}

// Registrations converts contracts to root-package registrations and validates
// that exactly one event owns each (address, topic0) pair.
func Registrations(contracts ...Contract) (chainindex.Registrations, error) {
	seen := make(map[string]struct{})
	registrations := make(chainindex.Registrations, 0, len(contracts))
	for _, contract := range contracts {
		for _, event := range contract.Events {
			if event.Anonymous {
				return nil, fmt.Errorf("%w: %s", ErrAnonymousEvent, event.Name)
			}
			key := strings.ToLower(contract.Address.Hex()) + "\x00" + strings.ToLower(event.ID.Hex())
			if _, exists := seen[key]; exists {
				return nil, ErrDuplicateRegistration
			}
			seen[key] = struct{}{}
		}
		registrations = append(registrations, contract)
	}
	return registrations, nil
}
