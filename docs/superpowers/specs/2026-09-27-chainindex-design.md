# Chainindex 设计说明

## 目标

`chainindex` 是一个可通过 Go module 引入的链上事件索引包。调用方注册链、合约地址、ABI 与事件处理器后，包负责可靠地扫描链上事件并将通用索引数据写入调用方提供的数据库连接。首版完整支持所有 EVM 兼容链，并为 Solana、Sui 保留适配器边界。

## 非目标

- 不创建、配置或关闭数据库连接。
- 不在运行时执行任何 SQL migration；使用者手动执行随包提供的 SQL 文件。
- 不携带 CookPump 的业务模型、常量、价格查询或业务表写入逻辑。
- 首版不实现 Solana 或 Sui 的网络读取。

## 从 CookPump 提取的通用同步逻辑

原仓库的 `watcher/block/watcher.go` 与 `watcher/transfer_fee/watcher.go` 提供了可抽取的模式：

- 以区块批次拉取指定合约与事件 topic 的日志；
- 使用确认数避开尚未稳定的区块；
- 持久化已完成高度，在重启后从下一高度继续；
- 为每条日志解析 ABI，保存交易哈希、日志索引和区块时间；
- 处理多个合约及不同 ABI。

CookPump 特有的 parser、GORM 模型、全局配置和固定合约/topic 不迁移；它们改由调用方通过配置和处理器提供。

## 架构

### 公共协调层

根包创建 `Indexer`。它接收：

- 注入的 `storage.Executor` / `storage.Beginner`；
- 一个或多个链适配器；
- 一个或多个独立的索引任务（`Job`）。

每个 `Job` 都有稳定的 `ID`、所属链、`StartCursor`、确认策略、批次上限、轮询间隔及合约注册项。`Run(ctx)` 在上下文取消时停止；`SyncOnce(ctx)` 便于定时任务和测试。游标语义固定为“最后一个已完整提交的安全链位置”；不存在持久化游标时，由适配器从 `StartCursor` 计算首个范围。配置的 `StartCursor` 仅对不存在 cursor 行的 job 生效；已有行永远以已提交值续跑。

核心适配器接口为：`Namespace() string`、`ChainID(context.Context) (string, error)`、`SafeHead(context.Context, ConfirmationPolicy) (Cursor, error)`、`NextRange(after *Cursor, start Cursor, safe Cursor, limit uint64) (Range, error)`、`Events(context.Context, Range, Registrations) ([]Event, error)` 与 `CanonicalHash(context.Context, Cursor) (string, error)`。因此游标解析、比较、推进与批次单位都归适配器负责：EVM 的 range 是闭区间区块范围，Solana 可按 slot，Sui 可按 checkpoint；核心只持久化它们的规范字符串值。

### EVM 适配器（首版实现）

`evm.Client` 使用 go-ethereum 的 RPC 接口读取 chain ID、最新区块、区块时间与 FilterLogs。每个 `Contract` 包含地址及 ABI JSON；注册事件时以 `(contract address, topic0/event ID)` 唯一定位，而不是事件名。一个 job 可以注册多个 contract，每个 contract 可以持有不同 ABI；同一地址 + topic0 的重复注册在创建阶段报错。首版拒绝 anonymous event，避免无法可靠地按 topic0 过滤和分派。

匹配到日志后，适配器以 ABI 解码 indexed 和 non-indexed 参数，并生成规范化的 `Event`：链类型、链 ID、job ID、发出事件的主体、事件类型、链原生交易 ID、事件序号、规范化游标、区块/检查点哈希、UTC 时间、原始载荷和 JSON 参数。EVM 映射为：主体=合约地址、事件类型=`topic0`、交易 ID=交易哈希、事件序号=日志索引、游标=十进制区块高、原始载荷=`{topics,data}`。字符串一律使用小写 `0x` 十六进制；整数与 `big.Int` 使用十进制字符串；地址使用小写 `0x`；bytes 使用小写 `0x`；数组和 tuple 递归编码为 JSON。indexed 的动态/复杂 Solidity 类型仅保存其不可逆 topic hash，而不会错误宣称已恢复原始值。

### 持久化边界

包仅约束调用方传入能够执行 SQL 与开启事务的实例，不依赖 GORM 或任何数据库驱动。生产使用可传 `*sql.DB`；事务执行时使用 `*sql.Tx`。精确接口为：`DB` 提供 `BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)`；`New(db DB, dialect storage.Dialect, ...)` 显式接收 `storage.Postgres` 或 `storage.MySQL`。内部 persister 根据该选项生成相应的 `ON CONFLICT` 或 `ON DUPLICATE KEY` SQL。handler 接收 `*sql.Tx` 与标准事件，使用 `ExecContext` / `QueryContext` 写业务表。包从不关闭调用方的 `DB` 或 `Tx`。这使 `*sql.DB` 可直接使用，也允许调用方以包装类型实现同一 `BeginTx` 能力。

每个已确认批次在一个事务中执行：对每个规范化事件按 `(chain_namespace, chain_id, job_id, transaction_id, event_index)` 幂等插入标准事件；只有插入实际新增一行时才调用业务 handler。handler 必须只通过传入的 `*sql.Tx` 写业务表。首个批次以 cursor 主键的条件 `INSERT` 创建行；唯一键冲突表示另一运行者已创建，事务回滚并重新加载。已有行的批次则以“旧 cursor 值仍匹配”的条件更新 job 游标；条件更新未影响一行时，事务失败并重试。事务失败则游标不推进，下一次安全重试。此机制避免常规重复投递；业务 handler 仍应以其业务唯一键保持幂等。

### 多数据库 SQL

- `migrations/mysql/001_chainindex.sql`
- `migrations/postgres/001_chainindex.sql`

使用者根据实际数据库手动执行对应文件。两个脚本创建同一逻辑表：`chainindex_cursors` 与 `chainindex_events`，并使用各自方言的 upsert、JSON 类型、时间类型和唯一索引。包启动时只校验/使用这些表，不会执行 SQL 文件。

`chainindex_cursors` 的主键为 `(chain_namespace varchar(32), chain_id varchar(128), job_id varchar(128))`，并保存 `cursor varchar(256)`, `canonical_hash varchar(256)`, `status varchar(32)`, `last_error text`, `updated_at`（UTC）。`chainindex_events` 的唯一键为 `(chain_namespace, chain_id, job_id, transaction_id varchar(256), event_index varchar(128))`，另有 `emitter varchar(256)`, `event_type varchar(256)`, `cursor varchar(256)`, `canonical_hash varchar(256)`, `occurred_at`（UTC）, `payload` 与 `arguments`。PostgreSQL 使用 `jsonb`；MySQL 使用 `json`。两个 migration 都将全部身份键文本列设为 bytewise/case-sensitive：PostgreSQL 指定 `COLLATE "C"`，MySQL 使用跨支持版本可用的 `utf8mb4_bin`。这保留 Solana/Sui 的大小写敏感 base58 值。所有链 ID、游标、哈希和原始值均为明确长度的文本，避免将未来链的 ID 或非数值游标截断。

### 非 EVM 链扩展点

核心定义不包含 EVM 的 address/topic 类型。链适配器只需实现读取安全游标范围及输出标准 `Event` 的能力。后续 `solana` 与 `sui` 适配器将映射各自的 slot/checkpoint（十进制字符串游标）、program/package（主体）、signature/digest（交易 ID）和 instruction/log index 或 event sequence（事件序号）到同一规范化字段；原始链数据放入 JSON payload。现有存储、重试和 handler API 不变。

## 对外使用形态

调用方将：

1. 手动执行目标数据库对应的 migration 文件；
2. 自己打开 `*sql.DB` 并传给 `chainindex.New`；
3. 以 EVM RPC URL 创建适配器，注册 job、多个合约、ABI 与事件 handler；
4. 调用 `Run(ctx)` 或由自身调度器周期调用 `SyncOnce(ctx)`。

handler 收到解码后的通用事件和注入事务，可选择只保留内置原始事件，或写入应用的业务表。

## 错误处理与一致性

- 非法 ABI、空 job ID、重复注册、无效地址/链 ID 在创建阶段返回错误。
- RPC、日志过滤、区块读取、解码、handler 或数据库错误都会使当前批次失败且游标不前移。
- 已保存事件通过唯一键去重，支持重试。
- 确认数默认为安全值但可按任务配置。EVM 在处理每条日志前以区块高度读取 canonical header（`HeaderByNumber(log.BlockNumber)`），将其 hash 与 `log.BlockHash` 比较，且时间戳只能来自这个已匹配 header；不一致则整个批次失败。事务提交前再次读取批次末尾 canonical hash，若与扫描期间的 hash 不同则回滚。每个批次持久化末尾 canonical hash；下一次同步前校验该 hash。发生不一致时，首版将 cursor 的 `status` 持久化为 `reorg_detected` 并记录 `last_error`，然后返回 `ErrReorgDetected`。状态为 `reorg_detected` 的 job 拒绝继续同步，直到调用方使用显式 `Rewind` API 选择安全游标并把状态恢复为 `ready`；不自动回滚应用业务数据。这提供概率最终性而非自动重组修复。
- 条件游标更新可防止两个运行者静默同时推进，但首版不提供跨进程租约；同一个 job 应只由一个进程运行。未来可在 cursor 表增加租约而不改变 API。

## 测试策略

- 使用假的 EVM 读取器和内存 SQL driver/测试数据库验证区块范围、确认数和游标推进。
- 用真实 ABI 样例验证 indexed 与 non-indexed 参数解码。
- 验证多合约、多 ABI、重复日志、handler 失败与 SQL 方言脚本内容。
- 全部新增行为遵循先写失败测试、再最小实现的流程。

## 交付物

- 可安装的 `chainindex` Go module；
- EVM 适配器；
- MySQL 与 PostgreSQL 手动 migration；
- 公开 API 文档及最小使用示例；
- Solana/Sui 适配器接口和未实现占位说明。
