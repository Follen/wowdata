# wow-mcp

一个精简的《魔兽世界》CASC/DB2 查询 MCP Server。它用来连接本地客户端或官方 CDN，查询 DB2、技能、Encounter、文件列表和 BLP 图标。

## 功能特性

- 支持本地 WoW 客户端目录或远程 CDN。
- `wow_warmup` 作为唯一初始化入口，可选择 Retail、Classic、Titan Reforged、Classic Era、PTR、Beta 等产品线。
- 启动阶段只做 CASC 查询必须的配置、index、encoding、root；listfile、DBD manifest 和常用 DB2 表在 warmup 阶段预热。
- listfile 是全局缓存，warmup 会校验本地缓存，缓存有效时直接复用，损坏或过期时才重新下载。
- 提供 DB2、技能链、技能光环、召唤 NPC、Encounter 章节树、文件搜索和图标导出能力。

## 环境要求

- Node.js 18 或更高版本
- npm
- 一个支持 MCP 的客户端，例如 Codex、Claude Desktop 或其他 MCP Host

## 安装

```bash
npm install
```

> 仓库不会提交 `node_modules/`、`user_data/`、`output/` 和日志文件。首次运行时会重新生成缓存或输出目录。

## 启动

标准 MCP stdio 启动方式：

```bash
npm start
```

推荐让模型在连接后先调用 `wow_warmup`。无参数调用会返回可选的数据源、区域和产品线；如果用户已经告诉模型要用哪个客户端，也可以直接带参数调用。

远端 CDN 示例：

```json
{
  "source": "remote",
  "region": "cn",
  "product": "wow"
}
```

本地客户端示例：

```json
{
  "source": "local",
  "path": "D:/World of Warcraft/_retail_",
  "region": "cn",
  "product": "wow"
}
```

也可以用启动参数提前 warmup：

```bash
node index.js --remote cn --build 0
node index.js --local "D:\World of Warcraft\_retail_" --region cn --build 0
```

参数说明：

- `--remote <region>`：使用远程 CDN，区域支持 `cn`、`us`、`eu`、`kr`、`tw`。
- `--local <path>`：使用本地 WoW 游戏目录。
- `--region <region>`：指定区域和默认语言。
- `--product <product>`：按产品线自动选择构建，例如 `wow`、`wow_classic_titan`。
- `--build <index>`：加载产品列表里的构建版本索引。

首次 `wow_warmup` 可能需要较久，MCP Host/模型应耐心等待，建议工具超时设置为 240 秒。

## MCP 工具列表

| 工具名 | 说明 |
| --- | --- |
| `wow_warmup` | 初始化并预热。无参数列出本地/CDN、区域、产品线和 build 选择；带参数后连接、加载 build、校验缓存并预热。 |
| `wow_db2` | 查询 DB2，支持 `schema`、`rows`、`search`。 |
| `wow_spell` | 查询技能，支持 `info` 技能链、`auras` 光环检测、`summons` 召唤 NPC 检测。 |
| `wow_encounter` | 查询 JournalEncounter 的章节树和关联 SpellID。 |
| `wow_file` | 查询文件列表，支持 `lookup`、`search`、`extension`、`extractIcon`。 |

## 使用流程

推荐流程：

1. 无参数调用 `wow_warmup`，让它列出本地客户端/CDN、区域、Retail/Classic/Titan Reforged/PTR/Beta 等选择。
2. 用户选择后，带 `source`、`region` 或 `path`、`product` 或 `buildIndex` 再调用 `wow_warmup`。
3. `wow_warmup` 会连接数据源、加载 build、校验/下载 listfile、准备 DBD manifest，并预热常用 DB2 表。
4. 之后使用 `wow_db2`、`wow_spell`、`wow_encounter` 或 `wow_file` 查询。

## 启动慢的原因

WoW CASC 不是单个本地数据库文件。加载一个 build 时必须解析：

- CDN/build 配置。
- archive index，用来知道数据块在哪个 CDN archive 里。
- encoding 表，用 content key 找到实际 data key。
- root 表，用 fileDataID 找到 content key。

这些是 CASC 查询的基础，没法完全省掉。首次远程加载会比较慢，之后会缓存在 `user_data/casc/`。现在把真正重的 listfile 下载/解析、DBD manifest 下载和常用 DB2 表读取放在 `wow_warmup`，这样启动 MCP 后能明确告诉模型等待 240 秒，并且后续查询更快。

## 缓存和输出

运行过程中可能生成以下本地目录或文件：

- `user_data/`：CASC、DBD、listfile 等缓存。
- `output/`：图标等导出结果。
- `*.log`：运行日志。
- `node_modules/`：npm 依赖。

这些内容都已加入 `.gitignore`，不建议提交到 GitHub。

## 项目说明

本项目基于 wow.export 相关核心读取模块封装。当前目标是保留一个精简、可预热、缓存可校验的 CASC/DB2 查询 MCP。
