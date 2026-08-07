# sql-effective-db2-hotfix 完整目标规格

# 一、最终架构决定

本 change 同时交付两套独立的只读查询能力，但它们不共享查询语言、AST、binder、logical plan、physical executor 或 row store：

```text
Static DB2 path
CLI/domain command -> wowdata-sql-v1 or typed DB2 query -> SQL binder/planner/executor -> static DB2 rows

Hotfix path
wowdata hotfix ... -> HotfixQuery -> provider adapter/index planner -> Wago/DBCache/Raidbots -> Hotfix records
```

允许共享的基础组件只有：target context、Build/product/region/locale 类型、DBD schema loader、cache primitives、I/O/metrics、统一 envelope 和稳定错误框架。

明确删除原目标中的以下耦合：

- `effective` SQL catalog；
- `hotfix.records` SQL catalog；
- Effective Row Store；
- DB2 logical/physical plan 中的 Hotfix overlay operator；
- 将 Hotfix insert/update/delete 应用到 static DB2 row；
- 现有领域命令的 `--hotfix` 或隐式 Hotfix 行为；
- DB2 与 Hotfix 的同查询 JOIN。

change 名称 `sql-effective-db2-hotfix` 暂时保留用于 Native 状态连续性；名称中的 `effective` 不构成验收要求。

# 二、总体目标

## A. Static DB2 SQL

建立真正可由 CLI 调用的只读 SQL 层，使用户可查询当前 Build 中能够被 DBD 正确绑定和 DB2 reader 解码的静态表/字段，并让现有领域命令逐步收敛到相同静态查询内核。

## B. Independent Hotfix Query

建立独立 Hotfix 查询体系，面向 Hotfix record/history 本身提供多维过滤、最近/最新、稳定分页、raw/decoded payload、coverage 与 provenance。Hotfix 查询负责解释记录，不负责生成应用到 DB2 后的 effective row。

# 三、实现真正的只读 SQL CLI

新增稳定的 SQL 命令，例如：

wowdata sql "<query>"

同时支持：

wowdata sql --file query.sql
wowdata sql --stdin
wowdata sql --param spell_id=100
wowdata sql --format json
wowdata sql --format jsonl
wowdata sql --format csv

SQL 查询必须运行在明确的目标上下文中：

wowdata \
  --source remote \
  --region cn \
  --product wow \
  --build 12.0.7.68974 \
  --locale zhCN \
  sql "SELECT * FROM static.SpellEffect WHERE SpellID = :spell_id" \
  --param spell_id=100

不得把 Scan、Filter、Project、Join 等 Go API 直接包装成伪 SQL；必须存在真实的：

- Lexer；
- Parser；
- SQL AST；
- Semantic Binder；
- Logical Plan；
- Physical Plan；
- Executor。

# 四、SQL 语法范围

实现一个明确版本化的只读 SQL 方言，例如：

wowdata-sql-v1

不要求完整兼容所有 ANSI SQL，但下列语法是本 change 的必要范围。

## 1. SELECT 和 FROM

支持：

SELECT *
FROM SpellEffect;

SELECT SpellID, EffectIndex, Effect, BasePoints
FROM SpellEffect;

标识符与 literal 支持：

- `SELECT`
- `FROM`
- `*`
- `table.*`
- 字段列表
- 表别名
- 字段别名
- `AS`
- quoted identifier
- string、integer、float、boolean、NULL literal
- 表名和字段名大小写规则必须明确且可测试

示例：

SELECT
  se.SpellID,
  se.EffectIndex,
  se.Effect,
  se.BasePoints AS base_points
FROM static.SpellEffect AS se;

## 2. WHERE

支持：

- `=`
- `!=`
- `<>`
- `<`
- `<=`
- `>`
- `>=`
- `AND`
- `OR`
- `NOT`
- 括号
- `IN`
- `NOT IN`
- `BETWEEN`
- `NOT BETWEEN`
- `LIKE`
- `NOT LIKE`
- `IS NULL`
- `IS NOT NULL`

示例：

SELECT *
FROM static.SpellEffect
WHERE SpellID IN (100, 200, 300)
  AND EffectIndex BETWEEN 0 AND 2
  AND BasePoints IS NOT NULL;

必须实现正确的 NULL 三值逻辑，不能把 NULL 静默当成 0、空字符串或 false。

## 3. JOIN

支持：

- `INNER JOIN`
- `JOIN`
- `LEFT JOIN`
- `CROSS JOIN`
- `ON`
- 多表 JOIN
- 表别名
- 字段歧义检查
- join key 类型检查

示例：

SELECT
  se.SpellID,
  sn.Name_lang,
  se.EffectIndex,
  se.Effect,
  se.BasePoints
FROM static.SpellEffect AS se
LEFT JOIN static.SpellName AS sn
  ON sn.ID = se.SpellID
WHERE se.SpellID = 100
ORDER BY se.EffectIndex;

第一阶段可以不实现 RIGHT JOIN 和 FULL JOIN，但必须在 grammar 中返回明确的 unsupported 错误，不能误解析。

不得默认根据相似字段名猜 JOIN。DBD relationship metadata 可以用于验证或优化已明确声明的 `ON` 条件，也可以提供后续简写，但不能在没有证据时自动把 `ID`、`SpellID`、`ParentID` 等字段关联起来。

## 4. DISTINCT、ORDER、LIMIT

支持：

- `DISTINCT`
- `ORDER BY`
- 多字段排序
- `ASC`
- `DESC`
- `NULLS FIRST`
- `NULLS LAST`
- `LIMIT`
- `OFFSET`

示例：

SELECT DISTINCT SpellID
FROM static.SpellEffect
WHERE Effect = 2
ORDER BY SpellID
LIMIT 100 OFFSET 0;

在没有 `ORDER BY` 时，必须明确返回顺序契约。涉及 DB2 原始记录顺序、record ID 顺序或 hash/index 访问时，不得产生不稳定输出。

## 5. 聚合

支持：

- `COUNT(*)`
- `COUNT(expr)`
- `MIN`
- `MAX`
- `SUM`
- `AVG`
- `GROUP BY`
- `HAVING`

示例：

SELECT
  SpellID,
  COUNT(*) AS effect_count,
  MIN(BasePoints) AS min_base,
  MAX(BasePoints) AS max_base
FROM static.SpellEffect
GROUP BY SpellID
HAVING COUNT(*) > 1
ORDER BY effect_count DESC
LIMIT 100;

聚合必须有明确的数值溢出、NULL 和类型提升行为。

## 6. 表达式

至少支持：

- 数值运算：`+ - * / %`
- unary `+ -`
- 字符串拼接或明确的不支持结果
- `CASE WHEN`
- `COALESCE`
- `NULLIF`
- `CAST`
- `LOWER`
- `UPPER`
- `LENGTH`
- 数值和字符串比较
- 参数表达式

示例：

SELECT
  SpellID,
  CASE
    WHEN BasePoints IS NULL THEN 0
    ELSE BasePoints
  END AS normalized_base_points
FROM static.SpellEffect;

不得把 Spell 的领域公式偷偷塞进通用 SQL scalar function。Spell 最终伤害计算应属于独立领域计算层，除非函数名、输入和公式版本被明确公开。

## 7. 参数绑定

支持 named parameter：

SELECT *
FROM static.SpellEffect
WHERE SpellID = :spell_id;

CLI：

wowdata sql \
  --param spell_id=100 \
  "SELECT * FROM static.SpellEffect WHERE SpellID = :spell_id"

参数绑定必须：

- 有类型推断或显式类型；
- 不通过字符串拼接执行；
- 对缺失参数返回明确错误；
- 对重复参数、未知参数和类型不匹配返回稳定错误；
- 在 query plan 中记录参数类型但不泄露敏感值。

## 8. CTE 和子查询

支持：

- `WITH`
- 非递归 CTE
- 标量子查询
- `IN (SELECT ...)`
- `EXISTS`
- `NOT EXISTS`
- derived table

示例：

WITH effects AS (
  SELECT
    SpellID,
    EffectIndex,
    Effect,
    BasePoints
  FROM static.SpellEffect
  WHERE SpellID IN (100, 200)
),
names AS (
  SELECT
    ID AS SpellID,
    Name_lang
  FROM static.SpellName
)
SELECT
  e.SpellID,
  n.Name_lang,
  e.EffectIndex,
  e.Effect,
  e.BasePoints
FROM effects AS e
LEFT JOIN names AS n
  ON n.SpellID = e.SpellID
ORDER BY e.SpellID, e.EffectIndex;

## 9. Recursive CTE

为 Encounter section tree、Spell 引用图和其他层级关系提供受限的：

WITH RECURSIVE

示例语义：

WITH RECURSIVE sections AS (
  SELECT
    ID,
    FirstChildSectionID,
    NextSiblingSectionID,
    SpellID
  FROM static.JournalEncounterSection
  WHERE ID = :first_section_id

  UNION ALL

  SELECT
    child.ID,
    child.FirstChildSectionID,
    child.NextSiblingSectionID,
    child.SpellID
  FROM static.JournalEncounterSection AS child
  JOIN sections AS parent
    ON child.ID = parent.FirstChildSectionID
)
SELECT *
FROM sections;

需要：

- `UNION ALL`
- 最大递归深度；
- cycle detection；
- cancellation；
- 资源预算；
- 确定性遍历；
- 超出预算时的明确错误。

不得允许无界递归占满内存或持续扫描整表。

## 10. EXPLAIN

支持：

EXPLAIN SELECT ...
EXPLAIN ANALYZE SELECT ...

`EXPLAIN` 至少报告：

- Build；
- locale；
- region；
- static catalog；
- 目标表；
- projection；
- predicates；
- join keys；
- logical operators；
- physical operators；
- point lookup / range scan / relationship lookup；
- 是否使用 WDC 原生 record ID；
- 是否使用 WDC relationship map；
- 是否发生全表扫描；
- 估算行数；
- 估算内存；
- worker/pool 预算。

`EXPLAIN ANALYZE` 额外报告：

- 实际访问行数；
- 实际解码行数；
- 实际输出行数；
- 各算子 wall time；
- CPU；
- allocs；
- peak heap；
- DB2 文件读取字节；
- CASC/CDN 请求数和字节；
- cache hit/miss；
- cancellation 和错误状态。

# 五、SQL Catalog 和数据源语义

`wowdata-sql-v1` 只有 `static` DB2 catalog。以下两种形式等价：

```sql
SELECT * FROM SpellEffect WHERE SpellID = 100;
SELECT * FROM static.SpellEffect WHERE SpellID = 100;
```

SQL binder 只绑定当前目标 Build 的 DBD 与静态 DB2/DBC/WDC 文件。SQL 结果必须报告 `catalog=static`、Build/product/locale 与数据来源。SQL parser 对 `effective.<Table>`、`hotfix.records` 或其他 Hotfix catalog 返回稳定的 `sql_unknown_catalog` 或 `sql_unsupported_feature`。

缓存是否存在不改变 SQL 数据来源。执行 `wowdata sql` 或现有领域命令时，Wago、Raidbots、DBCache sidecar 和 Hotfix cache 的调用计数必须为 0。

# 六、DBD、DB2 和 Build-specific schema

DBD 只作为 schema/catalog，不作为数据源。

Binder 必须根据当前 Build 绑定：

- 表是否存在；
- 字段是否存在；
- 字段类型；
- 字段数组长度；
- localized field；
- ID 字段；
- relationship 字段；
- copy table；
- WDC compression；
- section layout；
- sparse record；
- nullable/缺失语义；
- schema version。

同一个 SQL 在不同 Build 上字段不存在时，必须返回类似：

- `unknown_table`
- `unknown_column`
- `column_not_available_for_build`
- `type_mismatch`
- `ambiguous_column`
- `unsupported_schema`

不得：

- 猜字段名；
- 用相邻 Build 的 schema 解析当前 Build；
- 把缺失字段填 0；
- 把无法解析的行静默删除；
- 用 `map[string]interface{}` 全表物化作为默认执行方式。

# 七、Static DB2 Logical Plan 和 Physical Executor

SQL AST 必须先绑定为 logical plan，再生成 physical plan。

Logical operators 至少包括 LogicalScan、LogicalPointLookup、LogicalRelationshipLookup、LogicalFilter、LogicalProject、LogicalJoin、LogicalAggregate、LogicalSort、LogicalLimit、LogicalCTE、LogicalRecursiveCTE 和 LogicalUnionAll。

Physical operators 至少包括：

- 定位与读取：WDCRecordIDLookup、DBCRecordIDLookup、WDCRelationshipLookup、SortedIDLookup、DenseIDLookup、HashIDLookup、SparseOffsetLookup、PositionalLookup、LocalitySortedBatchLookup；
- 扫描与表达式：WDCColumnBatchScan、DBCColumnBatchScan、PredicateKernel、ProjectionKernel、FilterProjectLimitFusion、LateMaterialization；
- JOIN：IndexNestedLoopJoin、HashJoin、SortMergeJoin、受预算约束的 BlockNestedLoopJoin、SemiJoin 和 AntiJoin；
- 聚合：StreamingAggregate、HashAggregate、SortAggregate；
- 排序与集合：TopKHeap、BoundedInMemorySort、ExternalMergeSort、Distinct、UnionAll；
- 控制：LimitOperator、CTEMaterialize/CTEPipeline、RecursiveFrontier、StableReorder、OutputStream。

现有 WDC reader 的 binary-search、dense-index、hash-index、sparse-offset、positional 和 relationship fast path 必须被 planner 作为真实物理能力使用，不能统一降级成通用 scan。批量 ID 必须去重、按 section/offset 提升局部性，再按请求顺序和重复语义稳定重排。

SQL executor 使用 typed column batch 或等价紧凑批次。predicate 编译为类型专用 kernel 并生成 selection vector；projection、filter 和 limit 在语义允许时融合；变宽字符串、数组和低选择率列使用 late materialization。热路径不得为每个中间行创建 `map[string]interface{}`，该表示只允许出现在兼容 adapter、最终 JSON/CSV encoder 或明确的慢速回退边界。

JOIN dispatch：小外表且内表有 record-ID/relationship probe 时使用 IndexNestedLoopJoin；等值 JOIN 默认在较小侧建立紧凑 typed hash table；两侧已有兼容顺序或需要外部 spill 时使用 SortMergeJoin；非等值条件仅允许有严格 cardinality/memory 上限的 BlockNestedLoopJoin。planner 必须记录 build/probe 侧、估算与实际基数、hash/spill bytes 和算法回退原因。

Aggregate/sort dispatch：输入已按 group key 排序时使用 StreamingAggregate；否则在预算内使用 typed HashAggregate，超预算切换 SortAggregate/外部 spill。`ORDER BY ... LIMIT K` 在语义允许时使用 TopKHeap，不得默认完整排序；一般排序使用 BoundedInMemorySort，超预算使用 ExternalMergeSort。spill 文件按查询隔离、内容校验、取消清理和原子生命周期管理。

CTE 默认按引用次数、输入成本、输出基数和内存预算选择 pipeline 或 materialize；`IN/EXISTS/NOT EXISTS` 优先降为 semi/anti join。recursive CTE 使用分层 frontier、cycle/dedup set、深度/行数/内存上限与批量 probe，不允许每轮整表扫描。

DB2 planner 不包含 Hotfix operator，也不依赖 Hotfix provider、coverage 或 cache。

Planner 根据目标 ID 数量、表规模、relationship map、index 类型、section/file shape、projection、predicate selectivity、行宽、排序性、join graph、Build、内存/spill 预算、worker 数和真实 corpus 校准成本选择计划。代价模型必须版本化并在 EXPLAIN 中给出关键估算；只做有界、可解释的候选枚举，不引入优化器搜索空间爆炸。

小查询优先无 goroutine/queue 的串行 fast path；只有估算工作量超过实测调度临界点时才启用 pipeline/table/batch 并行。所有 operator 共享查询级 memory token、worker、file handle 和 spill budget，支持背压、取消与第一个错误传播，禁止算子内部再启动不受控并发。

局部 ID/relationship 查询回退整表扫描时必须有界并在 EXPLAIN 中报告缺失能力、实际扫描/解码行数和回退成本。存在可用原生定位能力的验收 case 中，`GetAllRows` 调用数和 full-scan 次数必须为 0。

# 八、独立 Hotfix 查询体系

## 8.1 Public boundary

Hotfix 使用独立 `HotfixQuery`、validator、source planner、filter executor 和 output encoder。它不接受 SQL 文本，也不构造 SQL AST/DB2 plan。

确认入口：

```text
wowdata hotfix query [filters]
```

查询维度至少覆盖：product、完整 Build、region、locale、table、record ID、push ID、status、source、时间范围、最近/最新、raw/decoded、limit/cursor 和排序。

`--latest` 的确认语义：在完整 product/Build/region/locale 与其余 filters 内确定最大 PushID，返回该 PushID 的整批匹配记录，并按稳定记录键排序。它不是“每个 table/record 各取最后一条”；后者若未来提供，必须使用独立显式模式。

## 8.2 Record model

Hotfix record 至少包含 WagoID、PushID、RecordID、RawStatus、可验证的 OperationLabel、Build、Product、Region/RegionID、Locale、TableName/TableHash、RawData、DecodedFields、CreatedAt、Source、SourceRef、PayloadSHA256 和 CoverageRef。

OperationLabel 仅用于查询展示/过滤；不驱动 DB2 overlay。未知 status 保留 raw 并返回稳定 warning/error。

## 8.3 Provider order

1. 匹配目标的本地 DBCache/managed cache，用于低延迟点查和本地记录查询。
2. Wago remote，作为默认远端历史 provider。
3. Raidbots DBCache，作为约 30 天近期快照补充。
4. 显式配置的 fixture/其他 snapshot source。

Wago 失败、协议变化或 coverage 校验失败时：用户已确认当前/最新查询仅在 Raidbots 存在完全匹配 product/Build/region/locale 快照时回退，并报告 warning/provenance；历史查询返回 `hotfix_coverage_incomplete`。

## 8.4 Wago adapter（2026-08-06 实测）

- `/api/hotfixes` 返回 404；入口为 `GET /hotfixes` HTML/Inertia。
- 使用 `golang.org/x/net/html` tokenizer 定位 `div#app[data-page]`；HTML unescape 后用 `encoding/json` 解码。
- 要求 `component == "Hotfixes"`，数据位于 `props.hotfixes`。
- Laravel 分页字段：current_page、last_page、per_page、total、data、next_page_url。
- record 字段：id、push_id、record_id、status、build、table_name、data、created_at、region_id、locale、search_text。
- data 为 null 或 positional array，可嵌套数组，数字可表现为 JSON string；通过 table_name + 完整 Build DBD 映射。
- search 是全文候选：69137 实测 1,508 条；68943 实测 11,307 条且混入 build 68914。解析后再次精确过滤完整上下文；0 条不单独证明完整 miss。
- 首页面冻结 total/last_page；后续检测漂移、重复、缺页和 next URL 不连续。
- 保存 raw HTML/embedded JSON 和 SHA-256；结构变化返回 hotfix_protocol_changed。
- 去重优先 Wago id，同时保存 `(push_id, record_id, build, table_name, region_id, locale, status)`。
- PushID 是稳定顺序；created_at 是 Wago 检测/写入时间。

## 8.5 Titan 与历史 coverage

- Wago `/api/builds` 当前包含 28 个 `wow_classic_titan` Build。
- 最新实测 3.80.2.69137（2026-08-05 22:09:07），本机 3.80.2.68943 也在清单中。
- 存在 build 69137、region 196、zhCN、表 SpellPowerDifficulty 的 Hotfix 样本。
- 当前集合 total=17,002,631、last_page=680,106；最老页样本到 2024-12-30/build 57212/push 90134。
- coverage metadata 保存起点、页范围、抓取时间、冻结 total/last_page 和 completeness，不宣称所有版本全历史。

# 九、DBCache 磁盘查询

DBCache V1-V9 reader 验证 magic、version、Build、扩展头、每条 record magic、固定头、DataSize、offset 和 EOF。V9 是 44-byte file header，后续每条为 32-byte header + payload。

原始 DBCache 是 source of truth。sidecar 按 `(context, tableHash, recordID, pushID)` 排序，只保存固定 metadata、raw status/operation、payload offset 与 length；每条不超过 40 bytes 加固定头。sidecar 使用二分、分页读取或只读 mmap，不复制为全量 Go map。

查询只对命中 offset 执行 ReadAt，并按 DBD 投影解码。单个 DBCache 是环境快照而非全历史 archive；miss 只在该快照 coverage 内成立。

本机 retail 探针：1,399,642 bytes、12,171 records；32-byte/record sidecar 389,472 bytes。完整机器证据位于 `analyze/sql-effective-db2-hotfix-bootstrap/`。

# 十、缓存与完整性

Hotfix cache identity 至少绑定 provider、protocol/parser version、完整 version、product、Build、region、locale、table、page/cursor、search/filters、response SHA-256、冻结 total/last_page、coverage 起点/范围/时间/completeness。

缓存保存 raw response/payload、分页 metadata、coverage、固定宽度 sidecar 和小型索引；不持久化完整 decoded history 副本，不保存 DB2/Hotfix 合并表。

构建采用临时文件、完整校验、原子发布；源文件或 response identity 变化时失效。repeat-warm 不重复下载未变化页面，duplicate payload bytes=0。

# 十一、现有 CLI 与 Static SQL 迁移

现有 db2、spell、encounter、item、creature、decor、file、icon 命令迁移到同一 static SQL/Query Engine。外部命令名、参数、输出和领域格式化保持不变；内部构造 typed SQL AST 或同构 logical-plan API，不要求把 SQL 字符串拼出来再经过 lexer/parser。

正确路径：CLI/domain command → typed static query/SQL AST → DB2 binder → DB2 logical planner → DB2 physical executor → static rows → 原领域 formatter。

`wowdata sql` 与固定命令对等价查询必须产生等价 binder/planner/executor 语义和可比较 EXPLAIN 证据。固定命令不得保留私有 GetAllRows、业务层整表过滤或手写 map join 快路径。它们不访问 Hotfix query system，不接受 `--hotfix`。

# 十二、Spell 领域数据

Spell 查询按当前 Build DBD 查询 static Spell、SpellName、SpellEffect、SpellMisc、SpellScaling、SpellCastTimes、SpellDuration、SpellRange、SpellPower、SpellAuraOptions 等实际存在表。

区分 static DB2 value、derived/calculated value 与 tooltip/display value。若最终伤害需要玩家等级、法强、天赋、装备、光环、难度、目标状态或运行时公式，报告缺失上下文，不从 Hotfix query 或展示值猜测 最终 damage。

# 十三、输出格式

SQL 与 Hotfix 都支持 JSON、JSONL、CSV，但使用不同 schema：

- SQL：dialect、normalized query、target、catalog=static、columns、rows、metrics、plan。
- Hotfix：query filters、source/provider、coverage、records、raw/decoded mode、pagination、warnings、metrics、provenance。

大结果流式输出。Hotfix 输出不嵌入 static DB2 row；SQL 输出不嵌入 Hotfix provenance。

# 十四、错误契约

SQL 错误：sql_lex_error、sql_parse_error、sql_unsupported_statement、sql_unsupported_feature、sql_unknown_catalog、sql_unknown_table、sql_unknown_column、sql_ambiguous_column、sql_type_mismatch、sql_missing_parameter、sql_invalid_join、sql_recursive_limit、sql_memory_limit、sql_cancelled、schema_not_available。

Hotfix 错误：hotfix_invalid_query、hotfix_unavailable、hotfix_build_unavailable、hotfix_protocol_changed、hotfix_unknown_status、hotfix_schema_mismatch、hotfix_build_mismatch、hotfix_locale_mismatch、hotfix_corrupt_cache、hotfix_coverage_incomplete、hotfix_cancelled、hotfix_memory_limit。

错误包含稳定 code、用户可读 message、相关参数/字段、Build/context、source/coverage 和操作建议。

# 十五、兼容性

现有 CLI 命令名、参数、JSON 字段、退出码、导出文件、hash 和 golden 保持 static 行为。新增 Hotfix 查询是独立命令，不要求现有调用方迁移，也不改变现有默认数据来源。

# 十六、明确非目标

- DB2↔Hotfix overlay、effective row/catalog、同查询 JOIN。
- Hotfix SQL catalog、现有领域命令 `--hotfix`。
- 修改 DB2/DBCache/Hotfix 数据。
- 事务、DDL、数据库 server、常驻 HTTP 服务、完整 ANSI SQL。
- 把 Wago 或单个 DBCache 描述为 Blizzard canonical 全历史。
- 持久化所有 decoded Hotfix history 或完整 effective table。
- 缺少运行时上下文时猜测技能最终伤害。

# 十七、测试与验收

## SQL

覆盖 lexer/parser/AST/binder/planner/executor、JOIN/aggregate/CTE/recursive CTE、参数、错误位置、fuzz、race、取消、资源限制、point/batch/relationship lookup、projection/filter pushdown、static corpus 差分和现有 CLI golden。

## Hotfix

覆盖独立 query validation/planner、Wago HTML entity/data-page、component/props/field 变化、分页漂移/重复/缺页、search 假阳性与 post-filter、Titan、positional/nested/string-number data、raw status、DBD decode、coverage、Wago→Raidbots fallback、DBCache V1-V9、sidecar、缓存损坏、网络、race/cancel/resource。

## 本地 DB2 功能测试集

从本机可发现的 WoW 安装创建版本化 corpus manifest，至少记录 product、完整 Build、region、locale、源路径逻辑标识、DBD manifest/definition SHA-256、DB2 file identity/SHA-256、table、case ID、输入与预期输出 hash。当前可发现产品包括 retail、anniversary、classic、classic-era、classic-titan；实际纳入项由 discovery receipt 冻结，不把机器绝对路径写入 portable golden。

功能 case 至少覆盖：

- schema/table/field presence 与 Build-specific evolution；
- record-ID point lookup、batch ID、relationship map；
- projection/filter pushdown、bounded scan；
- localized、array、copy table、sparse/section；
- JOIN、aggregate、ORDER/LIMIT、CTE/recursive CTE；
- db2/spell/encounter/item/creature/decor/file/icon 领域命令差分；
- malformed/missing schema 与稳定错误。

## 本地 DB2 性能测试集

固定 case manifest 至少包含 point lookup、batch、relationship、projection、selective scan、bounded full scan、JOIN、semi/anti join、aggregate、DISTINCT、Top-K、bounded/external sort、CTE/recursive frontier、JSONL streaming 和领域复合查询。每个 case 运行 independent-cold、shared-build-cold、warm、repeat-warm。

每种物理形态按表规模、请求 ID 数、选择率、投影列数/行宽、join 两侧基数、key 分布/skew、已有顺序和内存预算分层。每个固定 cell 至少产生 10 次有效样本；校准报告记录时钟/进程启动噪声、MAD/置信区间或等价稳健界限，单次极值不单独决定 dispatch。

对同一逻辑查询生成所有复杂度或内存边界实质不同且正确的合理候选，执行 tournament，并在未参与阈值拟合的 holdout 表/Build 上验证 dispatch。候选结果 hash、行序和错误必须完全等价；统计持平时选择 peak memory/alloc/read bytes 更低、p95 更稳、实现更简单者。

每次记录：binary/version、机器/OS/CPU/RAM、target、DBD/DB2 hashes、完整命令与输入、logical/physical plan、cost-model version、逐 operator wall/CPU、peak live bytes/RSS、allocs/alloc bytes、batch/worker、文件读取/复制/spill bytes、CASC/CDN 请求、visited/predicate/decoded/scanned/output rows、decoded fields、hash entries、fallback、stdout/stderr/result hash 和 exit status。性能阈值按 case baseline、噪声校准与明确预算判断，不把一次本机毫秒值直接硬编码为跨机器阈值。

## Wago Hotfix 测试集

测试集分为：

1. `wago-hotfix-offline`：从真实 Wago `/hotfixes` 保存 raw HTML 或 embedded data-page JSON、request URL/filters、完整 response SHA-256、抓取时间、冻结 total/last_page、coverage metadata 和预期精确 post-filter 结果。
2. `wago-hotfix-live`：有界网络 probe，只验证当前协议、component/props/字段、分页连续性、Build/Titan coverage、缓存 identity 和 receipt；动态 total、last_page、具体 records 不作为永久 golden。

离线 fixture 至少覆盖 retail 与 Titan、build 69137/region 196/zhCN、search=68943 假阳性、null data、positional/nested array、numeric JSON string、raw status、unknown status、第一页/中间页/最后页、pagination drift、duplicate/missing page、protocol changed、cache corruption、coverage incomplete、Wago→Raidbots current/latest fallback。

Hotfix 性能记录 request/page 数、下载/缓存字节、candidate/post-filter rows、decode rows、peak RSS、sidecar/mmap reads、raw payload ReadAt bytes、cold/warm/repeat-warm 与 cancellation。

## 解耦

- SQL 测试注入 Hotfix provider spy，调用数必须为 0。
- Hotfix 测试注入 SQL/DB2 planner spy，调用数必须为 0。
- 依赖图测试禁止 SQL packages import Hotfix provider/query packages，禁止 Hotfix query packages import SQL AST/planner/executor packages。

## CLI

覆盖 `wowdata sql` 全输入/格式/EXPLAIN；`wowdata hotfix query` 覆盖全部过滤、latest/history、raw/decoded、分页和 provider 行为。

## 回归

运行 `go test ./... -count=1`、`go test -race ./... -count=1`、相关 fuzz、`npm test`、`npm run pack:check`、完整 golden manifest、全 command matrix、independent-cold/shared-build-cold/warm/repeat-warm。

# 十八、性能验收

## Static SQL

### 算子级硬约束

- record/relationship point 与 batch lookup 在原生定位可用时，`GetAllRows=0`、full-scan=0；物理解码量与唯一命中/必要 copy source 相关，不与整表行数相关。
- scan 必须只解码 predicate 所需列，命中后再 late-materialize 输出列；`LIMIT`、Top-K 和 streaming 能提前停止时不得继续扫描/物化无用结果。
- JOIN 必须记录算法、build/probe 侧、估算/实际输入输出、probe 数、hash/排序/spill bytes、decoded rows、peak memory 和 fallback；禁止默认把两侧完整物化为通用 map rows。
- aggregate/distinct/sort 必须有 typed state、内存上限和 spill；Top-K case 不执行完整排序，已排序 group 输入不构建无必要 hash table。
- JSONL/CSV 输出以有界 batch 和背压流式编码；首批输出不等待全结果物化，取消后 operator、goroutine、reader 和 spill 文件全部回收。
- SQL 执行 Hotfix network/cache/DBCache reads=0。

### 极致优化验证协议

所有性能结论来自 `local-db2-performance` 固定 case manifest。每个查询形态比较全部合理正确候选，并保存 raw result、normalized comparison、环境差异、p50/p95/max、CPU、RSS/alloc、I/O/decode、结果 hash 和 dispatch 依据；在 holdout 表/Build 上复核阈值没有只拟合训练 corpus。

当前已归档实现只提供 point/batch/relationship/projected-scan/stream fast path，且 DB2 corpus 峰值 heap 约 3.10 GB、旧 DB2 CLI case 峰值约 0.55-0.67 GB；这些数值作为问题基线和优化前证据，不作为新 SQL 跨机器绝对上限。新引擎必须证明热路径不再按全行 map/整表规模增长，并在查询预算内稳定运行。

默认查询内存预算沿用已归档本地性能契约：point 查询 `min(max(384 MiB, availableRAM × 5%), 1 GiB)`，复合 SQL/领域查询 `min(max(768 MiB, availableRAM × 8%), 2 GiB)`，corpus/benchmark `min(max(1 GiB, availableRAM × 15%), 4 GiB)`；显式用户预算可覆盖，但不得超过进程/平台可用资源。operator 必须先缩 batch、关闭非必要并发或 spill，再返回稳定 `sql_memory_limit`，不得依赖 OOM。

功能正确性、稳定顺序、错误语义、Build/schema 隔离与取消/资源安全先于性能排名。用户已确认采用严格门禁：所有固定 case 必须保持 100% 结果等价；p95、peak RSS、alloc bytes、读取 bytes 或 decoded/scanned rows 出现统计噪声外退化即阻塞，不能用总体平均提速掩盖单个 case；每种查询形态选择全部正确候选中的 Pareto 最优算子。

## Hotfix

DBCache 点查只读取 sidecar 页和命中 payload；不把 raw 文件、全部 payload、全部 records 或 sidecar entries复制到 heap。Wago 点查使用 search 作候选并精确 post-filter，不为单个 record 默认遍历 680k 页；coverage 不足时返回 coverage 状态。

Wago adapter 的功能与性能结论来自 `wago-hotfix-offline` 固定真实响应 corpus；`wago-hotfix-live` 仅验证当前兼容性与真实网络指标。

repeat-warm：未变化页面/DBCache 不重复下载，raw cache delta=0，duplicate payload bytes=0，查询内存受 limit/cursor 约束。

# 十九、最终产物

- Native brief 与完整目标 spec。
- wowdata-sql-v1 grammar、Lexer、Parser、AST、static Binder/Catalog/Planner/Executor、EXPLAIN/ANALYZE。
- 独立 HotfixQuery model、validator、source planner、filter executor、CLI、output schema。
- Wago adapter、DBCache disk reader/sidecar、Raidbots snapshot adapter、fixtures、coverage/cache/provenance。
- `local-db2-functional` corpus manifest/golden、`local-db2-performance` case manifest/report。
- `wago-hotfix-offline` raw fixture/SHA-256/expected manifest、`wago-hotfix-live` runner/receipt/report。
- 现有 CLI static migration、golden、command matrix、performance report、verification.md、复现命令、Wago raw response SHA-256 manifest。

# 二十、Comet 工作流要求

开始前先读取：

- 当前 Native status；
- active changes；
- 当前 selection；
- 当前 end-to-end-performance change；
- 当前工作区未提交修改；
- 现有 internal DB2 engine；
- 现有 DBD/DB2/CASC loader；
- 现有领域服务；
- 现有 golden 和 benchmark。

这个目标应作为独立 change 管理。

不要把它静默并入已有 `end-to-end-performance` change，除非：

1. Runtime 和磁盘事实证明两个目标已经属于同一份完整规格；
2. 不会破坏已有 performance change 的 scope、baseline、verification evidence 和 archive 边界；
3. 用户明确确认合并。

如果已有性能 change 正在 Build/Verify，应优先保持其工作区和运行进程不受影响，为 SQL/Hotfix change 建立独立规格、scope、证据和回滚边界。

先完成 Shape，完整调查现有架构、SQL 语法边界、Hotfix provider 协议和兼容性决定；任何会改变 DB2 SQL 与独立 Hotfix 查询边界、默认 provider 或用户可见输出的分支必须询问用户。

# 已确认的 Shape 决定

- DB2 SQL 与 Hotfix 查询解耦。
- SQL 只有 static catalog。
- Hotfix 是独立查询体系，只查询/解释 Hotfix records，不应用到 DB2 rows。
- 现有领域命令保持 static，不新增 `--hotfix`。
- Wago 是默认远端 provider；Raidbots 是近期快照补充。
- 当前/最新 Hotfix 查询在完全匹配时可回退 Raidbots；历史查询保持 Wago coverage 语义。
- DBCache 使用磁盘 reader + sidecar，不全量常驻 payload。
- 必须交付本地真实 DB2 功能/性能测试集和真实 Wago 响应 Hotfix 测试集；二者分别验证各自查询体系。
- 独立 Hotfix CLI 固定为 `wowdata hotfix query`。
- 现有固定领域命令内部迁移到 static DB2 SQL AST/logical-plan 与统一 binder/planner/executor，外部兼容契约不变。
- `wowdata hotfix query --latest` 返回完整上下文与 filters 内最大 PushID 的整批记录。
- SQL 物理层必须以 typed/vectorized batch、late materialization、算子融合、成本 dispatch、spill 和真实 corpus candidate tournament 实现“优化到极致”，并逐算子证明延迟、内存、I/O 和解码工作量。
- 性能验收采用逐 case 噪声外零回退与 Pareto 最优门禁；用户已确认当前完整 Shape 合同并要求直接进入 Build。
