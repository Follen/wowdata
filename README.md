# wowdata

<div align="center">
  <strong>World of Warcraft data, from the command line.</strong>
  <br />
  <sub>Query CASC and DB2 data, export game assets, and keep every result scriptable.</sub>
  <br /><br />

  [![npm](https://img.shields.io/npm/v/@follenfang/wowdata?style=flat-square&logo=npm&label=npm)](https://www.npmjs.com/package/@follenfang/wowdata)
  [![downloads](https://img.shields.io/npm/dm/@follenfang/wowdata?style=flat-square&label=downloads)](https://www.npmjs.com/package/@follenfang/wowdata)
  [![CI](https://img.shields.io/github/actions/workflow/status/Follen/wowdata/ci.yml?branch=main&style=flat-square&logo=github&label=CI)](https://github.com/Follen/wowdata/actions/workflows/ci.yml)
  [![release](https://img.shields.io/github/v/release/Follen/wowdata?style=flat-square&logo=github&label=release)](https://github.com/Follen/wowdata/releases/latest)
  [![license](https://img.shields.io/github/license/Follen/wowdata?style=flat-square&label=license)](LICENSE)

  <br />
  <a href="#readme-zh">中文</a> · <a href="#readme-en">English</a>
</div>

---

<a id="readme-zh"></a>

## 中文

`wowdata` 是一个 CLI-only 的魔兽世界数据工具。它直接读取本地客户端或 Blizzard CDN，查询 CASC、DB2、Listfile、法术、物品、生物和地下城手册数据，也能导出 BLP、PNG、WebP 与其他游戏文件。

核心解析器使用 Go。npm 只负责跨平台安装、更新和 Skill 部署，没有隐藏的远端服务，也不需要 MCP。

### 为什么用它

- **直接查询**：执行目标命令即可，CLI 会自行判断缓存是否可用、是否需要下载。
- **结果稳定**：最终结果写入 stdout JSON，下载进度写入 stderr，适合 Shell 和 Agent。
- **缓存可控**：按 Build 保存不可变快照，支持校验、清理、容量限制和离线复用。
- **源码可审计**：数据解析在 Go 中完成，npm 生命周期脚本集中在 `npm/`。
- **原子命令**：CLI 只提供明确的小命令，语义理解和命令组合交给 Skill。

### 安装

```bash
npm install -g @follenfang/wowdata
```

支持 Windows amd64、Linux amd64/arm64、macOS amd64/arm64。安装完成后：

```bash
wowdata --version
wowdata doctor
wowdata --help
```

npm 会安装：

- `wowdata` CLI 到 `~/.wowdata/bin`，并创建全局命令。
- `wowdata` Skill 到 `~/.agents/skills/wowdata`。

也可以从 [GitHub Releases](https://github.com/Follen/wowdata/releases/latest) 直接下载单文件程序。

### 快速开始

CLI 不猜地区、产品、Build 或语言。涉及游戏数据时，目标必须明确：

```bash
wowdata db2 rows SpellName --id 123 \
  --source remote \
  --region cn \
  --product wow \
  --build latest \
  --locale zhCN
```

常用目标可以保存成 Profile：

```bash
wowdata profile set retail-cn \
  --source remote \
  --region cn \
  --product wow \
  --build latest \
  --locale zhCN

wowdata spell info --spell-id 123 --profile retail-cn
```

查看某个区域当前实际提供的产品和 Build：

```bash
wowdata casc products --source remote --region cn
```

### 不需要手动预热

直接执行查询即可：

```bash
wowdata file lookup --file-data-id 134400 --profile retail-cn
wowdata icon export --file-data-id 134400 --format png \
  --output output/icon.png --profile retail-cn
```

每次查询时，CLI 会依次完成：

1. 解析产品与 Build。
2. 对比远端内容身份和本地 SHA-256。
3. 复用有效缓存，下载缺失或过期内容。
4. 执行查询并返回最终 JSON。

只有需要提前准备数据时，才显式运行：

```bash
wowdata warmup --source remote --region cn \
  --product wow --build latest --locale zhCN
```

独立 CDN 对象和清单默认使用 4 个 worker 并发下载。

### 命令一览

| 领域 | 命令 |
| --- | --- |
| CASC | `casc info/products/diagnose` |
| DB2 | `db2 schema/rows/search/foreign-key/stream` |
| 文件 | `file lookup/search/extension/get/exists/encoding/export` |
| 图标 | `icon export` |
| 法术 | `spell info/auras/summons` |
| 副本 | `encounter get` |
| 物品 | `item get/models/geosets/textures` |
| 生物 | `creature display/model` |
| 装饰 | `decor list/get` |
| 视频 | `video demux` |
| 目标 | `profile list/show/set/remove` |
| 缓存 | `cache status/verify/prune/clear/config` |
| 维护 | `doctor/update/uninstall` |

运行 `wowdata <command> --help` 查看参数。

### 输出约定

```json
{
  "ok": true,
  "command": "icon export",
  "data": {},
  "warnings": []
}
```

- 成功：stdout 返回 `"ok": true`，退出码为 `0`。
- 业务错误：stdout 返回 `"ok": false` 和结构化错误，退出码为 `1`。
- 下载、缓存准备等进度只写入 stderr。
- 参数解析错误写入 stderr，退出码为 `1`。

普通 Shell 可以直接同时使用退出码和 JSON：

```bash
if wowdata db2 rows SpellName --id 133 \
  --source remote --region cn --product wow --build latest --locale zhCN \
  >result.json; then
  jq '.data.rows' result.json
else
  jq '.error' result.json
fi
```

### 缓存与本地目录

```text
~/.wowdata/
├── bin/       CLI 二进制
├── config/    用户配置
├── profiles/  完整数据目标
├── builds/    不可变 Build 快照
├── cache/     CASC、DBD、Listfile、TACT 与清单缓存
├── state/     安装和最近状态
├── tmp/       下载临时文件
└── locks/     跨进程下载锁
```

缓存默认上限为 20 GB。当前 Build 和上一个 Build 不参与自动清理，更早的 Build 按最久未使用顺序清理。

```bash
wowdata cache status
wowdata cache verify
wowdata cache prune
wowdata cache clear
wowdata cache config --max-gb 30 --workers 6
```

远端不可用时，CLI 会复用已经通过完整性校验的缓存；损坏或未完成的内容不会被使用。

### Skill 的默认规则

CLI 本身没有目标默认值。随 npm 安装的 Skill 会在用户未指定时明确传入：

- 地区：`cn`
- Build：`latest`
- 语言：`zhCN`

产品没有默认值。无法从问题或上下文确定产品时，Skill 会先询问。

### 维护

```bash
wowdata doctor
wowdata update
wowdata update --version 0.0.2
wowdata uninstall
wowdata uninstall --keep-data
```

`doctor` 是只读检查。`uninstall --keep-data` 会删除程序和托管 Skill，但保留 Profile、Build 与缓存。

### 从源码构建

```bash
go test ./... -count=1
go build -trimpath -o dist/wowdata ./cmd/wowdata
npm test
```

推送 `vX.Y.Z` tag 后，GitHub Actions 会运行测试、构建五个平台、生成 `SHA256SUMS`、创建 GitHub Release，并通过 npm Trusted Publisher OIDC 发布带 provenance 的包。

---

<a id="readme-en"></a>

## English

`wowdata` is a CLI-only toolkit for World of Warcraft data. It reads a local game installation or the Blizzard CDN to query CASC, DB2, listfiles, spells, items, creatures, and encounter data, and exports BLP, PNG, WebP, and other game files.

The parser is written in Go. npm is only used for cross-platform installation, updates, and Skill deployment. There is no hosted service and no MCP runtime.

### Why wowdata

- **Query directly**: run the command you need; the CLI decides whether cached data can be reused or downloaded.
- **Stable output**: final results go to stdout as JSON, while preparation progress stays on stderr.
- **Controlled cache**: immutable Build snapshots with verification, pruning, size limits, and offline reuse.
- **Auditable source**: data parsing lives in Go, and every npm lifecycle script is visible under `npm/`.
- **Atomic commands**: the CLI exposes small operations; the Skill handles intent and command composition.

### Install

```bash
npm install -g @follenfang/wowdata
```

Supported targets: Windows amd64, Linux amd64/arm64, and macOS amd64/arm64.

```bash
wowdata --version
wowdata doctor
wowdata --help
```

The npm package installs:

- The `wowdata` CLI under `~/.wowdata/bin`, with a global command.
- The `wowdata` Skill under `~/.agents/skills/wowdata`.

Standalone binaries are also available from [GitHub Releases](https://github.com/Follen/wowdata/releases/latest).

### Quick start

The CLI does not guess a region, product, Build, or locale. Data commands require an explicit target:

```bash
wowdata db2 rows SpellName --id 123 \
  --source remote \
  --region cn \
  --product wow \
  --build latest \
  --locale zhCN
```

Save frequently used targets as Profiles:

```bash
wowdata profile set retail-cn \
  --source remote \
  --region cn \
  --product wow \
  --build latest \
  --locale zhCN

wowdata spell info --spell-id 123 --profile retail-cn
```

Discover the products and Builds currently available in a region:

```bash
wowdata casc products --source remote --region cn
```

### Warmup is optional

Run the query directly:

```bash
wowdata file lookup --file-data-id 134400 --profile retail-cn
wowdata icon export --file-data-id 134400 --format png \
  --output output/icon.png --profile retail-cn
```

For each query, the CLI:

1. Resolves the product and Build.
2. Compares the remote content identity with the local SHA-256.
3. Reuses valid cache entries and downloads missing or stale data.
4. Executes the query and returns the final JSON result.

Use `warmup` only when data must be prepared ahead of time:

```bash
wowdata warmup --source remote --region cn \
  --product wow --build latest --locale zhCN
```

Independent CDN objects and manifests use four download workers by default.

### Command map

| Area | Commands |
| --- | --- |
| CASC | `casc info/products/diagnose` |
| DB2 | `db2 schema/rows/search/foreign-key/stream` |
| Files | `file lookup/search/extension/get/exists/encoding/export` |
| Icons | `icon export` |
| Spells | `spell info/auras/summons` |
| Encounters | `encounter get` |
| Items | `item get/models/geosets/textures` |
| Creatures | `creature display/model` |
| Decor | `decor list/get` |
| Video | `video demux` |
| Targets | `profile list/show/set/remove` |
| Cache | `cache status/verify/prune/clear/config` |
| Maintenance | `doctor/update/uninstall` |

Run `wowdata <command> --help` for complete flags.

### Output contract

```json
{
  "ok": true,
  "command": "icon export",
  "data": {},
  "warnings": []
}
```

- Success: stdout contains `"ok": true`; exit code `0`.
- Domain error: stdout contains `"ok": false` and a structured error; exit code `1`.
- Download and cache preparation progress is written only to stderr.
- Argument parsing errors are written to stderr and return exit code `1`.

Shell scripts can rely on both the process status and JSON body:

```bash
if wowdata db2 rows SpellName --id 133 \
  --source remote --region cn --product wow --build latest --locale zhCN \
  >result.json; then
  jq '.data.rows' result.json
else
  jq '.error' result.json
fi
```

### Cache layout

```text
~/.wowdata/
├── bin/       CLI binaries
├── config/    User configuration
├── profiles/  Complete data targets
├── builds/    Immutable Build snapshots
├── cache/     CASC, DBD, listfile, TACT, and manifest cache
├── state/     Installation and recent state
├── tmp/       Incomplete downloads
└── locks/     Cross-process download locks
```

The default cache limit is 20 GB. The current and previous Build for each Profile are protected from automatic pruning; older Builds are removed by least-recent use.

```bash
wowdata cache status
wowdata cache verify
wowdata cache prune
wowdata cache clear
wowdata cache config --max-gb 30 --workers 6
```

When the remote source is unavailable, the CLI can reuse cache entries that already passed integrity checks. Corrupt or incomplete entries are never used.

### Skill defaults

The CLI itself has no target defaults. When the user does not specify them, the installed Skill explicitly supplies:

- Region: `cn`
- Build: `latest`
- Locale: `zhCN`

There is no default product. If the product cannot be inferred from the request or context, the Skill asks first.

### Maintenance

```bash
wowdata doctor
wowdata update
wowdata update --version 0.0.2
wowdata uninstall
wowdata uninstall --keep-data
```

`doctor` is read-only. `uninstall --keep-data` removes the CLI and managed Skill while preserving Profiles, Builds, and cache data.

### Build from source

```bash
go test ./... -count=1
go build -trimpath -o dist/wowdata ./cmd/wowdata
npm test
```

Pushing a `vX.Y.Z` tag runs the test suite, builds five platforms, creates `SHA256SUMS` and a GitHub Release, then publishes the npm package through Trusted Publisher OIDC with provenance.

---

## License

[AGPL-3.0-or-later](LICENSE)
