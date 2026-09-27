# EVM 合约事件接入

`chainindex` 归档区块和合约日志，不创建代币、池子、交换或其他业务表。调用方提供 GORM 的 `*gorm.DB`、开始区块、RPC 和一个或多个合约 ABI。

## 1. 手工执行数据库脚本

按数据库选择并手工执行 `migrations/postgres/001_chainindex.sql` 或 `migrations/mysql/001_chainindex.sql`。脚本创建：

- `chainindex_cursors`：任务扫描进度和重组状态；
- `chainindex_blocks`：每个扫描区块的规范头字段与完整原始 JSON；
- `chainindex_events`：命中合约的完整 JSON-RPC log 字段、ABI 解码参数与原始 JSON。

包不会运行 migration，也不会打开或关闭数据库连接。

## 2. 准备 ABI

从 Solidity 编译输出中取 `abi` 数组并保存为 JSON，例如 `artifacts/contracts/My.sol/My.json`。索引器直接使用 ABI JSON，无需生成 Go 代码：

```go
artifact, err := os.ReadFile("artifacts/contracts/My.sol/My.json")
contract, err := evm.NewContract("0xYourContract", string(artifact))
```

如果文件是完整 Hardhat/Foundry artifact，请先取其中的 `abi` 字段；`evm.NewContract` 需要 ABI 数组本身。

可选地，若你的应用还要调用合约方法，可用 go-ethereum 的 `abigen` 生成绑定代码；这不是事件索引所必需的：

```sh
abigen --abi MyContract.abi --pkg contracts --type MyContract --out contracts/my_contract.go
```

## 3. 创建扫描器

```go
db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
if err != nil { return err }

client, err := evm.Dial(ctx, rpcURL, "1")
if err != nil { return err }
defer client.Close()

registrations, err := evm.Registrations(contractA, contractB) // 每个合约可有不同 ABI
if err != nil { return err }

indexer, err := chainindex.New(db, []chainindex.Job{{
	ID: "my-contract-events", ChainNamespace: "evm", ChainID: "1",
	StartCursor: chainindex.Cursor{Value: "21000000"},
	BatchLimit: 1, // 默认建议逐区块扫描
	Registrations: registrations,
	Handler: func(ctx context.Context, tx *gorm.DB, event chainindex.Event) error {
		// 可选：用同一事务写自己的业务表。
		return nil
	},
}}, []chainindex.Adapter{client})
if err != nil { return err }

return indexer.Run(ctx)
```

首次运行从 `StartCursor` 扫描；之后始终从 `chainindex_cursors` 的已提交进度继续。每个区块的区块头、命中日志和进度在同一个 GORM 事务里提交。日志唯一键为 `(chain_namespace, chain_id, job_id, transaction_hash, log_index)`，重复扫描不会重复入库或重复调用 handler。

## 保存的日志字段

`chainindex_events` 保留 Ethereum JSON-RPC 的 `address`、`topics`、`data`、`blockNumber`、`blockHash`、`transactionHash`、`transactionIndex`、`logIndex`、`removed`，以及 `topic0`、ABI `event_name`、`decoded_args` 和 `raw_log`。整数解码为十进制字符串；地址、哈希、bytes 均为小写 `0x` 字符串；动态 indexed 参数只保存不可逆的 topic hash。
