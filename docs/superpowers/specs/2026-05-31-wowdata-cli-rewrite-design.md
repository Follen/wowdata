# wowdata CLI 重写设计

> **给自动化执行者：** 必须使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 按任务逐步实现。本计划使用复选框（`- [ ]`）跟踪进度。

**目标：** 将当前的魔兽世界数据工具重写为一个名为 `wowdata` 的 Go CLI，暴露全部现有业务能力，并保持稳定、适合脚本调用的输出。

**架构：** 用一套共享的 Go 核心实现 CASC、DB2/WDC/DBC、BLTE、BLP、listfile、item、creature、decor 和诊断逻辑；CLI 层只负责子命令路由，并默认输出确定性的 JSON。当前 Node 仓库是必须复刻的能力基线，直到 Go 版对旧 Node 版已封装能力和 core 未封装能力都通过黄金样本对照。

**技术栈：** Go、标准库、Cobra 或同类 CLI 库、JSON 输出、文件系统缓存、HTTP 客户端、二进制解析、基于 fixture 的黄金测试。

---

## 范围

`wowdata` 将替代当前 Node 实现，成为主执行程序。
它必须完整复刻当前 Node 仓库里的全部业务能力，而不只是旧 Node 入口已经封装出来的工具能力。
这里的“完整复刻”包括：

- 旧 Node 入口已经封装出来的对外能力
- core 模块中存在但尚未封装为对外工具的能力
- 现有缓存、导出、诊断和查询语义
- 现有本地/远程 source 行为
- 现有错误路径中对业务有意义的行为

如果某个 Node 行为被判断为历史副作用、bug 或无业务意义，不能直接丢弃；必须在设计或实现记录中明确列为“有意差异”，并说明 Go CLI 的替代行为。

包含的能力组：

- 本地客户端或远程 CDN 的 warmup 和数据源选择
- DB2 的 schema、行、过滤、外键和搜索访问
- 技能链、光环检测和召唤检测
- Encounter 章节树和关联 SpellID
- 文件 lookup、search、extension 查询、原始读取和导出
- 将 BLP 图标导出为 PNG 和 WebP
- CASC 诊断、encoding/root/build 检查、文件存在性检查
- item、model、geoset、texture、creature 和 decor 查询
- 用于回归测试的黄金样本捕获和对照工具

## 非目标

- 第一版不做 MCP server，也不提供 MCP 兼容层。
- 不做只保留 Node 运行时的半吊子重写。
- 不为了实现简单而改变业务语义。
- 不做 GUI。

## CLI 形态

二进制名称是 `wowdata`。

必须具备的顶层行为：

- `wowdata --help`
- `wowdata <command> --help`
- 稳定的退出码
- 正常结果默认输出机器可读 JSON
- 错误信息输出到 stderr

计划中的命令分组：

- `warmup`
- `db2`
- `spell`
- `encounter`
- `file`
- `icon`
- `casc`
- `item`
- `creature`
- `decor`
- `golden`

计划中的子命令：

- `wowdata warmup`：初始化 source、region、product/build、listfile、DBD manifest，并可选预热 DB2 表
- `wowdata db2 schema <table>`：输出解析后的 DBD/WDC schema 元数据
- `wowdata db2 rows <table>`：按 ID、字段选择、过滤和 limit 获取行
- `wowdata db2 search <table>`：按字段做不区分大小写的搜索
- `wowdata db2 foreign-key <table>`：按外键查询关联行
- `wowdata db2 stream <table>`：以 JSON lines 流式输出大表数据
- `wowdata spell info`：递归查看触发链和描述引用
- `wowdata spell auras`：检测技能是否带 aura
- `wowdata spell summons`：检测技能效果中的 NPC 召唤
- `wowdata encounter get`：返回 JournalEncounter 的章节树和相关 SpellID
- `wowdata file lookup`：把 fileDataID 解析为文件名
- `wowdata file search`：搜索 listfile 条目
- `wowdata file extension`：按扩展名列出文件
- `wowdata file get`：按 fileDataID 或文件名获取原始 CASC 文件
- `wowdata file exists`：检查 fileDataID 或文件名是否存在
- `wowdata file encoding`：查看 content key 和 encoding key 元数据
- `wowdata file export`：把原始 CASC 文件写到磁盘
- `wowdata icon export`：将 BLP 导出为 PNG 或 WebP，支持 mask、mipmap 和 quality 参数
- `wowdata casc info`：显示当前 build、build key、region、source、locale 和缓存路径
- `wowdata casc products`：列出某个 source 可用的 products/builds
- `wowdata casc diagnose`：检查 CDN host、archive、root、encoding、cache 和 TACT key 状态
- `wowdata item get`：返回 item 概要和装备槽信息
- `wowdata item models`：返回 item 的 model fileDataID、race/gender 选择和 textures
- `wowdata item geosets`：返回 item 的 geoset 和 helmet-hide 数据
- `wowdata item textures`：返回角色 texture fileDataID
- `wowdata creature display`：按 display ID 或 fileDataID 查询 creature display 元数据
- `wowdata creature model`：查询 creature model fileDataID 和 display 变体
- `wowdata decor list`：列出 decor 条目
- `wowdata decor get`：按 ID 或 model fileDataID 查询 decor item
- `wowdata golden capture`：把 Node 基线或 Go 命令输出捕获为 fixture
- `wowdata golden compare`：把 Go 命令输出和已捕获基线做对照

示例意图：

- `wowdata warmup --source remote --region cn --product wow`
- `wowdata db2 rows SpellName --id 123`
- `wowdata file lookup --file-data-id 456`
- `wowdata icon export --file-data-id 789 --format png`

## 数据模型

Go 核心需要显式维护的运行时状态包括：

- 当前数据源：local 或 remote
- region 和 product/build 上下文
- build cache 和已下载的 manifest 数据
- 已加载的 listfile 和 DBD manifest 状态
- 缓存路径和输出路径
- 当前 warmup 状态和诊断信息

CLI 不能复制业务逻辑；它只做参数解析、调用服务和格式化输出。

## 实现边界

建议的包划分：

- `internal/app`：命令装配和生命周期
- `internal/casc`：本地/远程 source 加载、build 配置、archives、encoding、root、cache
- `internal/db2`：DB2/WDC/DBC 读取器和查询辅助
- `internal/blp`：纹理解码与导出
- `internal/listfile`：文件名和扩展名查询
- `internal/wowdata`：item、creature、decor、spell 和 encounter 业务服务
- `internal/golden`：fixture 捕获和对照
- `cmd/wowdata`：二进制入口

命令层应该依赖接口，而不是依赖具体解析器内部实现。

## 输出契约

默认命令输出应为 JSON，并且结构稳定：

- `ok`
- `command`
- `data`
- `warnings`
- 失败时使用 `error`

这样既方便脚本调用，也方便做 Node 和 Go 的对照。

输出格式可以比 Node 更适合 CLI，但输出内容必须覆盖 Node 的业务信息。对旧 Node 对外能力已经返回的关键业务字段，Go CLI 要么保留等价字段，要么在兼容说明中记录映射关系。

## 帮助设计

`--help` 必须在没有上下文时也能读懂。

帮助内容至少要包括：

- `wowdata` 是做什么的
- 如何 warmup 本地或远程数据源
- 命令分组列表
- 常用工作流的简短示例
- 某些命令首次执行可能比较慢的提示

每个命令的帮助页都必须包含：

- 目的
- 必填参数
- 重要可选 flag
- JSON 输出结构摘要
- 至少一个示例
- 是否需要 warmup 或活动 build 上下文

帮助输出本身就是产品的一部分。测试应该验证 `wowdata --help` 和每一个计划中的 `wowdata <command> --help` 都返回退出码 0。

## 回归策略

在替换 Node 之前，先从当前实现捕获黄金输出。

黄金覆盖应包含：

- 有代表性的 warmup 流程
- 每一个旧 Node 入口已经封装出来的对外能力
- 每一个 core 模块中已有但尚未封装为对外工具的业务能力
- 缺少参数和未初始化状态的错误路径
- 本地和远程 source 的样例行为
- 文件导出输出和缓存复用

对照规则：

- 比较语义 JSON 字段，而不是原始排版
- 比较生成产物的文件存在性和导出内容哈希
- 如果 Go CLI 有意改进格式，必须把预期差异记录清楚

## 迁移计划

1. 用 fixture 冻结当前 Node 行为。
2. 实现 Go 核心包。
3. 实现 `wowdata` CLI 的帮助和命令树。
4. 先迁移当前已经暴露的工具。
5. 再迁移当前没有暴露的业务能力。
6. 持续做黄金对照，直到达到一致。
7. 用 Go 二进制替换 Node 入口。
8. 只有在 Go 版稳定后，才退役 Node 源文件。

## 风险

- CASC 和 DB2 解析风险最高，因为它们编码了很多 WoW 特有的隐式行为。
- 如果不尽早抓黄金样本，输出漂移的概率会很高。
- 某些旧 Node 行为可能只是历史遗留的副作用；这些情况不能默认照抄，必须单独分类。

## 验收标准

- `wowdata --help` 可用。
- 所有计划中的能力都能通过 CLI 命令访问。
- 当前 Node 仓库已有的业务能力全部能在 Go CLI 中找到等价入口。
- 未封装的 Node core 能力也必须被清点、迁移、测试；不能因为旧版没有对外工具就跳过。
- 本地和远程 source 的 warmup 都可用。
- 已发布命令集的黄金对照全部通过。
- 正常使用不再依赖 Node 版本。
