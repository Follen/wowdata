# wowdata

<p align="center">
  <strong>A fast World of Warcraft data toolbox for CLI users and MCP agents.</strong>
</p>

<p align="center">
  <a href="#中文">中文</a> |
  <a href="#english">English</a>
</p>

<p align="center">
  <img alt="Version v0.0.1" src="https://img.shields.io/badge/version-v0.0.1-7c3aed?style=for-the-badge">
  <img alt="CLI Supported" src="https://img.shields.io/badge/CLI-Supported-16a34a?style=for-the-badge">
  <img alt="MCP Supported" src="https://img.shields.io/badge/MCP-Supported-0ea5e9?style=for-the-badge">
  <img alt="Go 1.26.1" src="https://img.shields.io/badge/Go-1.26.1-00ADD8?style=for-the-badge&logo=go&logoColor=white">
  <img alt="Windows" src="https://img.shields.io/badge/Windows-amd64-0078D4?style=for-the-badge&logo=windows&logoColor=white">
  <img alt="macOS" src="https://img.shields.io/badge/macOS-arm64%20%7C%20amd64-111827?style=for-the-badge&logo=apple&logoColor=white">
  <img alt="Linux" src="https://img.shields.io/badge/Linux-amd64%20%7C%20arm64-FCC624?style=for-the-badge&logo=linux&logoColor=black">
  <img alt="License AGPL-3.0-or-later" src="https://img.shields.io/badge/license-AGPL--3.0--or--later-b91c1c?style=for-the-badge">
</p>

<p align="center">
  <code>wow_warmup</code> · <code>wow_casc</code> · <code>wow_db2</code> · <code>wow_file</code> · <code>wow_icon</code> · <code>wow_spell</code> · <code>wow_item</code>
</p>

## 中文

`wowdata` 是一个纯 Go 实现的 World of Warcraft 数据工具箱，同时提供命令行工具、MCP stdio server 和 MCP Streamable HTTP server。它可以从本地客户端或 Blizzard CDN 构建读取 CASC、DB2、listfile、贴图、图标、法术、地下城手册、物品、生物、装饰物、视频和诊断数据。

当前版本：`v0.0.1`

### 亮点

- **CLI Supported**：适合脚本、调试、批量查询和资源导出。
- **MCP Supported**：支持本地 `stdio` 和远端 `http` 两种 MCP 传输，通过 `wow_*` 工具查询 WoW 数据。
- **远端和本地数据源**：支持 Blizzard CDN，也支持本机 WoW 客户端目录。
- **Go 单文件发布**：release 提供 Windows、Linux、macOS 构建产物。
- **强开源保护**：使用 `AGPL-3.0-or-later`，修改、分发或作为网络服务使用时需要继续公开源码。

### 下载

从 GitHub Releases 下载 `v0.0.1`：

- `wowdata-v0.0.1-windows-amd64.exe`
- `wowdata-v0.0.1-linux-amd64`
- `wowdata-v0.0.1-linux-arm64`
- `wowdata-v0.0.1-darwin-amd64`
- `wowdata-v0.0.1-darwin-arm64`

### 快速开始

查看帮助：

```powershell
wowdata --help
```

从远端 CDN 预热 Retail 数据：

```powershell
wowdata warmup --source remote --region cn --product wow
```

从本地客户端预热：

```powershell
wowdata warmup --source local --path "D:\Game\World of Warcraft" --region cn --product wow
```

查询 DB2：

```powershell
wowdata --auto-warmup --source remote --region us --product wow_classic_era --tables=SpellName db2 rows SpellName --id 1
```

导出图标：

```powershell
wowdata --auto-warmup --source remote --region us --product wow_classic_era --listfile=false --dbd-manifest=false --tables= icon export --file-data-id 134400 --format png --output output/icon.png
```

### MCP Server

作为本地 MCP stdio server 运行：

```powershell
wowdata mcp stdio
```

内置别名：

```powershell
wowdata --mcp
```

作为远端 MCP Streamable HTTP server 运行：

```powershell
wowdata mcp http --host 0.0.0.0 --port 9788 --base-url http://211.154.18.253:11223 --artifact-root /var/lib/wowdata/artifacts --artifact-base-url http://211.154.18.253:11223/files --max-contexts 3
```

HTTP endpoint:

- `POST /mcp`：MCP Streamable HTTP JSON-RPC endpoint
- `GET|HEAD /mcp`：健康探测和客户端 reachability 检查
- `GET /health`：服务健康状态
- `GET /help`：面向用户和 agent 的配置指南
- `GET /files/...`：导出图标、贴图和原始文件的静态下载入口

当前公开 HTTP 地址：

- `http://211.154.18.253:11223/mcp`
- `http://211.154.18.253:11223/help`
- `http://211.154.18.253:11223/health`
- `http://211.154.18.253:11223/files/...`

`--base-url` 用于生成运行时返回的公开 endpoint。`--artifact-root` 是容器或主机内的写入目录，`--artifact-base-url` 是用户可访问的公开下载 URL 前缀。HTTP runtime 会内置服务 `/files/...`，导出图标、贴图和原始文件后，MCP 结果会返回可下载地址。

`--max-contexts` 只对远端 HTTP MCP 生效，用来控制最多有多少个已 warmup 的 build context 常驻内存。比如 `--max-contexts 3` 可以同时保留 Retail、Classic 和 Titan；超过上限时会按 LRU 淘汰最久未使用的 context。stdio 和普通 CLI 仍使用单上下文模型。

公网部署链路：

```text
211.154.18.253:11223 -> host 9443 -> Docker 0.0.0.0:9443 -> wowdata container 9788
```

Codex:

```powershell
codex mcp add wowdata --url http://211.154.18.253:11223/mcp
```

cc-switch custom MCP:

```json
{
  "type": "http",
  "url": "http://211.154.18.253:11223/mcp"
}
```

Claude Code:

```powershell
claude mcp add --transport http wowdata http://211.154.18.253:11223/mcp
```

Claude Code 本地 stdio：

```powershell
claude mcp add --transport stdio wowdata -- wowdata mcp stdio
```

导出类工具会返回 `path`、`uri`、`mimeType`、`size` 和 `sha256`。在 MCP 调用里，结果同时包含 `structuredContent` 和 `resource_link`。本地 stdio 默认返回 `file://` URI；远端 HTTP 配置了 artifact 参数后会额外返回 `downloadUrl`，并让 `resource_link.uri` 指向公开 HTTP/HTTPS 下载地址。

### 详细文档

- [架构说明](docs/architecture.md)
- [MCP 工具列表](docs/mcp-tools.md)
- [HTTP runtime](docs/http-service-runtime.md)
- [Docker 部署](docs/deployment.md)
- [缓存布局](docs/cache-layout.md)
- [性能验证](docs/performance.md)

暴露的 MCP 工具：

| Tool | Purpose |
| --- | --- |
| `wow_warmup` | 初始化本地或远端 WoW 数据上下文 |
| `wow_casc` | 查看 CASC 状态、产品和诊断信息 |
| `wow_db2` | 查询 DB2 schema、行、搜索、外键和流式输出 |
| `wow_file` | 查询、搜索、读取和导出 CASC 文件 |
| `wow_icon` | 从 BLP 导出 PNG/WebP 图标 |
| `wow_spell` | 查询法术、光环和召唤关系 |
| `wow_encounter` | 查询 JournalEncounter 数据 |
| `wow_item` | 查询物品、模型、geoset 和贴图 |
| `wow_creature` | 查询生物 display/model 信息 |
| `wow_decor` | 查询装饰物数据 |
| `wow_video` | 处理视频容器数据 |

### 命令组

```text
warmup
db2 schema|rows|search|foreign-key|stream
spell info|auras|summons
encounter get
file lookup|search|extension|get|exists|encoding|export
icon export
casc info|products|diagnose
item get|models|geosets|textures
creature display|model
decor list|get
video demux
mcp stdio
mcp http
```

### 开发构建

```powershell
go test ./... -count=1
go build -o dist/wowdata.exe ./cmd/wowdata
```

### 缓存和输出

`wowdata` 会在本地保存运行所需的缓存，并把导出的资源写入输出目录：

- `cache/`：CASC、DBD、listfile 和 TACT key 缓存。
- `output/`：默认导出目录。

首次预热可能较慢；后续会优先使用本地缓存。

### 许可证

本项目使用 `AGPL-3.0-or-later`。如果你修改、分发，或把修改版作为网络服务提供给用户，你需要按照 AGPL 公开对应源码。

旧 Node 实现保留在 `old-node-version` 分支。

## English

`wowdata` is a pure Go World of Warcraft data toolbox with a CLI, an MCP stdio server, and an MCP Streamable HTTP server. It can query CASC, DB2, listfiles, textures, icons, spells, encounters, items, creatures, decor, video containers, and diagnostics from a local WoW client or Blizzard CDN builds.

Current version: `v0.0.1`

### Highlights

- **CLI Supported**: built for scripts, debugging, bulk queries, and asset export.
- **MCP Supported**: supports local `stdio` and remote `http` MCP transports with `wow_*` tools.
- **Local or remote data**: use a local WoW client path or Blizzard CDN metadata.
- **Go release binaries**: Windows, Linux, and macOS builds are published in releases.
- **Strong copyleft license**: `AGPL-3.0-or-later` keeps distributed and network-served modifications open.

### Download

Download `v0.0.1` from GitHub Releases:

- `wowdata-v0.0.1-windows-amd64.exe`
- `wowdata-v0.0.1-linux-amd64`
- `wowdata-v0.0.1-linux-arm64`
- `wowdata-v0.0.1-darwin-amd64`
- `wowdata-v0.0.1-darwin-arm64`

### Quick Start

Print help:

```bash
wowdata --help
```

Warm a remote Retail build:

```bash
wowdata warmup --source remote --region cn --product wow
```

Warm a local client:

```bash
wowdata warmup --source local --path "/Applications/World of Warcraft/_retail_" --region us --product wow
```

Query DB2:

```bash
wowdata --auto-warmup --source remote --region us --product wow_classic_era --tables=SpellName db2 rows SpellName --id 1
```

Export an icon:

```bash
wowdata --auto-warmup --source remote --region us --product wow_classic_era --listfile=false --dbd-manifest=false --tables= icon export --file-data-id 134400 --format png --output output/icon.png
```

### MCP Server

Run as an MCP stdio server:

```bash
wowdata mcp stdio
```

Built-in alias:

```bash
wowdata --mcp
```

Run as an MCP Streamable HTTP server:

```bash
wowdata mcp http --host 0.0.0.0 --port 9788 --base-url http://211.154.18.253:11223 --artifact-root /var/lib/wowdata/artifacts --artifact-base-url http://211.154.18.253:11223/files --max-contexts 3
```

HTTP endpoints:

- `POST /mcp`: MCP Streamable HTTP JSON-RPC endpoint
- `GET|HEAD /mcp`: health/reachability checks for clients
- `GET /health`: service health
- `GET /help`: setup guide for users and agents
- `GET /files/...`: static downloads for exported icons, textures, and raw files

Current public HTTP URLs:

- `http://211.154.18.253:11223/mcp`
- `http://211.154.18.253:11223/help`
- `http://211.154.18.253:11223/health`
- `http://211.154.18.253:11223/files/...`

`--base-url` generates the public endpoint returned at runtime. `--artifact-root` is the container or host write directory, and `--artifact-base-url` is the public download URL prefix. The HTTP runtime serves `/files/...` directly, so exported icons, textures, and raw files can be downloaded from MCP results.

`--max-contexts` only affects remote HTTP MCP. It controls how many warmed build contexts stay resident in memory. For example, `--max-contexts 3` can keep Retail, Classic, and Titan warm at the same time; extra contexts are evicted by LRU. stdio and normal CLI keep the single-context model.

Public deployment path:

```text
211.154.18.253:11223 -> host 9443 -> Docker 0.0.0.0:9443 -> wowdata container 9788
```

Codex:

```bash
codex mcp add wowdata --url http://211.154.18.253:11223/mcp
```

cc-switch custom MCP:

```json
{
  "type": "http",
  "url": "http://211.154.18.253:11223/mcp"
}
```

Claude Code:

```bash
claude mcp add --transport http wowdata http://211.154.18.253:11223/mcp
```

Claude Code local stdio:

```bash
claude mcp add --transport stdio wowdata -- wowdata mcp stdio
```

Export tools return `path`, `uri`, `mimeType`, `size`, and `sha256`. MCP tool calls also include `structuredContent` and `resource_link`. Local stdio returns `file://` URIs by default; remote HTTP deployments configured with artifact options also return `downloadUrl` and point `resource_link.uri` at the public HTTP/HTTPS download URL.

### Documentation

- [Architecture](docs/architecture.md)
- [MCP tools](docs/mcp-tools.md)
- [HTTP runtime](docs/http-service-runtime.md)
- [Docker deployment](docs/deployment.md)
- [Cache layout](docs/cache-layout.md)
- [Performance verification](docs/performance.md)

Exposed MCP tools:

| Tool | Purpose |
| --- | --- |
| `wow_warmup` | Initialize a local or remote WoW data context |
| `wow_casc` | Inspect CASC state, products, and diagnostics |
| `wow_db2` | Query DB2 schemas, rows, search results, foreign keys, and streams |
| `wow_file` | Lookup, search, read, and export CASC files |
| `wow_icon` | Export PNG/WebP icons from BLP assets |
| `wow_spell` | Inspect spells, auras, and summon relationships |
| `wow_encounter` | Query JournalEncounter data |
| `wow_item` | Query items, models, geosets, and textures |
| `wow_creature` | Query creature display/model data |
| `wow_decor` | Query decor data |
| `wow_video` | Process video container data |

### Command Groups

```text
warmup
db2 schema|rows|search|foreign-key|stream
spell info|auras|summons
encounter get
file lookup|search|extension|get|exists|encoding|export
icon export
casc info|products|diagnose
item get|models|geosets|textures
creature display|model
decor list|get
video demux
mcp stdio
mcp http
```

### Development

```bash
go test ./... -count=1
go build -o dist/wowdata ./cmd/wowdata
```

### Cache And Output

`wowdata` stores runtime caches locally and writes exported assets to an output directory:

- `cache/`: CASC, DBD, listfile, and TACT key caches.
- `output/`: default exported artifacts.

The first warmup can take time; later runs reuse the local cache.

### License

This project is licensed under `AGPL-3.0-or-later`. If you modify, distribute, or run a modified version as a network service, you must provide the corresponding source code under the AGPL.

The previous Node implementation is preserved on the `old-node-version` branch.
