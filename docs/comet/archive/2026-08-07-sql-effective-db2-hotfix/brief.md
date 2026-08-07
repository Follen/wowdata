# Outcome

为 wowdata 建立两套边界清晰、互不耦合的只读查询能力：

1. `wowdata sql`：只查询静态 DB2/DBC/WDC 数据，使用 `wowdata-sql-v1`、DBD binder、logical planner 和 physical executor。
2. 独立 Hotfix 查询体系：查询本地 DBCache、Wago 历史数据、Raidbots 近期快照和缓存记录，支持最近/最新、Build、region、locale、table、record、push、status、时间与 raw/decoded 数据。

Hotfix 不进入 DB2 SQL catalog，不生成 effective row，不修改现有领域命令结果。两套体系只共享 target context、DBD schema 解析、缓存基础设施和统一输出 envelope 等底层组件。

# Scope

## Static DB2 SQL

- `wowdata sql`：query string、`--file`、`--stdin`、named `--param`、JSON/JSONL/CSV。
- SQL lexer/parser/AST、NULL 三值逻辑、JOIN、聚合、CTE/受限 recursive CTE、EXPLAIN/EXPLAIN ANALYZE。
- 仅有 static DB2 catalog；Build/product/locale/schema binder。
- record-ID、relationship lookup、projection/filter pushdown、bounded scan、streaming output。
- typed column batch、late materialization、selection vector、编译后的表达式 kernel 与 Filter/Project/Limit 算子融合；通用 `map[string]interface{}` 只允许出现在兼容/输出边界，不作为 SQL 热路径行表示。
- 成本驱动的 physical planner：按 WDC/DBC 文件形态、ID/relationship 索引、基数、选择率、行宽、排序性、内存与 spill 预算，在 point/multi-get/scan、index nested-loop/hash/merge join、stream/hash/sort aggregate、Top-K/bounded/external sort 之间选择。
- 每类物理算子使用真实本地 DB2 corpus 做候选算法 tournament、holdout 验证与 p50/p95/RSS/alloc/read/decode 对比；正确性门禁先于性能排名。
- 现有 db2、spell、encounter、item、creature、decor、file、icon 命令可迁移到同一静态 DB2 planner/executor，同时保持兼容。

## Independent Hotfix query

- 独立命令和独立 query model/planner/executor，不复用 SQL AST、DB2 logical plan 或 DB2 row store。
- Wago 是默认远端 provider；本地 DBCache 与 managed cache 支持磁盘点查；Raidbots 是约 30 天的近期快照补充。
- 支持 product、完整 Build、region、locale、table、record ID、push ID、status、时间范围、最近/最新、source、raw/decoded、稳定排序和分页。
- Wago HTML/Inertia adapter、原始响应缓存、SHA-256、coverage/completeness、分页漂移与协议变化检查。
- DBCache V1-V9 流式磁盘 reader 与固定宽度 sidecar offset index；payload 按 offset 延迟读取和 DBD 解码。
- provider fallback：Wago 失败时，只有当前/最新且存在完全匹配快照的查询可回退 Raidbots并报告 warning/provenance；历史查询返回 `hotfix_coverage_incomplete`。

## Test datasets

- 本地 DB2 功能测试集：基于本机已安装客户端和真实 DBD/DB2 文件，覆盖 retail、Classic、Titan 等可发现产品/Build/locale，保存 corpus manifest、文件/Schema hash、表/字段/关系用例与预期结果。
- 本地 DB2 性能测试集：固定 point lookup、batch ID、relationship、projection、scan、JOIN、aggregate、streaming case，分别运行 cold/warm/repeat-warm，并记录 wall time、CPU、RSS、读取字节、decoded rows 和 fallback。
- Wago Hotfix 测试集：保存真实 `/hotfixes` HTML/Inertia 响应与 SHA-256，覆盖 retail/Titan、search 假阳性、分页、coverage 起点、null/positional/nested/string-number data、status、协议错误与缓存。
- Wago live probe 与离线 fixture 分离：离线 fixture 保证确定性回归；有界 live integration 只验证当前协议/分页/Build coverage，并保存 receipt，不用变化中的行数作为固定 golden。

# Non-goals

- 不在 `wowdata sql` 中提供 `effective` 或 `hotfix` catalog。
- 不把 Hotfix 应用到静态 DB2 row，不建立 Effective Row Store，不实现 insert/update/delete overlay。
- 不给现有领域命令增加 `--hotfix`，不让 spell/item/encounter 等命令隐式访问 Hotfix provider。
- 不在同一查询中 JOIN DB2 与 Hotfix，不共享 SQL binder/logical plan/physical executor。
- INSERT/UPDATE/DELETE、DDL、事务/WAL、数据库 server、常驻 HTTP 服务、完整 ANSI SQL。
- 修改 CASC、DB2、DBCache 或远端 Hotfix 数据；把 Wago 当成 Blizzard canonical source；把单个 DBCache 或 Wago 当前集合描述为所有版本的完整历史。

# Acceptance examples

- `wowdata sql "SELECT * FROM SpellEffect WHERE SpellID = :spell_id" --param spell_id=100` 只读取 static DB2，输出 catalog=`static`、target context、columns、rows 和 execution metrics；执行期间不访问 Hotfix provider/cache。
- SQL 支持 `--file`、`--stdin`、JSON、JSONL、CSV；大结果流式输出。
- SQL 的点查询使用 record-ID/relationship lookup；字段歧义、类型错误、未知表列和不支持语法返回稳定错误。
- `EXPLAIN ANALYZE` 逐算子报告 estimated/actual rows、visited/scanned/decoded/output rows、decoded fields、batch 数、文件读取/复制/spill bytes、wall/CPU、allocations、peak live/RSS、fallback 与取消状态。
- 对存在原生 record-ID/relationship 定位能力的 point、batch 和 relationship case，计划不得调用 `GetAllRows`，不得回退全表 scan；EXPLAIN 中对应 full-scan 次数为 0，解码量与请求命中集合而非表总行数相关。
- JOIN、aggregate、ORDER BY/LIMIT 与 streaming case 必须在相同结果 hash 下比较合理候选，按查询形态选择低延迟/低内存的物理算子；超出内存预算时有界 spill 或返回稳定资源错误，不依赖 OOM。
- 独立 Hotfix 查询可表达：Titan build 69137、region 196、zhCN、表 `SpellPowerDifficulty`、record/push/status/时间过滤，以及最近/最新和历史分页。
- 本地 DBCache 点查使用磁盘 sidecar 定位 payload offset；不把整个 DBCache、全部 payload 或全部 sidecar entry 复制到 heap。
- Wago search 只作候选加速，结果解析后按完整上下文精确过滤；分页/结构变化返回协议或 coverage 错误，不解释为空结果。
- 现有领域 CLI 的命令、参数、JSON、退出码、导出文件和 golden 保持 static 兼容。
- 固定命令与 `wowdata sql` 对同一 static DB2 查询生成等价 binder/planner/executor 行为；固定命令可直接构造 typed AST/plan，不要求拼接或重新解析 SQL 字符串，领域格式化保留在命令输出层。
- 本地 DB2 corpus 可重复运行功能与性能矩阵，结果绑定目标 Build、DBD/DB2 hash 和机器环境；SQL 测试期间 Hotfix provider 调用为 0。
- Wago Hotfix corpus 可离线复现 adapter/filter/pagination/coverage 行为，并有独立有界 live probe；Hotfix 测试期间 SQL/DB2 planner 调用为 0。

# Constraints and invariants

- DB2 SQL 与 Hotfix 查询是两个独立的 public command/query boundary。
- 共享 DBD 只用于 schema 解释：DB2 binder 绑定静态表；Hotfix decoder 解释 positional payload。共享 schema library 不代表共享 query planner。
- 默认 static 路径不访问 Wago、Raidbots、DBCache sidecar 或 Hotfix cache。
- Wago 是远端默认 provider；所有记录在 adapter 解析后按 product、完整 Build、region、locale、table、record、status 精确过滤。
- Wago `search` 是全文候选搜索；0 条不单独证明完整 miss。
- Wago cache identity 绑定 provider、protocol/parser version、完整 version、page、search/filters、region、locale、响应 SHA-256、冻结 total/last_page 和 coverage/completeness。
- DBCache 原文件是 source of truth；sidecar 只保存固定宽度 metadata、operation、payload offset/length，并按页或 mmap 查询。
- Hotfix status 保留 raw；operation 只作为查询结果中的已验证分类，不驱动 DB2 overlay。
- 多条 Hotfix 默认按 PushID 稳定排序；Wago `created_at` 表示检测/写入时间，不替代 PushID 顺序。
- 参数不通过字符串拼接执行；资源预算、取消、稳定错误码和 provenance 必须可审计。
- 物理计划必须可解释且确定：同一 snapshot、统计与参数类型生成稳定计划；并发度变化不得改变结果顺序或字段值。
- 小查询优先低调度开销串行 fast path；只有实测收益覆盖 goroutine/queue/synchronization 成本时才并行。所有 worker、batch、hash table、sort/aggregate buffer 与 spill 均受统一查询预算和背压约束。
- 性能优化不得绕过 Build/schema 正确性、NULL/溢出语义、snapshot 生命周期、取消传播、内存所有权或稳定错误契约。

# Research findings

- Wago `/api/hotfixes` 返回 404；可用入口为 HTML/Inertia `GET /hotfixes`，数据位于 `div#app[data-page]` 解码后的 `component=Hotfixes`、`props.hotfixes`。
- Laravel 分页含 current_page、last_page、per_page、total、data、next_page_url；record 含 id、push_id、record_id、status、build、table_name、data、created_at、region_id、locale、search_text。
- `data` 为 null 或 positional array，可能嵌套数组与字符串数字；必须使用 table_name + 完整 Build DBD 解码。
- `search=69137` 实测 1,508 条且当前页匹配 Titan build 69137/region 196/zhCN；`search=68943` 实测 11,307 个候选且混有 build 68914，证明 search 不是精确字段过滤。
- Wago `/api/builds` 当前含 28 个 `wow_classic_titan` Build；最新实测 3.80.2.69137，本机 3.80.2.68943 也在清单中；Hotfix 存在 Titan `SpellPowerDifficulty` 样本。
- Wago 当前集合 total=17,002,631、last_page=680,106；最老页样本到 2024-12-30/build 57212/push 90134，因此需要 coverage 起点和 completeness。
- Raidbots 约保留 30 天 DBCache，只作近期快照补充。
- DBCache V9 是 44-byte 文件头 + 重复的 32-byte record header + payload；格式没有内置 table/record TOC。
- 本机 retail DBCache 为 1.335 MiB/12,171 records；磁盘 sidecar 原型 389,472 bytes，点查只读取命中 payload。
- 现有 `internal/db2` 已有 point/batch/relationship/projected scan/stream fast path，物理标签主要是 `point-or-scan`、`projected-scan`、`relationship`、`stream-scan`；尚无 SQL JOIN、aggregate、sort、CTE 的 typed physical pipeline。
- 已归档 DB2 corpus `db2-corpus-r31-final` 覆盖 2,484 张表、成功加载 2,097 张、差分错误 0，但峰值 heap 约 3.10 GB；旧端到端报告中的 DB2 point/search/stream 仍出现约 0.55-0.67 GB 峰值内存，证明 SQL 热路径必须避免全行 map 物化并设置逐算子内存门禁。
- 已归档性能规格要求 projection/filter pushdown、late materialization、typed column batch、轻量成本选择、有界 buffer reuse、真实 Build cold/warm/repeat-warm 和候选算法 tournament；本 change 将其收敛为 SQL 每个物理算子的可测契约，而不是复用旧毫秒数作为跨机器硬阈值。

# Decisions

- change 名称与 capability ID 保留 `sql-effective-db2-hotfix`，但 `effective` 不再代表本 change 的产品行为。
- `wowdata-sql-v1` 只查询 static DB2。
- 用户最新确认：Hotfix 与 DB2 查询解耦，Hotfix 是独立查询体系；支持查询即可，不应用到 DB2/领域结果。
- Hotfix 不出现在 SQL catalog，现有领域命令不增加 `--hotfix`。
- Wago 为默认远端 provider；Raidbots 为近期快照补充。
- 用户确认 fallback：当前/最新查询可在完全匹配时回退 Raidbots；历史查询不以短窗口替代 Wago coverage。
- 用户要求：必须提供基于本机真实 DB2 的功能与性能测试集，以及基于真实 Wago 响应的 Hotfix 测试集。
- 用户确认独立 Hotfix CLI 入口使用 `wowdata hotfix query` 与组合 flags。
- 用户确认现有固定领域命令保留原命令/输出，但内部迁移为同一 static DB2 SQL AST 或 logical-plan API；不再各自维护私有扫描、过滤和手写 join。
- 用户确认 `wowdata hotfix query --latest` 在完整 product/Build/region/locale 与其他 filters 内找到最大 PushID，并返回该 PushID 的整批记录；不是每个 table/record 各取最后一条。
- 用户要求 SQL 物理算子和性能优化到极致；落实方式是对真实 DB2 查询形态提供专用 typed/vectorized 算子、成本 dispatch、逐算子指标、候选 tournament、holdout 与资源上限验证，而不是只增加抽象 planner 层。
- 用户确认采用严格性能门禁：所有固定 case 先保持 100% 结果等价；p95、peak RSS、alloc bytes、读取 bytes 或 decoded/scanned rows 出现统计噪声外退化即阻塞，不能用总体平均提速掩盖单个 case；每种查询形态选择正确候选中的 Pareto 最优算子。
- 用户确认当前 Shape 已完成，并要求直接进入 Build；该确认覆盖本 brief、完整目标规格、范围、关键决定、验收标准与非目标。

# Open questions

无。用户已确认按当前完整合同进入 Build。

# Verification expectations

- `local-db2-functional`：真实本地客户端 corpus，覆盖 schema/binder、point/batch/relationship、projection/filter、scan/JOIN/aggregate、localized/array/copy/schema-evolution、领域 CLI 差分与 golden。
- `local-db2-performance`：固定 case manifest；independent-cold/shared-build-cold/warm/repeat-warm；每个物理算子形态至少 10 次有效样本并包含基数/选择率/行宽/排序性/内存预算分层；记录 target、DBD/DB2 SHA-256、命令、输入、输出 hash、logical/physical plan、operator timing、wall/CPU/RSS/alloc、文件读取/复制/spill bytes、visited/decoded/scanned/output rows、batch、worker、fallback、结果稳定性和 candidate tournament。
- `wago-hotfix-offline`：真实 Wago raw HTML/embedded JSON fixture 与 SHA-256，覆盖 Titan/retail、全文 search post-filter、分页漂移/重复/缺页、coverage、status、raw/decoded、协议变化和缓存损坏。
- `wago-hotfix-live`：有界网络 integration，冻结首页面分页 metadata，验证当前 component/props/字段、Build/Titan coverage 和缓存 receipt；动态 total/rows 不作为固定 golden。
- 解耦证明：本地 DB2 套件的 Hotfix provider 调用数为 0；Wago Hotfix 套件的 SQL AST/binder/DB2 planner 调用数为 0。
- 通用验证：fuzz、race、cancel/resource、`go test ./... -count=1`、`go test -race ./... -count=1`、`npm test`、`npm run pack:check`、完整 golden/command matrix。
