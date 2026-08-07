# Outcome

把 wowdata 重构为以 Go 实现为核心的纯 CLI 工具。npm 负责安装和更新 CLI，并默认把轻量 wowdata Skill 安装到 `~/.agents/skills/wowdata`；Skill 负责把用户语义桥接为带完整数据目标的稳定 CLI 原子命令。所有本地状态统一进入 `~/.wowdata`。CLI 本身没有数据目标默认值，中国区、简体中文和 latest 策略只由 Skill 应用。

# Scope

- 删除 MCP stdio、HTTP MCP、兼容参数、入口、实现、测试、依赖和文档，只保留 CLI 产品面。
- 保留现有 Go CLI 原子命令的名称、层级、参数和 JSON 输出合同，并允许增加安装维护及缓存管理命令。
- 建立 `~/.wowdata` 的二进制、配置、Profile/Build、缓存、临时文件和锁等分区及生命周期。
- 设计查询时按需准备、显式预热、持久缓存复用、状态查看、校验、清理和失败恢复动线。
- 新建标准 npm 包并提供全局 `wowdata` 命令；安装时默认安装 CLI 和 wowdata Skill。
- 提供 `wowdata update` 和 `wowdata uninstall`，其中更新由 npm 完成。
- 提供只读的 `wowdata doctor` 基础诊断，让用户或 Agent 快速定位安装、环境、目标、缓存和网络问题。
- 使用 Git tag 触发 GitHub Actions，测试、构建各平台 Go 二进制、生成校验信息、创建 GitHub Release 并发布 npm。
- 在仓库内维护不携带二进制的 wowdata Skill 源码及 Reference；参考 wowdoc 的 CLI-only、原子调用和可追溯输出组织方式。
- 更新 README、安装说明、目录与缓存说明、发布说明。

# Non-goals

- 不提供任何 MCP、HTTP 服务、Ticket 或常驻服务进程。
- 不用 Node.js 重写 CASC、DB2、WoWDBDefs 或业务查询核心。
- 不删除、重命名或合并现有 CLI 原子命令。
- 不把 Go 可执行文件继续内嵌在 Skill 目录中。
- 不在这次重构中把 wowdoc 合并进 wowdata。

# Acceptance examples

- Skill 生成远端命令时，用户未指定区域、语言和 Build 则显式传入 `--region cn --locale zhCN --build latest`；用户显式要求其他值时原样传递。
- 用户或上下文没有给出产品时，Skill 必须先询问用户选择正式服、怀旧服、经典旧世、PTR、Beta 等产品，再调用 CLI；不得默认成正式服。
- CLI 调用必须明确传完整 source/region/product/build/locale（本地源还包括 path），或明确传一个包含完整目标的 `--profile`；缺少任一必需目标时返回结构化错误，不使用默认值，也不交互提问。
- `wowdata --help` 不出现 `mcp` 命令、`--mcp` 参数或任何 HTTP 服务入口，仓库也不再编译 MCP 实现。
- 现有 `db2 rows`、`db2 schema`、`file export`、`icon export`、`spell info` 等命令仍按原名称和参数工作，现有 golden 合同继续通过。
- `npm install -g @follen/wowdata` 后 PATH 中存在 `wowdata`，匹配当前系统的 Go 二进制位于 `~/.wowdata/bin`，Skill 位于 `~/.agents/skills/wowdata` 且不包含 exe。
- Skill 收到“查法术、导出图标、检查 DB2、选择客户端 Build”等自然语言意图时，按 Reference 选择并执行一个或多个现有原子命令，不调用 MCP。
- `wowdata casc products --source remote --region cn` 只向 CDN 查询并返回该区域可用的产品、版本、Build 与语言组合，不执行预热。
- 产品已经由显式参数或本地 Profile 确定后，用户可以直接执行业务原子命令。CLI 解析有效目标并检查缓存；缺失或过期时报告正在下载/预热，准备完成后在同一次命令中继续查询并返回最终结果。
- `wowdata warmup` 作为可选的提前准备命令保留，与查询命令复用同一套缓存判定和下载实现；Skill 不得强制用户先显式运行 warmup。
- `wowdata update` 通过 npm 更新 `@follen/wowdata`，并同步更新托管的 CLI 与 Skill。
- `wowdata uninstall` 默认删除全局命令、托管 Skill 和整个 `~/.wowdata`；显式使用 `--keep-data` 时保留配置、Profile 和缓存。
- 符合版本格式的 Git tag 能在测试通过后生成当前支持平台的 Release 资产、校验文件和同版本 npm 包。
- 缓存损坏、下载中断或并发预热不会把半成品当作有效缓存；用户可查看占用、校验并清理可安全重建的数据。
- 远端检查失败但本地缓存完整时，CLI 使用已验证的离线缓存继续查询，并在 stderr 明确提示；本地缓存缺失或损坏时返回稳定错误。
- `wowdata doctor` 返回结构化检查项，指出 CLI/npm 版本、目录权限、活动目标、缓存完整性、锁/临时文件、CDN/WoWDBDefs/Listfile/TACT 连通性及建议的下一条命令。
- `~/.wowdata/profiles/default.json` 可以表达“远端、中国区、正式服、简体中文、跟随最新 Build”；当 CDN 发布新 Build 时，CLI 更新该 Profile 的解析结果，并把具体 Build 身份记录在 `builds/`。

# Constraints and invariants

- Go 继续作为数据解析和命令执行核心；Node.js 只承担 npm 安装、启动桥接和包生命周期。
- CLI 不设置 source、path、region、product、build 或 locale 默认值。
- Skill 在用户未显式要求其他目标时使用远端、`cn`、`zhCN` 和 `latest`；产品语义选择由 Skill 根据用户话语/上下文确定或向用户询问。产品发现与真实 Build 信息由 CLI 处理，Skill Reference 不写死具体 Build 号。
- 支持矩阵至少保持现有 CI 的 Windows amd64、Linux amd64/arm64、macOS amd64/arm64。
- 发布资产和本地安装必须可校验，落盘采用临时文件加原子替换，并避免并发写坏共享状态。
- CDN 数据和彼此独立的 manifest 获取使用有界并发；存在依赖的版本/Build/CDN 配置按依赖顺序执行。默认 4 个下载 worker，可配置，并按内容身份去重。
- 保留本轮开始前已有的 WoWDBDefs 远端优先、缓存回退相关工作区改动。
- npm 与 Skill 中的脚本保持源码可读，不引入不可审计的安装器二进制。

# Decisions

- 产品定位：wowdata 只提供 CLI。
- 发布包名：`@follen/wowdata`；npm 组织 `follen` 已确认当前账号为 owner，包名当前未发布。
- CLI 目标合同：没有任何隐式数据目标，调用方必须传完整目标或明确 Profile。
- Skill 默认策略：用户未显式要求其他目标时，Skill 传入远端、`region=cn`、`locale=zhCN` 和 `build=latest`；产品没有默认值。
- 产品选择：Skill 优先使用用户显式产品或上下文中唯一可确定的产品；仍不明确时必须询问用户。CLI 不交互提问，也不静默补任何目标字段。
- Skill 默认安装位置：`~/.agents/skills/wowdata`。
- Skill 职责：Reference、语义路由和原子命令编排；不携带 CLI 二进制，不实现数据核心。
- 对外命令保持原子：`casc products` 负责显式列出组合，`warmup` 负责可选的提前准备，业务命令从用户视角只需直接查询。业务命令内部可以自动完成查询所需的缓存检查、下载和预热，但不得要求 Agent 另发一条 warmup 命令。
- 缓存准备模型：CLI 根据远端内容身份和本地完整性记录决定命中、增量下载或重建；准备期间报告动作和进度，准备完成后继续原查询。
- 输出分流：自动准备过程写入 stderr，最终命令结果保持 stdout JSON；进度不得污染现有结构化结果。
- 故障诊断：新增只读原子命令 `wowdata doctor`，默认只报告问题和下一步，不自动修改或删除数据。
- 卸载策略：默认完整删除 CLI、Skill 和 `~/.wowdata`；需要重装复用时由用户显式选择 `--keep-data`。
- 本地目录：`~/.wowdata` 使用 `bin/`、`config/`、`profiles/`、`builds/`、`cache/`、`state/`、`tmp/`、`locks/` 分区；cache 再按 casc/dbd/listfile/tact 分开。
- Profile 与 Reference 分工：Profile 是本机动态目标和最新 Build 解析结果，必须位于 `~/.wowdata/profiles`；Skill Reference 只保存稳定的客户端/语言别名、表依赖和命令映射。
- Build 缓存保留：每个 Profile 保护当前 Build 和上一个 Build；更老缓存只在超过容量上限时按最久未使用顺序清理。
- 缓存容量：默认上限 20GB，允许用户通过配置修改。
- Skill 安装冲突：发现现有 `~/.agents/skills/wowdata` 不是本包托管版本时，先原子备份旧目录再安装；后续更新只覆盖本包托管内容。
- 离线回退：远端身份检查失败时允许使用已经校验完整的缓存继续查询，并明确标记离线状态；不使用未校验、损坏或半成品缓存。
- 更新渠道：`wowdata update` 走 npm。
- 发布触发：语义版本 Git tag 触发 GitHub Release 与 npm 发布。
- 共享理解：用户已确认本 brief 与完整目标规格，可以进入实施。

# Open questions

- 无。

# Verification expectations

- 运行 `go test ./... -count=1`，并确保删除 MCP 后不再有相关包、命令、参数或文档引用。
- 对现有 CLI 命令树和 golden fixture 做回归，证明原子命令合同未被重命名或破坏。
- 在隔离 HOME/npm prefix 中执行 npm pack、全局安装、启动、Skill 安装、更新模拟、卸载和重复安装验证。
- 验证 CLI 缺少目标时稳定失败、Skill 显式传入 `remote/cn/zhCN/latest`，以及用户要求其他目标时完整覆盖。
- 验证 `~/.wowdata` 目录初始化、缓存命中、原子写、损坏检测、并发锁和清理边界。
- 校验 GitHub Actions 的 tag/version 一致性、Release 资产矩阵、SHA-256 文件及 npm provenance/发布配置。
