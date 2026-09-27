package evm

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	chainindex "github.com/jksusu/chainindex"
)

var (
	ErrInvalidRPCURL           = errors.New("chainindex/evm: invalid RPC URL")
	ErrInvalidChainID          = errors.New("chainindex/evm: invalid chain ID")
	ErrChainIDMismatch         = errors.New("chainindex/evm: chain ID mismatch")
	ErrInvalidCursor           = errors.New("chainindex/evm: invalid block cursor")
	ErrMissingHeader           = errors.New("chainindex/evm: block header not found")
	ErrNonCanonicalLog         = errors.New("chainindex/evm: log does not match canonical header")
	ErrUnsupportedRegistration = errors.New("chainindex/evm: unsupported registration")
)

// Reader is the narrow RPC surface used by Client. It makes scanner tests
// independent from a live JSON-RPC endpoint.
type Reader interface {
	ChainID(context.Context) (*big.Int, error)
	BlockNumber(context.Context) (uint64, error)
	FilterLogs(context.Context, ethereum.FilterQuery) ([]types.Log, error)
	HeaderByNumber(context.Context, *big.Int) (*types.Header, error)
}

// Client is a chainindex adapter for EVM-compatible chains.
type Client struct {
	reader  Reader
	chainID string
	close   func()
}

// New constructs a client around a caller-owned reader and checks its chain ID.
// Client.Close never closes a reader supplied through this constructor.
func New(reader Reader, expectedChainID string) (*Client, error) {
	return newClient(context.Background(), reader, expectedChainID, nil)
}

// Dial dials an RPC endpoint. The returned client owns and closes the RPC
// connection when Close is called.
func Dial(ctx context.Context, rpcURL, expectedChainID string) (*Client, error) {
	if strings.TrimSpace(rpcURL) == "" {
		return nil, ErrInvalidRPCURL
	}
	reader, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return nil, err
	}
	client, err := newClient(ctx, reader, expectedChainID, reader.Close)
	if err != nil {
		reader.Close()
		return nil, err
	}
	return client, nil
}

// NewClient is an alias for Dial retained as a descriptive production constructor.
func NewClient(ctx context.Context, rpcURL, expectedChainID string) (*Client, error) {
	return Dial(ctx, rpcURL, expectedChainID)
}

func newClient(ctx context.Context, reader Reader, expectedChainID string, close func()) (*Client, error) {
	if reader == nil {
		return nil, ErrInvalidRPCURL
	}
	expected, ok := new(big.Int).SetString(expectedChainID, 10)
	if !ok || expected.Sign() <= 0 {
		return nil, ErrInvalidChainID
	}
	actual, err := reader.ChainID(ctx)
	if err != nil {
		return nil, err
	}
	if actual == nil || actual.Sign() <= 0 {
		return nil, ErrInvalidChainID
	}
	if actual.Cmp(expected) != 0 {
		return nil, fmt.Errorf("%w: got %s, want %s", ErrChainIDMismatch, actual, expected)
	}
	return &Client{reader: reader, chainID: actual.String(), close: close}, nil
}

func (*Client) Namespace() string                         { return "evm" }
func (c *Client) ChainID(context.Context) (string, error) { return c.chainID, nil }

// Close releases an RPC connection only when Client created it with Dial.
func (c *Client) Close() error {
	if c.close == nil {
		return nil
	}
	c.close()
	c.close = nil
	return nil
}

func (c *Client) SafeHead(ctx context.Context, policy chainindex.ConfirmationPolicy) (chainindex.Cursor, error) {
	head, err := c.reader.BlockNumber(ctx)
	if err != nil {
		return chainindex.Cursor{}, err
	}
	if policy.Confirmations > head {
		return chainindex.Cursor{Value: "0"}, nil
	}
	return chainindex.Cursor{Value: strconv.FormatUint(head-policy.Confirmations, 10)}, nil
}

func (c *Client) NextRange(after *chainindex.Cursor, start, safe chainindex.Cursor, limit uint64) (chainindex.Range, error) {
	safeHeight, err := parseCursor(safe)
	if err != nil {
		return chainindex.Range{}, err
	}
	from := uint64(0)
	if after == nil {
		from, err = parseCursor(start)
	} else {
		from, err = parseCursor(*after)
		if err == nil {
			if from == math.MaxUint64 {
				return chainindex.Range{}, ErrInvalidCursor
			}
			from++
		}
	}
	if err != nil {
		return chainindex.Range{}, err
	}
	if from > safeHeight {
		return chainindex.Range{}, nil
	}
	if limit == 0 {
		return chainindex.Range{}, ErrInvalidCursor
	}
	to := safeHeight
	if limit-1 < to-from {
		to = from + limit - 1
	}
	return chainindex.Range{From: chainindex.Cursor{Value: strconv.FormatUint(from, 10)}, To: chainindex.Cursor{Value: strconv.FormatUint(to, 10)}}, nil
}

func (c *Client) CanonicalHash(ctx context.Context, cursor chainindex.Cursor) (string, error) {
	height, err := parseCursor(cursor)
	if err != nil {
		return "", err
	}
	header, err := c.reader.HeaderByNumber(ctx, new(big.Int).SetUint64(height))
	if err != nil {
		return "", err
	}
	if header == nil {
		return "", ErrMissingHeader
	}
	return lowerHex(header.Hash().Bytes()), nil
}

// Blocks returns every canonical header in a scanned range. The raw header is
// retained as JSON so new EVM header fields are not lost by this package.
func (c *Client) Blocks(ctx context.Context, blockRange chainindex.Range) ([]chainindex.Block, error) {
	from, err := parseCursor(blockRange.From)
	if err != nil { return nil, err }
	to, err := parseCursor(blockRange.To)
	if err != nil || from > to { return nil, ErrInvalidCursor }
	blocks := make([]chainindex.Block, 0, to-from+1)
	for height := from; height <= to; height++ {
		header, err := c.reader.HeaderByNumber(ctx, new(big.Int).SetUint64(height))
		if err != nil { return nil, err }
		if header == nil { return nil, ErrMissingHeader }
		raw, err := json.Marshal(header)
		if err != nil { return nil, err }
		baseFee := ""
		if header.BaseFee != nil { baseFee = header.BaseFee.String() }
		blocks = append(blocks, chainindex.Block{
			ChainNamespace: c.Namespace(), ChainID: c.chainID,
			Number: strconv.FormatUint(height, 10), Hash: lowerHash(header.Hash()),
			ParentHash: lowerHash(header.ParentHash), Timestamp: time.Unix(int64(header.Time), 0).UTC(),
			Miner: lowerAddress(header.Coinbase), GasLimit: strconv.FormatUint(header.GasLimit, 10),
			GasUsed: strconv.FormatUint(header.GasUsed, 10), BaseFeePerGas: baseFee,
			TransactionsRoot: lowerHash(header.TxHash), StateRoot: lowerHash(header.Root),
			ReceiptsRoot: lowerHash(header.ReceiptHash), LogsBloom: lowerHex(header.Bloom[:]), Raw: raw,
		})
		if height == math.MaxUint64 { break }
	}
	return blocks, nil
}

func (c *Client) Events(ctx context.Context, blockRange chainindex.Range, jobID string, registrations chainindex.Registrations) ([]chainindex.Event, error) {
	from, err := parseCursor(blockRange.From)
	if err != nil {
		return nil, err
	}
	to, err := parseCursor(blockRange.To)
	if err != nil {
		return nil, err
	}
	if from > to {
		return nil, ErrInvalidCursor
	}
	dispatch, addresses, topics, err := eventDispatch(registrations)
	if err != nil {
		return nil, err
	}
	logs, err := c.reader.FilterLogs(ctx, ethereum.FilterQuery{FromBlock: new(big.Int).SetUint64(from), ToBlock: new(big.Int).SetUint64(to), Addresses: addresses, Topics: [][]common.Hash{topics}})
	if err != nil {
		return nil, err
	}
	events := make([]chainindex.Event, 0, len(logs))
	for _, log := range logs {
		if len(log.Topics) == 0 {
			continue
		}
		registered, ok := dispatch[dispatchKey(log.Address, log.Topics[0])]
		if !ok {
			continue
		}
		header, err := c.reader.HeaderByNumber(ctx, new(big.Int).SetUint64(log.BlockNumber))
		if err != nil {
			return nil, err
		}
		if header == nil {
			return nil, ErrMissingHeader
		}
		if header.Hash() != log.BlockHash {
			return nil, fmt.Errorf("%w: block %d", ErrNonCanonicalLog, log.BlockNumber)
		}
		arguments, err := decodeArguments(registered, log)
		if err != nil {
			return nil, err
		}
		payload, err := json.Marshal(map[string]any{"topics": hashesJSON(log.Topics), "data": lowerHex(log.Data)})
		if err != nil {
			return nil, err
		}
		events = append(events, chainindex.Event{ChainNamespace: c.Namespace(), ChainID: c.chainID, JobID: jobID, Emitter: lowerAddress(log.Address), EventType: lowerHash(log.Topics[0]), TransactionID: lowerHash(log.TxHash), EventIndex: strconv.FormatUint(uint64(log.Index), 10), Cursor: chainindex.Cursor{Value: strconv.FormatUint(log.BlockNumber, 10)}, CanonicalHash: lowerHash(log.BlockHash), OccurredAt: time.Unix(int64(header.Time), 0).UTC(), Payload: payload, Arguments: arguments})
	}
	return events, nil
}

type registeredEvent struct{ event abi.Event }

func eventDispatch(registrations chainindex.Registrations) (map[string]registeredEvent, []common.Address, []common.Hash, error) {
	dispatch := make(map[string]registeredEvent)
	addresses := make([]common.Address, 0, len(registrations))
	topics := make([]common.Hash, 0)
	addressSeen := make(map[common.Address]struct{})
	for _, registration := range registrations {
		contract, ok := registration.(Contract)
		if !ok {
			if p, pointer := registration.(*Contract); pointer && p != nil {
				contract = *p
				ok = true
			}
		}
		if !ok {
			return nil, nil, nil, ErrUnsupportedRegistration
		}
		if _, seen := addressSeen[contract.Address]; !seen {
			addresses = append(addresses, contract.Address)
			addressSeen[contract.Address] = struct{}{}
		}
		for _, event := range contract.Events {
			if event.Anonymous {
				return nil, nil, nil, ErrAnonymousEvent
			}
			key := dispatchKey(contract.Address, event.ID)
			if _, exists := dispatch[key]; exists {
				return nil, nil, nil, ErrDuplicateRegistration
			}
			dispatch[key] = registeredEvent{event: event}
			topics = append(topics, event.ID)
		}
	}
	return dispatch, addresses, topics, nil
}

func decodeArguments(event registeredEvent, log types.Log) (json.RawMessage, error) {
	values, err := event.event.Inputs.NonIndexed().Unpack(log.Data)
	if err != nil {
		return nil, err
	}
	result := make(map[string]any, len(event.event.Inputs))
	indexedOffset, nonIndexedOffset := 1, 0
	for _, argument := range event.event.Inputs {
		if argument.Indexed {
			if indexedOffset >= len(log.Topics) {
				return nil, errors.New("chainindex/evm: missing indexed topic")
			}
			topic := log.Topics[indexedOffset]
			indexedOffset++
			if indexedHashed(argument.Type) {
				result[argument.Name] = lowerHash(topic)
				continue
			}
			decoded := map[string]any{}
			if err := abi.ParseTopicsIntoMap(decoded, abi.Arguments{argument}, []common.Hash{topic}); err != nil {
				return nil, err
			}
			result[argument.Name] = jsonValue(decoded[argument.Name], argument.Type)
			continue
		}
		if nonIndexedOffset >= len(values) {
			return nil, errors.New("chainindex/evm: missing non-indexed value")
		}
		result[argument.Name] = jsonValue(values[nonIndexedOffset], argument.Type)
		nonIndexedOffset++
	}
	return json.Marshal(result)
}

func indexedHashed(t abi.Type) bool {
	return t.T == abi.StringTy || t.T == abi.BytesTy || t.T == abi.SliceTy || t.T == abi.ArrayTy || t.T == abi.TupleTy
}
func parseCursor(c chainindex.Cursor) (uint64, error) {
	if c.Value == "" {
		return 0, ErrInvalidCursor
	}
	n, err := strconv.ParseUint(c.Value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %q", ErrInvalidCursor, c.Value)
	}
	return n, nil
}
func dispatchKey(address common.Address, topic common.Hash) string {
	return lowerAddress(address) + "\x00" + lowerHash(topic)
}
func lowerHex(b []byte) string             { return "0x" + hex.EncodeToString(b) }
func lowerHash(h common.Hash) string       { return lowerHex(h.Bytes()) }
func lowerAddress(a common.Address) string { return lowerHex(a.Bytes()) }
func hashesJSON(hashes []common.Hash) []string {
	values := make([]string, len(hashes))
	for i, h := range hashes {
		values[i] = lowerHash(h)
	}
	return values
}

func jsonValue(value any, typ abi.Type) any {
	if value == nil {
		return nil
	}
	switch typ.T {
	case abi.IntTy, abi.UintTy:
		switch n := value.(type) {
		case *big.Int:
			return n.String()
		case big.Int:
			return n.String()
		}
	case abi.AddressTy:
		if address, ok := value.(common.Address); ok {
			return lowerAddress(address)
		}
	case abi.BytesTy, abi.FixedBytesTy:
		if bytes, ok := value.([]byte); ok {
			return lowerHex(bytes)
		}
	case abi.SliceTy, abi.ArrayTy:
		v := reflect.ValueOf(value)
		result := make([]any, v.Len())
		for i := 0; i < v.Len(); i++ {
			result[i] = jsonValue(v.Index(i).Interface(), *typ.Elem)
		}
		return result
	case abi.TupleTy:
		v := reflect.ValueOf(value)
		if v.Kind() == reflect.Ptr {
			v = v.Elem()
		}
		result := make(map[string]any, len(typ.TupleElems))
		for i, elem := range typ.TupleElems {
			var field reflect.Value
			if v.Kind() == reflect.Struct {
				field = v.Field(i)
			} else if v.Kind() == reflect.Array || v.Kind() == reflect.Slice {
				field = v.Index(i)
			}
			if field.IsValid() {
				result[typ.TupleRawNames[i]] = jsonValue(field.Interface(), *elem)
			}
		}
		return result
	}
	return value
}
