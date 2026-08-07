# wowdata CLI 完整目标规格

## 产品边界

### Requirement: CLI-only 产品面

wowdata 必须只暴露命令行界面。发行物、帮助、源码、依赖、测试和文档中不得保留 MCP stdio、HTTP MCP、Ticket 或服务端入口。

### Requirement: 保持原子命令合同

重构必须保留现有业务原子命令的名称、层级、参数语义和结构化输出合同，包括 warmup、db2、spell、encounter、file、icon、casc、item、creature、decor、video 和 golden 命令族。安装维护和缓存生命周期命令可以新增，但不得用它们替换已有业务原子命令。

## 默认数据目标

### Requirement: CLI 目标零默认值

CLI 不得为 source、path、region、product、build 或 locale 设置业务默认值。每次需要数据上下文的调用必须显式传入完整目标，或显式传入一个能够解析出完整目标的 `--profile`。任一必需字段缺失时，CLI 必须保持非交互并返回结构化 `target_required` 错误、缺失字段和可用的发现命令，不得自行补值或在终端提问。

### Requirement: Skill 默认策略

Skill 必须优先保留用户显式提供的 source、path、region、product、build 和 locale。远端任务中用户未指定 region、build 或 locale 时，Skill 必须显式传入 `region=cn`、`build=latest` 和 `locale=zhCN`，不得依赖 CLI 默认值。

产品没有默认值。Skill 必须优先采用用户显式给出的产品，或上下文中能够唯一确定的产品；两者都不存在时必须先询问用户，再调用 CLI。用户显式要求本地源时，Skill 必须取得明确 path 和其他必需目标字段。

### Requirement: Build 由 CLI 解析

CLI 必须从本地客户端或远端版本服务解析真实产品与 Build。Skill Reference 只能保存稳定的客户端别名、语言别名、表依赖和命令映射，不得把当前 Build 号当作长期常量。

### Requirement: 原子发现与直接查询

`casc products` 必须作为显式发现命令，按 source、region 或本地路径返回真实可用的 product、version、build ID、build config key、CDN config key 和 locale 组合，不下载业务查询所需的完整缓存。

业务查询命令必须允许用户直接调用。CLI 必须在同一个进程内解析有效数据目标、检查查询依赖、复用有效缓存，并在缺失、过期或损坏时完成必要下载和预热，然后继续执行原查询并返回最终结果。调用方不得被迫先运行另一条 warmup 命令。

`warmup` 必须作为可选的提前准备原子命令保留，并与直接查询共用完全相同的目标解析、缓存判定、下载、校验和发布实现。它不得成为业务查询的前置调用合同。

CLI 必须严格按完整参数或明确 Profile 解析真实远端组合。`build=latest` 是调用方显式传入的选择策略，不是 CLI 默认值；CLI 只能在收到该值后解析所选产品的最新 Build。Skill 可以在用户要求查看可选组合时调用 `casc products`，但普通查询不得为了形式要求固定的发现或 warmup 前置步骤。

### Requirement: 可见的按需准备过程

直接查询触发下载或预热时，CLI 必须通过 stderr 持续报告正在解析的目标、缓存命中与缺失、计划下载内容、已完成进度和校验结果。准备完成后必须自动继续原查询，并只把最终结构化结果写入 stdout；准备失败时必须返回稳定错误码、已完成状态和可重试信息。进度不得混入或破坏现有 stdout JSON 合同。

## 本地数据目录

### Requirement: 统一 home root

CLI 的托管二进制、用户配置、Profile/Build 状态、可重建缓存、临时下载和并发锁必须统一位于 `~/.wowdata` 下。固定分区为 `bin/`、`config/`、`profiles/`、`builds/`、`cache/`、`state/`、`tmp/` 和 `locks/`，其中 `cache/` 至少按 casc、dbd、listfile、tact 分区。首次安装或首次运行必须按需创建所需目录；删除可重建缓存不得误删用户配置或导出文件。

### Requirement: 动态 Profile 与不可变 Build

Profile 必须保存在 `~/.wowdata/profiles`，并完整表达 source、必要 path、region、product、locale 和 Build 选择策略。Profile 不具有 CLI 注入的默认字段，只有完整目标已经由用户或 Skill 确定后才能创建。CLI 收到明确 Profile 后根据其中的策略解析 latest，发现新 Build 后原子更新 resolved Build。具体 Build 的 version、build ID、build config key、CDN config key、locale 和依赖身份必须作为按内容身份区分的快照保存在 `~/.wowdata/builds`，不得用新 Build 覆盖旧 Build 身份。

Skill Reference 不得保存动态 Profile 或当前 Build 号；它只保存稳定的客户端别名、语言别名、表依赖和语义到原子命令的映射。

### Requirement: 可靠缓存

下载和缓存写入必须先写临时文件，完成内容校验后再原子发布。并发进程必须通过有界锁或等价机制避免写坏同一对象。损坏或不完整缓存必须被识别为无效并可重新获取，网络失败时只能回退到已校验的完整缓存。

彼此独立的 CDN 对象和 manifest 请求必须通过默认 4 个 worker 的有界并发池获取，并允许配置 worker 数。存在数据依赖的 versions、Build config、CDN config 等步骤必须按依赖顺序调度；单个小型 manifest 不得为了并发而无条件切片。相同内容身份的并发请求必须合并或通过锁去重，失败必须有界重试，只有完整校验后的对象才能原子发布到共享缓存。

远端内容身份必须使用各来源的真实稳定标识：CASC 使用 Build/CDN config key、content key、encoding key 和相关内容地址；WoWDBDefs 使用 Git commit SHA；其他来源优先使用发布摘要或内容地址，并为本地文件保存 SHA-256。不得把 Last-Modified 时间或下载成功本身当作完整性证明。

远端版本或身份检查失败时，如果有效 Profile 所需的本地对象都已通过清单和内容校验，CLI 必须继续完成查询，并通过 stderr 标记正在使用已验证的离线缓存。如果任何必需对象缺失、损坏或仍是临时文件，CLI 必须失败并返回稳定错误码，不得把未知状态伪装成缓存命中。

### Requirement: 缓存可管理

CLI 必须提供可脚本化的缓存状态、校验和清理能力，区分可重建缓存与配置/Profile。清理操作必须给出结构化结果，并允许用户明确要求全量清理。

缓存默认容量上限必须为 20GB，并允许用户通过配置修改。每个 Profile 的当前 Build 和上一个 Build 必须受到自动清理保护。缓存超过配置容量上限时，CLI 必须只从未受保护的可重建缓存中按最久未使用顺序清理；不得为了满足上限删除当前或上一个 Build、配置、Profile、用户导出文件或正在使用的对象。

### Requirement: 基础诊断

CLI 必须提供只读的原子命令 `wowdata doctor`。该命令必须返回结构化检查项，至少覆盖 npm/CLI 版本和安装路径、`~/.wowdata` 目录与写权限、活动数据目标、缓存清单与内容完整性、残留锁和临时文件、CDN 版本服务、WoWDBDefs、Listfile 与 TACT Keys 的基础连通性，以及稳定的问题码和建议下一条命令。默认执行不得下载大体积数据、修复、删除或改写用户状态。

## npm 安装与维护

### Requirement: 标准 npm 包

发行包必须命名为 `@follen/wowdata`，提供全局 `wowdata` 命令，并为 Windows amd64、Linux amd64/arm64、macOS amd64/arm64 选择匹配的 Go 二进制。不支持的平台必须返回明确错误，不得执行错误架构的文件。

### Requirement: 默认安装 Skill

npm 安装必须默认把仓库内的 wowdata Skill 发布到 `~/.agents/skills/wowdata`。安装的 Skill 必须由文本、配置和 Reference 组成，不得包含 Go exe 或另一份数据解析实现；重复安装和升级必须可安全覆盖本包托管的旧版本。

安装发现目标目录存在且不能通过本包的托管清单确认身份时，必须先把旧目录原子移动到带时间与冲突保护的备份路径，再安装新版，不得直接覆盖未知内容。后续升级只能替换清单确认由本包托管的 Skill；卸载也不得删除未被托管清单覆盖的备份。

### Requirement: npm 更新

`wowdata update` 必须通过 npm 获取 `@follen/wowdata` 的目标版本，并同步更新 `~/.wowdata/bin` 中的 CLI 与托管 Skill。成功与失败必须返回可脚本化状态，版本未变化时也必须明确报告。

### Requirement: npm 卸载

`wowdata uninstall` 必须通过 npm 移除全局命令、托管二进制和托管 Skill，并默认删除整个 `~/.wowdata`，包括配置、Profile、Build 状态和缓存。显式使用 `--keep-data` 时必须保留 `~/.wowdata` 中除托管二进制外的数据，以便以后重装复用。两种模式都必须无交互、返回结构化结果，并且只删除本包明确托管的路径。

## Skill Reference

### Requirement: 语义桥接

wowdata Skill 必须采用 wowdoc 的 CLI-only 与原子调用原则，把自然语言任务映射到实际存在的 wowdata 命令。普通数据任务必须直接调用对应业务原子命令，让 CLI 自行完成按需准备；只有用户明确要求提前下载、查看可选组合或管理缓存时，才调用 warmup、casc products 或缓存管理命令。Skill 不得发明 `query rows` 等不存在的命令。

每次调用需要数据上下文的业务命令前，Skill 必须构造完整目标或选择明确 Profile。Skill 必须从用户显式描述或当前上下文取得唯一产品，无法唯一确定时询问用户；随后显式补入远端任务默认策略 `cn/zhCN/latest`。不得默认正式服，也不得把缺失目标或交互职责转给 CLI。

### Requirement: 可追溯结果

Skill 使用 CLI 输出回答时必须保留可追溯的产品、Build、区域、语言、表、文件 ID、输出路径或诊断信息。用户未显式指定区域、语言和 Build 时，Skill 显式传入 `cn/zhCN/latest`；用户显式指定时原样传递。

## 发布

### Requirement: tag 驱动发布

语义版本 Git tag 必须触发 GitHub Actions。在完整 Go 测试和 npm 包测试通过后，工作流必须构建支持矩阵中的二进制、生成 SHA-256 校验信息、创建同版本 GitHub Release，并发布版本一致的 `@follen/wowdata` npm 包。

### Requirement: 可审计供应链

发布工作流必须使用最小权限和 GitHub/npm 的受控凭据，npm 包内安装与启动脚本必须为可读源码。Release 与 npm 包必须能关联到同一 Git commit 和版本，不得在本地手工拼装无法复现的正式包。
