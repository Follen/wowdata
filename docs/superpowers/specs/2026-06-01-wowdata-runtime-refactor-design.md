# wowdata Runtime Refactor 设计规格

日期：2026-06-01
分支：`Go-refactor`
状态：待实现

## 背景

`wowdata` 当前同时承担三种用法：

- 本机 CLI：一次性命令，例如 `wowdata db2 rows SpellName --id 1250599`。
- 本机 MCP stdio：由 Codex、Claude Code 或 cc-switch 在本机启动的长进程。
- 远端 MCP HTTP：部署在服务器上，由多个用户或 agent 通过 HTTP 调用。

目前三者共享同一个 `cmd/wowdata.Runtime`，HTTP 的 warmup gate、context cache、artifact URL、health handler 等服务端逻辑已经开始进入通用 runtime 和 MCP command 文件。这种结构可以短期工作，但长期会让 CLI/stdio 被 HTTP 服务端复杂度污染，也会让 HTTP 无法成为真正的数据服务。

本规格要求完成一次最终态架构重构：CLI 和 stdio 保持现有用户语义，HTTP 单独演进为服务端数据平台。实现不能依赖占位空壳，不能以“后续补”作为验收条件；每个阶段必须有测试和可运行验证。

## 目标

1. 将 CLI、MCP stdio、MCP HTTP 三个运行场景解耦。
2. 保持 CLI 和 stdio 的现有命令、工具名、输出语义兼容。
3. 将 HTTP MCP 改造成服务型 runtime，支持 lazy prepare、多 build context pool、持久化 cache、artifact 管理和服务状态。
4. 将查询能力从 Cobra/CLI JSON stdout 包装中抽离，形成可被 CLI、stdio、HTTP 共同调用的 query service。
5. HTTP 持久化层采用 SQLite + Parquet + DuckDB + Go memory + raw CASC cache 的组合。
6. HTTP 配置独立于 CLI/stdio，只在 `wowdata mcp http` 启动时读取。
7. 使用已配置的远端开发服务器验证 HTTP 服务路径，公开服务地址为 `http://211.154.18.253:11223`。远端使用 Docker CE，HTTP MCP 验收部署必须使用 Docker 容器运行。SSH、key、部署细节从 `.local/wowdata/` 本地材料读取，不提交到仓库。

## 非目标

1. 不改变 CLI 的用户命令形态。
2. 不要求 stdio 默认支持多 build 常驻。
3. 不把 Redis、Postgres 或外部队列作为第一版必要依赖。
4. 不把 HTTP 服务配置加载到 CLI/stdio 路径。
5. 不把 `.local/`、服务器密钥、部署私有配置提交到仓库。

## 最终目录结构

目标文件树如下。实现时允许在不破坏职责边界的前提下微调文件名，但不允许重新合并 CLI/stdio/HTTP runtime 边界。

```text
wowdata/
├─ cmd/
│  └─ wowdata/
│     ├─ main.go
│     ├─ cli.go
│     ├─ cli_flags.go
│     ├─ cli_output.go
│     ├─ mcp_stdio.go
│     ├─ mcp_http.go
│     ├─ mcp_tools.go
│     └─ version.go
│
├─ internal/
│  ├─ core/
│  │  ├─ casc/
│  │  │  ├─ source.go
│  │  │  ├─ local.go
│  │  │  ├─ remote.go
│  │  │  ├─ build.go
│  │  │  ├─ root.go
│  │  │  ├─ encoding.go
│  │  │  ├─ download.go
│  │  │  └─ cache.go
│  │  ├─ blte/
│  │  │  ├─ reader.go
│  │  │  ├─ decode.go
│  │  │  └─ keys.go
│  │  ├─ dbd/
│  │  │  ├─ manifest.go
│  │  │  ├─ parser.go
│  │  │  ├─ schema.go
│  │  │  └─ source.go
│  │  ├─ db2/
│  │  │  ├─ reader.go
│  │  │  ├─ wdc.go
│  │  │  ├─ schema.go
│  │  │  ├─ fields.go
│  │  │  └─ values.go
│  │  ├─ listfile/
│  │  │  ├─ listfile.go
│  │  │  ├─ parser.go
│  │  │  └─ source.go
│  │  └─ tact/
│  │     ├─ keyring.go
│  │     └─ source.go
│  │
│  ├─ runtime/
│  │  ├─ context.go
│  │  ├─ context_key.go
│  │  ├─ single_runtime.go
│  │  ├─ local_runtime.go
│  │  ├─ remote_runtime.go
│  │  ├─ warmup.go
│  │  └─ diagnostics.go
│  │
│  ├─ service/
│  │  ├─ http/
│  │  │  ├─ service.go
│  │  │  ├─ config.go
│  │  │  ├─ context_pool.go
│  │  │  ├─ warmup_scheduler.go
│  │  │  ├─ materializer.go
│  │  │  ├─ singleflight.go
│  │  │  ├─ metrics.go
│  │  │  ├─ status.go
│  │  │  └─ artifacts.go
│  │  └─ local/
│  │     ├─ service.go
│  │     └─ warmup.go
│  │
│  ├─ query/
│  │  ├─ db2/
│  │  │  ├─ service.go
│  │  │  ├─ query.go
│  │  │  ├─ schema.go
│  │  │  ├─ rows.go
│  │  │  ├─ search.go
│  │  │  └─ foreign_key.go
│  │  ├─ item/
│  │  │  ├─ service.go
│  │  │  ├─ summary.go
│  │  │  ├─ models.go
│  │  │  ├─ textures.go
│  │  │  ├─ geosets.go
│  │  │  └─ slots.go
│  │  ├─ spell/
│  │  │  ├─ service.go
│  │  │  ├─ info.go
│  │  │  ├─ auras.go
│  │  │  └─ relations.go
│  │  ├─ file/
│  │  │  ├─ service.go
│  │  │  ├─ lookup.go
│  │  │  ├─ search.go
│  │  │  ├─ exists.go
│  │  │  └─ export.go
│  │  ├─ icon/
│  │  │  ├─ service.go
│  │  │  └─ export.go
│  │  ├─ creature/
│  │  │  ├─ service.go
│  │  │  ├─ display.go
│  │  │  └─ model.go
│  │  ├─ encounter/
│  │  │  ├─ service.go
│  │  │  └─ journal.go
│  │  ├─ decor/
│  │  │  ├─ service.go
│  │  │  ├─ list.go
│  │  │  └─ get.go
│  │  └─ video/
│  │     ├─ service.go
│  │     └─ demux.go
│  │
│  ├─ cache/
│  │  ├─ cache.go
│  │  ├─ paths.go
│  │  ├─ raw/
│  │  │  ├─ casc_cache.go
│  │  │  └─ downloader.go
│  │  ├─ metadata/
│  │  │  ├─ sqlite.go
│  │  │  ├─ migrations.go
│  │  │  ├─ builds.go
│  │  │  ├─ schemas.go
│  │  │  ├─ files.go
│  │  │  ├─ listfile.go
│  │  │  └─ materialized.go
│  │  ├─ parquet/
│  │  │  ├─ store.go
│  │  │  ├─ writer.go
│  │  │  ├─ reader.go
│  │  │  └─ schema.go
│  │  ├─ duckdb/
│  │  │  ├─ engine.go
│  │  │  ├─ query.go
│  │  │  └─ parquet.go
│  │  └─ memory/
│  │     ├─ lru.go
│  │     └─ table_cache.go
│  │
│  ├─ mcp/
│  │  ├─ server.go
│  │  ├─ transport_stdio.go
│  │  ├─ transport_http.go
│  │  ├─ registry.go
│  │  ├─ tools.go
│  │  ├─ schemas.go
│  │  ├─ response.go
│  │  └─ resources.go
│  │
│  ├─ adapter/
│  │  ├─ cli/
│  │  │  ├─ commands.go
│  │  │  ├─ warmup.go
│  │  │  ├─ db2.go
│  │  │  ├─ item.go
│  │  │  ├─ spell.go
│  │  │  ├─ file.go
│  │  │  ├─ icon.go
│  │  │  ├─ creature.go
│  │  │  ├─ encounter.go
│  │  │  ├─ decor.go
│  │  │  └─ video.go
│  │  └─ mcp/
│  │     ├─ stdio_tools.go
│  │     ├─ http_tools.go
│  │     ├─ builds.go
│  │     ├─ status.go
│  │     ├─ db2.go
│  │     ├─ item.go
│  │     ├─ spell.go
│  │     ├─ file.go
│  │     ├─ icon.go
│  │     ├─ creature.go
│  │     ├─ encounter.go
│  │     ├─ decor.go
│  │     └─ video.go
│  │
│  ├─ artifact/
│  │  ├─ manager.go
│  │  ├─ paths.go
│  │  ├─ links.go
│  │  └─ cleanup.go
│  │
│  ├─ config/
│  │  ├─ config.go
│  │  ├─ defaults.go
│  │  └─ env.go
│  │
│  └─ diagnostics/
│     ├─ diagnostics.go
│     ├─ status.go
│     └─ metrics.go
│
├─ migrations/
│  └─ sqlite/
│     ├─ 0001_init.sql
│     ├─ 0002_builds.sql
│     ├─ 0003_dbd_schema.sql
│     ├─ 0004_file_index.sql
│     ├─ 0005_listfile_index.sql
│     └─ 0006_materialized_tables.sql
│
├─ config/
│  ├─ http-mcp.example.yaml
│  ├─ stdio.example.json
│  └─ codex.example.json
│
├─ docs/
│  ├─ architecture.md
│  ├─ http-service-runtime.md
│  ├─ cache-layout.md
│  ├─ mcp-tools.md
│  ├─ deployment.md
│  └─ performance.md
│
├─ README.md
├─ CHANGELOG.md
├─ LICENSE
├─ go.mod
└─ go.sum
```

## 三条运行路径

### CLI

CLI 是一次性本机工具。

```text
wowdata db2 rows SpellName --id 1250599
        │
        ▼
cmd/wowdata CLI adapter
        │
        ▼
internal/runtime single-use runtime
        │
        ▼
internal/query service
        │
        ▼
internal/core readers
```

要求：

- CLI 不读取 HTTP 配置文件。
- CLI 不启动 HTTP context pool、scheduler、metrics server 或 artifact manager。
- CLI 可以使用 raw CASC cache 和 SQLite metadata 作为加速，但这些 cache 必须是可选能力；缺失时仍能按现有流程从 CASC/DBD 读取。
- CLI 输出 envelope 保持兼容，现有 golden 和 handler 测试必须继续通过。

### MCP stdio

stdio 是本机 agent 长进程。

```text
Codex / Claude Code / cc-switch
        │ stdio
        ▼
wowdata mcp stdio
        │
        ▼
stdio MCP adapter
        │
        ▼
local service runtime
        │
        ▼
single active context
        │
        ▼
query service
```

要求：

- `wowdata mcp stdio` 命令保持兼容。
- stdio 保留现有 MCP tool 名称，包括 `wow_warmup`。
- stdio 默认单 active context，不启用 HTTP multi-context pool。
- stdio 不读取 `http-mcp.yaml`。
- stdio 导出产物默认返回本机 `file://` URI，不要求 HTTP download URL。

### MCP HTTP

HTTP 是远端多人共享数据服务。

```text
Remote agents / users
        │ HTTP
        ▼
public IP port forwarding
        │
        ▼
wowdata mcp http
        │
        ▼
HTTP MCP adapter
        │
        ▼
HTTP service runtime
        │
        ├─ context pool
        ├─ lazy prepare
        ├─ materialization scheduler
        ├─ request singleflight
        ├─ SQLite metadata
        ├─ Parquet DB2 cache
        ├─ DuckDB query engine
        ├─ artifact manager
        └─ metrics/status
```

要求：

- HTTP 配置通过 `wowdata mcp http --config /path/to/http-mcp.yaml` 读取。
- HTTP 命令行参数覆盖配置文件；环境变量覆盖默认值但低于命令行参数。
- HTTP 普通查询工具不要求用户先调用 warmup。
- HTTP 查询工具必须自动 ensure build context、schema、table materialization 和必要 file index。
- HTTP admin prepare 工具只有在配置 `tools.expose_admin_tools: true` 时暴露。
- HTTP 不再通过 Cobra command 和 JSON stdout 执行业务查询；必须直接调用 query service。

## HTTP MCP 工具设计

HTTP 默认暴露：

```text
wow_builds
wow_status
wow_db2
wow_item
wow_spell
wow_file
wow_icon
wow_creature
wow_encounter
wow_decor
wow_video
```

HTTP 默认不暴露 `wow_warmup`。如果需要主动预热，使用 admin 工具 `wow_prepare`，并且只有在配置中明确开启：

```yaml
tools:
  expose_admin_tools: true
  expose_debug_tools: false
```

所有 HTTP 查询工具接受统一上下文参数：

```json
{
  "region": "cn",
  "product": "wow",
  "locale": "zhCN"
}
```

如果未提供上下文参数，使用 HTTP 配置中的默认值：

```yaml
defaults:
  region: cn
  product: wow
  locale: zhCN
```

## HTTP 配置文件

示例配置提交到 `config/http-mcp.example.yaml`。生产配置放在服务器，例如 `/etc/wowdata/http-mcp.yaml`，不提交私有配置。

```yaml
server:
  host: 127.0.0.1
  port: 9788
  base_url: http://211.154.18.253:11223

defaults:
  region: cn
  product: wow
  locale: zhCN

contexts:
  max_contexts: 4
  pinned:
    - region: cn
      product: wow
      locale: zhCN
      label: CN Retail
    - region: cn
      product: wowt
      locale: zhCN
      label: CN PTR
    - region: cn
      product: wow_classic
      locale: zhCN
      label: CN Classic
    - region: cn
      product: wow_classic_titan
      locale: zhCN
      label: CN Titan

# 默认常驻 context 固定为 CN Retail、CN PTR、CN Classic、CN Titan。
# CN Classic Era 不默认常驻；它和其他 region/product/build 通过 lazy prepare、
# Parquet materialization 和 DuckDB 查询按需服务。

cache:
  root: /opt/wowdata/cache
  metadata_db: /opt/wowdata/cache/metadata.sqlite
  raw_dir: /opt/wowdata/cache/raw
  db2_dir: /opt/wowdata/cache/db2
  duckdb_path: /opt/wowdata/cache/duckdb/wowdata.duckdb

artifacts:
  root: /opt/wowdata/output
  base_url: http://211.154.18.253:11223/files
  retention_hours: 24

prepare:
  lazy: true
  prewarm_on_start: true
  dbd_manifest: true
  listfile: false
  default_tables:
    - SpellName
    - Spell
    - SpellEffect
    - SpellMisc
    - Item
    - ItemSparse
    - ItemEffect
    - ItemModifiedAppearance
    - ItemAppearance
    - ItemDisplayInfo
    - TextureFileData
    - ModelFileData
    - CreatureDisplayInfo
    - CreatureModelData
    - HouseDecor

refresh:
  product_check_interval_minutes: 30
  auto_prepare_new_builds: true
  keep_builds_per_product: 2
  prune_on_start: true
  max_cache_gb: 80

limits:
  max_concurrent_prepares: 1
  max_concurrent_materializations: 2
  max_concurrent_queries: 8
  request_timeout_seconds: 120
  materialize_timeout_seconds: 600

tools:
  expose_admin_tools: false
  expose_debug_tools: false
```

## 缓存架构

HTTP 持久化缓存目录：

```text
cache/
├─ metadata.sqlite
├─ raw/
│  └─ casc/
│     └─ {region}/{product}/{buildKey}/
│        ├─ build_manifest.json
│        ├─ cache_integrity.json
│        ├─ config/
│        ├─ data/
│        └─ indexes/
├─ db2/
│  └─ {region}/{product}/{buildKey}/{locale}/
│     ├─ SpellName.parquet
│     ├─ Item.parquet
│     ├─ ItemSparse.parquet
│     ├─ SpellEffect.parquet
│     ├─ TextureFileData.parquet
│     └─ ModelFileData.parquet
├─ duckdb/
│  └─ wowdata.duckdb
└─ artifacts/
   ├─ icons/
   ├─ files/
   ├─ textures/
   └─ videos/
```

### SQLite

SQLite 保存控制面数据：

- `builds`
- `products`
- `dbd_manifest`
- `db2_schema`
- `file_index`
- `listfile_index`
- `materialized_tables`
- `prepare_jobs`
- `cache_integrity`

SQLite migration 必须放在 `migrations/sqlite/`，启动 HTTP service 时自动迁移。迁移必须幂等，失败时服务启动失败并返回明确错误。

### Parquet

Parquet 保存 decoded DB2 表数据。路径由 `region/product/buildKey/locale/table` 决定。

要求：

- Parquet 文件必须包含 build identity metadata。
- 同一个 table materialization 必须先写临时文件，校验成功后原子替换。
- schema 变化时必须生成新的 materialization 或标记旧缓存失效。
- 不能把整张大表序列化成 JSON 作为持久化格式。

每个 Parquet 文件必须写入并校验以下 metadata：

- `region`
- `product`
- `buildKey`
- `buildName`
- `locale`
- `table`
- `db2FileDataID`
- `dbdDefinitionHash`
- `decoderVersion`
- `materializerVersion`

只要这些指纹中任一值与 SQLite `materialized_tables` 记录不一致，就必须拒绝复用该 Parquet 文件，并重新 materialize。

### DuckDB

DuckDB 作为查询引擎读取 Parquet。要求：

- 查询必须参数化，不能拼接用户输入为 SQL 字符串。
- DuckDB 不作为唯一真相；Parquet 文件和 SQLite metadata 是可恢复状态。
- DuckDB 不可用时，HTTP 服务必须对已支持的简单查询回退到 Go memory/table reader，或者返回明确 `query_engine_unavailable` 错误。

### Go memory

Go memory 保存热路径：

- active build contexts
- hot table LRU
- typed indexes，例如 Item、Spell、FileDataID lookup
- in-flight prepare/materialization singleflight 状态

HTTP 内存上限由配置控制。超过 context 上限时使用 LRU 淘汰；pinned context 不被普通 LRU 淘汰。

## HTTP 更新与缓存失效策略

HTTP 远端服务必须假设 Blizzard CDN、DBD schema、listfile 和本服务的 materializer 都会更新。缓存策略以 `buildKey` 为不可变边界，不能用新数据覆盖旧 build 的缓存。

### buildKey 不可变缓存

所有 raw CASC、Parquet、SQLite materialization 状态都必须包含 `region/product/buildKey/locale`：

```text
cache/raw/casc/{region}/{product}/{buildKey}/data
cache/db2/{region}/{product}/{buildKey}/{locale}/{table}.parquet
```

当同一个 `region/product` 发现新的 `buildKey` 时，服务创建新 build context。旧 build context 继续服务已有请求，直到新 build 准备完成并完成原子切换。

### Build watcher

HTTP service runtime 必须包含 build watcher。它按配置间隔检查 pinned contexts 和默认 context 对应的 product/build：

```yaml
refresh:
  product_check_interval_minutes: 30
  auto_prepare_new_builds: true
```

流程：

```text
poll product list
  │
  ├─ buildKey unchanged: no-op
  └─ buildKey changed:
       ├─ record update candidate in SQLite
       ├─ start background prepare for new build
       ├─ keep old context active
       ├─ if new build prepare succeeds: atomically switch default context
       └─ if prepare fails: keep old context and surface failure in wow_status
```

### 原子切换

新 build 只有在以下条件全部满足后才能成为默认 context：

- CASC metadata loaded。
- DBD manifest/schema ready。
- pinned/default tables 已按配置 prepare 或 materialize。
- `wow_status` 对该 context 显示 `ready`。
- 旧 context 没有被直接覆盖或删除。

切换必须只更新 context pointer / metadata 状态，不得修改旧 build 的 Parquet 或 raw cache。

### DBD、decoder、materializer 失效

DBD 定义变化、decoder 版本变化、materializer 版本变化，即使 buildKey 不变，也必须让对应 table 的 Parquet 失效。失效判断使用以下指纹：

```text
dbdDefinitionHash
decoderVersion
materializerVersion
db2FileDataID
```

当指纹不一致：

- SQLite `materialized_tables` 标记为 `stale`。
- 下一次查询该表时重新 materialize。
- 如果 stale table 当前正在被查询，当前查询继续使用已打开的旧数据；新查询在重新 materialize 后使用新数据。

### Admin refresh tools

HTTP 普通工具不暴露手动刷新。开启 admin tools 后必须提供：

```text
wow_refresh_builds
wow_prepare
wow_prune_cache
```

`wow_refresh_builds` 强制检查 product/build 更新。

`wow_prepare` 支持：

```json
{
  "region": "cn",
  "product": "wow",
  "locale": "zhCN",
  "force": {
    "schema": true,
    "tables": ["ItemSparse", "SpellEffect"]
  }
}
```

`wow_prune_cache` 支持按 build、product、table 和 artifact retention 清理，但必须拒绝删除 active context、pinned context 和 in-flight materialization 正在使用的路径。

### Cache prune

缓存清理由配置控制：

```yaml
refresh:
  keep_builds_per_product: 2
  prune_on_start: true
  max_cache_gb: 80
```

清理顺序：

1. 删除过期 artifacts。
2. 删除非 active、非 pinned、非 in-flight 的旧 Parquet。
3. 删除非 active、非 pinned、非 in-flight 的旧 raw CASC cache。
4. 如果仍超过 `max_cache_gb`，返回 `cache_pressure` 状态并停止自动 materialization，直到管理员清理或提高上限。

清理必须写入 SQLite audit 记录，包含时间、路径、大小、原因和操作者。

## HTTP lazy prepare 流程

HTTP 查询工具执行流程：

```text
request
  │
  ├─ resolve request context: region/product/locale
  ├─ resolve buildKey
  ├─ get or create service context
  ├─ ensure CASC metadata
  ├─ ensure DBD manifest/schema
  ├─ ensure required DB2 tables
  │    ├─ if memory hot: use memory
  │    ├─ else if Parquet exists and valid: load/query Parquet
  │    └─ else: decode from CASC, write Parquet, update SQLite
  ├─ execute query service
  └─ return MCP response
```

并发要求：

- 同一 build/table 的 materialization 必须 singleflight 去重。
- 不同 table 可以在 `max_concurrent_materializations` 限制内并行。
- 同一时间 prepare 数量受 `max_concurrent_prepares` 限制。
- busy 状态返回结构化错误，不能让请求无限挂起。

## Artifact 管理

HTTP artifact manager 负责导出文件、图标、贴图、视频片段的本地路径和公开 URL 映射。

要求：

- 所有导出路径必须限制在 `artifacts.root` 内。
- 返回 MCP `resource_link`，并包含公开 `downloadUrl`。
- 对越界路径拒绝映射。
- 产物包含 `path`、`uri`、`mimeType`、`size`、`sha256`。
- `retention_hours` 到期的产物由清理任务删除。

## 远端开发服务器

HTTP 服务开发和验收使用已配置的远端服务器，公开地址为：

```text
http://211.154.18.253:11223
```

本地部署材料位于 `.local/wowdata/`，其中私有部署脚本包含远端 host、SSH 端口、key 路径、Docker、cache 和 artifact 目录等配置。实现和验证脚本可以读取这些本地材料，但不能提交 `.local/` 内容。

远端使用 Docker CE。HTTP service 的开发验收部署必须使用 Docker，避免直接污染宿主机 Go runtime、动态库和 DuckDB/Parquet 依赖。容器镜像由当前仓库构建，镜像内只包含运行 HTTP MCP 所需的二进制、配置入口、迁移文件和必要证书/CA 依赖；不得把 `.local/`、SSH key 或私有配置打进镜像。

目标容器运行形态：

```text
docker run -d --name wowdata-mcp \
  --restart unless-stopped \
  -p 0.0.0.0:9443:9788 \
  -v /opt/wowdata/config/http-mcp.yaml:/etc/wowdata/http-mcp.yaml:ro \
  -v /opt/wowdata/cache:/var/lib/wowdata/cache \
  -v /opt/wowdata/output:/var/lib/wowdata/artifacts \
  wowdata:http-refactor \
  /usr/local/bin/wowdata mcp http --config /etc/wowdata/http-mcp.yaml
```

公网访问统一使用 `http://211.154.18.253:11223`，服务链路为 `211.154.18.253:11223 -> host 9443 -> Docker 0.0.0.0:9443 -> container 9788`。容器内 HTTP service 监听 `0.0.0.0:9788`，公开入口由服务器端口转发控制。

生产 systemd 不直接执行 `wowdata mcp http`，而是管理容器生命周期。systemd unit 应使用 Docker 启停命令，并保留资源限制和重启策略。

容器内最终启动命令应为配置文件模式：

```text
/usr/local/bin/wowdata mcp http --config /etc/wowdata/http-mcp.yaml
```

命令行覆盖仍允许：

```text
/usr/local/bin/wowdata mcp http \
  --config /etc/wowdata/http-mcp.yaml \
  --max-contexts 4 \
  --base-url http://211.154.18.253:11223
```

Docker 验收要求：

- `docker version` 在远端返回 Docker CE Server 版本。
- 镜像构建脚本使用 Docker multi-stage 构建 Linux amd64 二进制，并构建可运行镜像。
- 容器启动后 `/health` 返回 `ok: true`。
- 容器重启后 SQLite metadata、Parquet DB2 cache、raw CASC cache 和 artifacts 通过 volume 保留。
- 容器日志可通过 `docker logs wowdata-mcp` 查看。
- 更新部署必须支持原子替换：新容器健康检查通过后再移除旧容器。
- 如果容器启动失败，部署脚本保留旧容器并报告失败。

## 迁移策略

实现必须分阶段，但每个阶段都要达到可运行、可测试、可回滚状态。

1. 抽出 query service 接口，让 CLI handler 和 MCP adapter 能直接调用业务服务。
2. 拆出 runtime `Context`，让 CASC、DB2、listfile、services、diagnostics 属于 context，而不是散落在大 `Runtime`。
3. 保持 CLI 和 stdio 走 single active context，并用测试证明行为不变。
4. 建立 HTTP service runtime，不再把 HTTP context pool 放进通用 runtime。
5. 将 HTTP MCP tools 改为直接调用 HTTP service query path，不再通过 Cobra JSON stdout 包装。
6. 增加 HTTP config 文件加载、配置覆盖、示例配置和 systemd/deploy 更新。
7. 增加 SQLite metadata 和 migration。
8. 增加 Parquet materialization。
9. 增加 DuckDB query engine。
10. 将 HTTP 普通 tools 改成 lazy prepare；`wow_prepare` 仅作为 admin 工具。
11. 补齐远端开发服务器验证脚本和性能基准。

## 测试矩阵

### 单元测试

- context key 生成和比较。
- CLI runtime 不读取 HTTP config。
- stdio runtime 不启用 HTTP context pool。
- HTTP config 解析、默认值、命令行覆盖、环境变量覆盖。
- HTTP context pool LRU、pinning、eviction。
- prepare scheduler 并发限制。
- singleflight 同 build/table 去重。
- SQLite migration 幂等。
- Parquet writer 原子写和 schema metadata。
- DuckDB 参数化查询。
- artifact 路径越界拒绝。

### 集成测试

- CLI `warmup/db2/item/file/icon/spell/encounter/creature/decor/video` 现有命令通过。
- stdio `tools/list` 包含现有工具。
- stdio `wow_warmup` 后查询 `wow_db2/wow_file/wow_item` 通过。
- HTTP `wow_db2` 在没有显式 warmup 时自动 lazy prepare。
- HTTP `wow_item` 自动 materialize item 相关表。
- HTTP `wow_icon` 返回公开 download URL。
- HTTP `wow_builds` 返回 CN/US/EU/KR/TW product/build 列表。
- HTTP `wow_status` 返回 context、cache、in-flight jobs、memory 状态。
- HTTP build watcher 发现新 build 后，旧 context 继续服务，新 context 后台 prepare。
- HTTP 新 build prepare 成功后默认 context 原子切换。
- HTTP 新 build prepare 失败时保留旧 context，并在 `wow_status` 暴露失败原因。
- HTTP DBD hash、decoderVersion 或 materializerVersion 变化时，旧 Parquet 被标记 stale，下一次查询重新 materialize。
- HTTP `wow_prune_cache` 不删除 active、pinned 或 in-flight context 使用的缓存。
- HTTP admin force refresh 只刷新指定 schema/table，不影响未指定表。

### 真实数据验证

必须保留并更新现有 `.local/wowdata/auditdb2` 类审计能力，覆盖：

- CN Retail、PTR、Classic、Classic Titan、Classic Era。
- US/EU/KR/TW Retail、PTR、Beta、Classic、Classic Era。
- 每个组合至少验证 manifest table load/schema/rows/file existence 分类。
- Retail 关键业务路径验证：DB2 rows、file encoding、icon export、item slot、item model、spell、encounter、creature、decor。

### 性能基准

必须提供可重复的 benchmark 脚本，至少覆盖：

- HTTP cold query：无 context、无 Parquet。
- HTTP warm query：context 和 Parquet 均存在。
- HTTP repeated query：memory hot。
- stdio warm query。
- CLI single command with auto-warmup。

性能结果输出到 `.local/wowdata/bench-*`，不提交结果文件。

## 验收标准

1. `go test ./... -count=1` 通过。
2. CLI 现有命令和 JSON envelope 兼容。
3. stdio MCP 现有 tool list 和基础 tool call 兼容。
4. HTTP MCP 普通查询不要求 `wow_warmup`。
5. HTTP service runtime 不通过 Cobra JSON stdout 执行业务查询。
6. HTTP 配置只在 `mcp http` 路径读取。
7. SQLite、Parquet、DuckDB 路径有测试覆盖。
8. Artifact download URL 在远端 HTTP 模式可用。
9. 远端开发服务器 `http://211.154.18.253:11223/health`、`/help`、`/mcp` 验证通过。
10. 远端 Docker `20.10.24` 容器部署验证通过，容器重启后持久化 cache 保留。
11. HTTP build watcher、原子切换、stale Parquet、admin force refresh 和 cache prune 测试通过。
12. 真实 region/product/build 审计通过，失败项必须分类为网络、当前 build 不可用、schema 缺失或真实业务 bug，不允许吞错。
13. spec、README、CHANGELOG、部署说明同步更新。

## 禁止事项

- 禁止写空目录或空接口作为“已完成”。
- 禁止留下 `TODO`、`TBD`、`placeholder` 作为验收内容。
- 禁止为了让测试通过而删除现有 CLI/stdio 功能。
- 禁止把 HTTP 配置引入 CLI/stdio 启动路径。
- 禁止把 `.local/`、SSH key、服务器私有配置提交到仓库。
- 禁止使用 JSON 文件作为 DB2 大表的持久化主格式。
- 禁止 HTTP 查询路径继续依赖 Cobra command stdout 作为最终架构。
- 禁止新 build 覆盖旧 build 缓存；必须以 buildKey 隔离。
- 禁止 cache prune 删除 active、pinned 或 in-flight context 使用的数据。

## 后续计划

本规格通过后，下一步使用 writing-plans 技能生成实施计划。实施计划必须按阶段拆分，每个阶段包含明确文件改动、测试命令、远端验证步骤和回滚边界。
