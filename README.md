# DBX SQL Server Agent Manager · 0.1.3

## 0.1.3：记住 MCP Token

- 新增「记住 Token（macOS 钥匙串）」和「清除已保存 Token」。首次输入并勾选，连接成功后保存；下次打开留空，后端自动使用保存值，不把 Token 返回界面。
- 凭据按规范化 MCP 地址区分，不按数据库连接区分。同一 MCP 地址下切换 SQL Server 连接可复用；其他端口/路径不会取到该凭据。仅查询本插件固定 service 下的精确 account，不枚举系统凭据。
- 使用 macOS Security.framework 的 Keychain API，不通过命令行参数传递 Token，也不明文写入插件偏好、日志或 localStorage。系统可能在首次访问、钥匙串锁定或插件升级后要求授权；拒绝授权时仍可手动填写。开发预览未签名包的升级可能触发再次授权。
- 清除按钮或取消已勾选的记住选项会删除当前地址的保存值。输入新 Token 并连接成功后可替换旧值；连接失败不会覆盖旧值。
- 优先级：本次输入 → `DBX_AGENT_MCP_TOKEN` → 当前地址的钥匙串值。清除钥匙串不会删除用户自行设置的环境变量。
- 本次构建目标为 macOS arm64；macOS 构建需本机 SDK 和 `CGO_ENABLED=1`。其他平台/禁用 CGO 的构建不提供凭据持久化，不能回退为明文保存。
- 已通过模拟凭据库/模拟 MCP 与界面重开测试；没有读取或修改真实钥匙串，原生授权及真实 DBX 安装尚待用户实测。

## 0.1.1 修复说明（本版保留）

- 兼容官方 DBX 将成功结果首行首列写入 `error_message` 的响应。只对插件内置只读查询、消息与首值完全一致的情况兼容；真实错误、回滚、未知结果和写入异常仍拒绝。
- 修正 SQL Server 2008 R2 不支持的 `THROW`：改用 `RAISERROR`，保留显式回滚和退出。错误信息以字符串参数传入，避免其中 `%` 被当格式占位符。
- 服务状态查询先检查 DMV 是否可见；旧版本缺少 DMV 时显示不可用，不将其解释为 Agent 停止。
- 官方 DBX 无需重新编译。使用相同插件 ID 安装升级包，保留原来的 MCP 地址、Token 和授权。若宿主未自动重载，请重启 DBX，再确认插件界面显示 0.1.3。
- 先连接并查看作业列表；写操作请先在可丢弃测试实例验收。本次未连接真实 SQL Server，2008 R2 语法和事务行为尚待实机验证。

本仓库是官方商店审核候选源码，尚未获得官方上架或 verified 认证。当前发布目标仅为 macOS Apple Silicon。隐私与数据访问见 [PRIVACY.md](PRIVACY.md)，发布说明见 [CHANGELOG.md](CHANGELOG.md)。

根因定位：DBX 源码快照 `761f1b7d3703d8c0d34616e007cea83ee5481537` 的 `crates/dbx-mcp/src/backend.rs`，两条批次转换路径都在没有错误详情时无条件回退到 `rows[0][0]`。成功时 `execution_error=false` 还会被省略，因此字段缺失本身不是失败。

面向 SQL Server 的独立 DBX 插件，按 SSMS「SQL Server Agent」的对象树与作业属性组织功能。
**这是可构建、已通过模拟测试的开发预览版，尚未在真实 DBX / SQL Server 上安装联调，不是完整 SSMS 替代品。**

## 功能范围

| SSMS 功能 | 本版实现 |
|---|---|
| 作业活动 | 列表、搜索、状态过滤、手动/15秒刷新；上次结果、运行状态、所有者 |
| 作业定义 | 新建（默认禁用）、修改名称/说明/起始步骤、删除、启用/禁用 |
| 执行控制 | 启动、停止、从指定步骤启动；均先预览确认 |
| 步骤 | T-SQL 新增/修改/删除；数据库、命令、成功/失败动作、跳转、重试 |
| 其他子系统 | 只读查看；不编辑 CmdExec、PowerShell、SSIS 等步骤 |
| 计划 | 新建一次/每日/每周/每月/相对月份/Agent启动/CPU空闲计划并绑定；已有计划按ID绑定/解绑 |
| 共享计划 | 查看绑定数量；不修改/删除计划本体；解绑保留未使用计划 |
| 历史 | 最近500条步骤和作业汇总、错误消息、重试次数、时长 |
| 脚本导出 | 禁用的可移植 T-SQL 作业副本；不是完整备份 |
| 警报、操作员、类别 | 只读列表 |
| Agent 服务 | 只读状态，依赖额外服务器权限 |
| 所有者、通知、类别、自动删除策略 | 显示部分属性，暂不编辑 |
| 代理凭据、服务启停、多服务器管理、维护计划设计器 | 暂不支持 |

计划按服务器本地时间调度，关闭插件不影响服务器 Agent。开始日期默认取电脑日期，请核对服务器日期。
列表最多2000个作业；历史最多500条。下次运行时间可能因 SQL Server Agent 缓存而延迟。
运行状态来自 Agent 最新会话活动记录，不是插件进程状态；服务异常时应结合服务页/SSMS核验。

## 安装与连接

提供 macOS Apple Silicon (`darwin-arm64`) 包，插件 ID 与 SQL 发布插件不同，可以并存。

1. DBX → 插件中心 → 设置 → 第三方与开发者选项，仅在测试时允许未签名开发包。
2. 安装 `dist/io.github.fathermrsing.sql-agent-0.1.3-darwin-arm64.unsigned.dbxp`。
3. 从工作台打开 SQL Server Agent，或右键 SQL Server 连接打开同名入口。
4. DBX 设置 → MCP → 启用本机 HTTP 服务，核对实际地址与 Bearer Token。
5. 为测试连接显式授权 `msdb` 与 `dbx_execute_batch`，按实际管理 SQL 开放所需写入权限。
   不要为了插件关闭全局只读/生产保护。权限不足时停止，按 DBX 提示核实具体授权。
6. 插件中选择已有 SQL Server 连接，填写本机 MCP 地址、Token，点击「连接 Agent」。
7. 测试后关闭允许安装未签名开发包。

只支持本机回环 HTTP MCP；不支持远程 MCP 或独立 stdio MCP。
插件使用 DBX 已保存连接的配置和驱动，不读取密码，不承诺共享编辑器的同一个物理数据库会话。
不勾选「记住 Token」时仅本次使用；勾选并连接成功后保存到本机钥匙串。普通偏好和操作日志不保存 Token。可选环境变量 `DBX_AGENT_MCP_TOKEN`，GUI宿主未必继承终端环境。
所有数据库访问都固定到 `msdb`；步骤的业务目标数据库独立配置。

需要支持 Agent 的 SQL Server 版本与正在运行的 Agent 服务；Express 不支持 Agent。
登录需有 msdb 的 SQLAgentUserRole、SQLAgentReaderRole、SQLAgentOperatorRole 或 sysadmin。
UserRole只列出自己作业；Reader/Operator可见更多，但修改/启停权限仍由服务器存储过程判定。
不是角色成员但通过自定义GRANT取得权限的场景，本版身份预检尚不支持。
服务状态查询另需 SQL Server 版本对应的服务器状态/安全状态权限；失败不代表作业功能不可用。
当前登录、作业所有者、步骤执行身份不同，尤其应审核高权限作业。插件不会授予角色或设置凭据。

## 第一个测试作业

只在可丢弃测试环境操作：

1. 新建一个唯一名称作业，如 `DBX_AGENT_SMOKE_<日期>`，检查预览后输入完整目标标识，确认创建；初始禁用。
2. 选中作业 → 步骤 → 新增 T-SQL；数据库选测试库，命令先用 `SELECT 1;`。
3. 核对成功/失败动作和重试设置，预览确认。手动启动后刷新历史，验证真实执行结果。
4. 计划 → 新建每日计划，先保持作业禁用，核对服务器日期时间及共享计划绑定数。
5. 在 SSMS 交叉检查定义，再测试启用/禁用、步骤修改、停止长耗时测试作业、绑定/解绑。
6. 删除测试作业后核对保留的未使用计划，按需由 SSMS 人工清理。不要删除其他作业使用的计划。

以上是真实联调验收清单，**开发时未执行这些数据库操作**。

## 预览、执行与异常处理

- 预览只读取身份/定义并生成 SQL，不执行管理变更。计划有效期10分钟、一次使用。
- 确认文本包含连接ID、服务器、操作、作业名称。更改连接/地址/Token会清空旧数据和预览。
- 执行前重新读取实例、登录、角色、作业/步骤/计划定义；变化则拒绝。
- 这不是服务器端全局锁。复核后到存储过程执行间仍有外部并发窗口；多管理员应协调操作。
- CRUD SQL 采用 TRY/CATCH + 显式事务；启动/停止不包事务；检查系统SP返回码。
- SQL封装为单个 `EXEC(N'...')`，避免客户端拆分局部变量和事务；步骤命令原样转义保存，包含GO的实际执行兼容性仍需真实联调。
- 异步状态 `accepted` 只说明管理调用已完成，不代表作业最终成功或确已停止；请刷新历史/活动确认。
- 发出请求后断线、超时或响应错误统一视为 `unknown`，不自动重试。执行超时85秒不等于服务器取消执行。

插件数据目录由宿主 `DBX_PLUGIN_DATA_DIR` 提供：

- `operation-<id>.json`：请求状态、目标和时间，不含Token或SQL正文。
- `.agent-operation-lock`：进程间操作锁；unknown或日志落盘失败时保留，防止重复提交。

若出现unknown/异常退出：先在SSMS核验作业、步骤、计划及实际运行历史；确认Sidecar已经停止且没有在途调用，备份数据目录与日志后，才可人工移走该锁重新打开插件。**不要在请求仍可能执行时直接删锁或重试。** 没有自动解锁按钮。
操作日志不是 SQL Server 审计或完整DDL备份，不提供无限增长清理策略。所有SQL命令执行后可能有服务外部副作用，不存在通用自动回滚。

## 脚本副本不是恢复备份

副本使用原名加 `_copy` 且默认禁用；保留基础TSQL步骤、跳转、重试及起始步骤。
不导出所有者、类别、通知、自动删除策略、计划和绑定，不保证原执行身份语义。
含非TSQL步骤或高级输出/代理/执行用户设置时直接拒绝导出，避免静默丢配置。
删除原作业前，应使用 SSMS 生成完整脚本并核验，而不是仅保留这里的副本。

## 构建与离线验证

无需npm依赖；Go SDK已随源码提供，不下载外部Go依赖。

```sh
cd backend
go test -race -cover -v ./...
go vet ./...
cd ..
node scripts/protocol-smoke.mjs
node scripts/build.mjs darwin-arm64
python3 scripts/verify-package.py dist/*.dbxp
```

单元测试/协议测试会创建仅监听本机的模拟HTTP服务器；不会访问真实DBX/数据库。
`ui/demo.html` 是明确标记的只读UI模拟，可用静态HTTP服务打开；模拟执行调用全部拒绝。
`ui/index.html` 必须在DBX工作台使用，不会自行绕过宿主连接数据库。
验证详情见 `TESTING.md`。编译包只含运行文件及第三方许可；README/测试在源码包中。

## 契约与参考

插件按本地DBX源码提交 `761f1b7d3703d8c0d34616e007cea83ee5481537` 的 Manifest v1 / Host API 1.4 / Go SDK实现，不声称这是今天的最新提交。

- [DBX 插件说明](https://github.com/t8y2/dbx/blob/761f1b7d3703d8c0d34616e007cea83ee5481537/plugins/README.md)
- [SQL Server Agent 存储过程](https://learn.microsoft.com/en-us/sql/relational-databases/system-stored-procedures/sql-server-agent-stored-procedures-transact-sql?view=sql-server-ver17)
- [Agent 固定数据库角色](https://learn.microsoft.com/en-us/ssms/agent/sql-server-agent-fixed-database-roles)
- [作业步骤参数](https://learn.microsoft.com/en-us/sql/relational-databases/system-stored-procedures/sp-add-jobstep-transact-sql?view=sql-server-ver17)
- [计划参数](https://learn.microsoft.com/en-us/sql/relational-databases/system-stored-procedures/sp-add-schedule-transact-sql?view=sql-server-ver15)

## License

[Apache-2.0](LICENSE). Copyright 2026 fathermrsing. Third-party attribution is in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
