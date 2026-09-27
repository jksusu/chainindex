package chainindex_test

import (
	"context"
	"os"

	chainindex "github.com/jksusu/chainindex"
	"github.com/jksusu/chainindex/evm"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func ExampleIndexer_SyncOnce() {
	ctx := context.Background()

	// Run migrations/postgres/001_chainindex.sql before creating the indexer.
	// The application owns this handle, including its lifecycle and pool settings.
	db, err := gorm.Open(postgres.Open(os.Getenv("DATABASE_URL")), &gorm.Config{})
	if err != nil {
		panic(err)
	}
	sqlDB, err := db.DB()
	if err != nil { panic(err) }
	defer sqlDB.Close()

	client, err := evm.Dial(ctx, os.Getenv("EVM_RPC_URL"), "1")
	if err != nil {
		panic(err)
	}
	defer client.Close()

	erc20, err := evm.NewContract(
		"0x0000000000000000000000000000000000000001",
		`[{"anonymous":false,"inputs":[{"indexed":true,"name":"from","type":"address"},{"indexed":true,"name":"to","type":"address"},{"indexed":false,"name":"value","type":"uint256"}],"name":"Transfer","type":"event"}]`,
	)
	if err != nil {
		panic(err)
	}
	governor, err := evm.NewContract(
		"0x0000000000000000000000000000000000000002",
		`[{"anonymous":false,"inputs":[{"indexed":true,"name":"proposalId","type":"uint256"}],"name":"ProposalCreated","type":"event"}]`,
	)
	if err != nil {
		panic(err)
	}
	registrations, err := evm.Registrations(erc20, governor)
	if err != nil {
		panic(err)
	}

	indexer, err := chainindex.New(db, []chainindex.Job{{
		ID:                 "mainnet-events",
		ChainNamespace:     "evm",
		ChainID:            "1",
		StartCursor:        chainindex.Cursor{Value: "19000000"},
		ConfirmationPolicy: chainindex.ConfirmationPolicy{Confirmations: 12},
		BatchLimit:         500,
		Registrations:      registrations,
	}}, []chainindex.Adapter{client})
	if err != nil {
		panic(err)
	}
	if err := indexer.SyncOnce(ctx); err != nil {
		panic(err)
	}
}
