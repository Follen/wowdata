# wow-mcp

一个用于查询《魔兽世界》CASC 游戏数据的 MCP Server。它可以连接本地游戏目录或官方 CDN，读取构建版本中的 DB2 表、技能数据、文件列表和 BLP 图标，方便在 AI 工具或 MCP 客户端中直接分析 WoW 数据。

## 功能特性

- 支持连接本地 WoW 客户端目录或远程 CDN。
- 支持选择并加载指定构建版本。
- 查询任意 DB2 表结构和数据，例如 `SpellName`、`SpellEffect`、`Item`、`Creature`、`Map`、`Achievement`。
- 支持按 ID、字段、简单条件和文本关键字检索 DB2 数据。
- 支持搜索 CASC 文件列表，按 `fileDataID` 精确查找文件路径。
- 支持提取 BLP 图标并导出为 PNG。
- 提供技能链递归查询、技能光环检测、NPC 召唤检测、Encounter 关联技能查询等面向副本/技能分析的工具。

## 环境要求

- Node.js 18 或更高版本
- npm
- 一个支持 MCP 的客户端，例如 Codex、Claude Desktop 或其他 MCP Host

## 安装

```bash
npm install
```

> 仓库不会提交 `node_modules/`、`user_data/`、`output/` 和日志文件。首次运行时会按需重新生成缓存或输出目录。

## 启动

标准 MCP stdio 启动方式：

```bash
npm start
```

也可以在启动时自动连接数据源并加载构建版本，避免第一次工具调用超时。

连接远程 CDN：

```bash
node index.js --remote cn --build 0
```

连接本地游戏目录：

```bash
node index.js --local "D:\World of Warcraft\_retail_" --region cn --build 0
```

参数说明：

- `--remote <region>`：使用远程 CDN，区域支持 `cn`、`us`、`eu`、`kr`、`tw`。
- `--local <path>`：使用本地 WoW 游戏目录。
- `--region <region>`：指定区域和默认语言。
- `--build <index>`：加载 `wow_connect` 返回的构建版本索引。

## MCP 工具列表

| 工具名 | 说明 |
| --- | --- |
| `wow_connect` | 连接 CASC 数据源，并返回可用构建版本列表。 |
| `wow_load_build` | 加载指定构建版本。 |
| `wow_db2_schema` | 获取 DB2 表字段和总行数。 |
| `wow_query_db2` | 查询 DB2 表数据，支持 ID、字段、过滤和限制数量。 |
| `wow_search_db2` | 在 DB2 表的指定字符串字段中进行文本搜索。 |
| `wow_extract_icon` | 从 CASC 中提取 BLP 图标并保存为 PNG。 |
| `wow_list_files` | 浏览或搜索 CASC 文件列表。 |
| `wow_check_spell_auras` | 批量检测技能是否包含光环效果。 |
| `wow_get_npc_summons` | 检测技能是否召唤指定 NPC。 |
| `wow_batch_spell_info` | 递归查询技能及其触发子技能的完整信息。 |
| `wow_encounter_spells` | 查询 JournalEncounter 关联的技能和章节树。 |

## 使用流程

1. 调用 `wow_connect` 连接数据源。
2. 从返回的构建版本列表中选择一个 `buildIndex`。
3. 调用 `wow_load_build` 加载构建版本。
4. 使用 DB2、技能、图标或文件查询工具读取需要的数据。

如果使用启动参数提前完成连接和加载，可以直接调用查询类工具。

## 缓存和输出

运行过程中可能生成以下本地目录或文件：

- `user_data/`：CASC、DBD、listfile 等缓存。
- `output/`：图标等导出结果。
- `*.log`：运行日志。
- `node_modules/`：npm 依赖。

这些内容都已加入 `.gitignore`，不建议提交到 GitHub。

## 项目说明

本项目基于 wow.export 相关核心模块封装，将 WoW CASC 数据读取能力暴露为 MCP 工具，适合用于游戏数据分析、技能机制排查、Boss Encounter 技能整理、图标导出和 DB2 快速查询。

