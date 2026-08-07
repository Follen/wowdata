# Outcome

对 wowdata 自动枚举出的全部 CLI 叶子命令，从远程 Build 解析、CASC 下载、WDC/DB2 解码与查询、领域关系遍历到批量文件导出的整条链路进行算法与执行编排重构。在保持数据正确性和现有 CLI 契约的前提下，让每条命令在当前机器、磁盘、网络、CDN 和输入规模下，以合理动态内存预算逼近其 cold 端到端执行 DAG 的理论下界；缓存只负责原始网络 payload 复用和正确性，不作为掩盖解析与查询低效的主要手段。

# Scope

- 远程 product/Build 解析、版本缓存、CASC root/encoding/archive 数据的下载、持久化与热启动；CDN 路径仍属于本 change 的优化口径，并作为本地缺失、损坏或 Build 不匹配时的 fallback。
- DB2/DBD 的按需装载、字段与关系索引、批量 ID/外键查询及跨表领域解析。
- WDC 编译解码计划、字段投影下推、按记录 ID 懒解码、原生 relationship map 和批量向量化读取。
- `internal` 内嵌只读 DB2 引擎：统一 catalog、Build snapshot、逻辑计划、物理算子、批次执行、关系展开和资源调度，供全部 DB2 与领域命令复用。
- 高级数据库工程架构参与设计：稳定 snapshot、列式/向量化批次、代价与特征驱动的物理计划、算子融合、late materialization、临时索引、buffer 生命周期、背压和内存配额；算法选择由数据特征与实测证据驱动。
- DB2 引擎成熟性：快照与资源生命周期、完整 WDC 类型/压缩语义、确定性顺序、计划回退、内存所有权、取消与清理、损坏输入、schema 演进、差分/模糊/竞态验证。
- Encounter 指针树遍历、Spell 批量 BFS frontier、跨表查询计划和关键路径优先 DAG 调度。
- CASC archive 相邻 Range 合并、连接复用、自适应分片、背压及 BLTE/BLP 流水线。
- 目标级缓存锁、对象级写入协调、原子发布、完整性验证和并发读取。
- Spell、Encounter、File、Icon 等业务路径的批量接口与单进程编排。
- 图标等重复 FileDataID 的去重下载、并发解码、独立命名输出和 manifest。
- Skill 的批量优先决策、PowerShell 参数引用、旧 CLI 回退和结果验证规则。
- 冷缓存、热缓存、并发和故障恢复基准及阶段耗时观测。
- 现有 `fixtures/golden`/`internal/golden` 保留为小型冻结语义集；新增按 Build × 全 CLI 子叶 × 真实数据规模展开的 integration benchmark/test layer，作为主要覆盖面，用于发现性能退化、DB2/CASC 字段读取 bug、CLI 未覆盖分支和领域逻辑错误。
- 集成基准使用 `wowdata.integration-benchmark-matrix.v1`：由 Cobra 实际叶子树、Build catalog、本地 source 与 neutral case、四种缓存协议和 success/missing-args/empty-result/representative-error 场景展开；正式封存只运行 `offline`（local + neutral）范围。没有真实本地 corpus 的格子必须输出 `coverageGap` 与原因/证据，不能用 help 或预生成输出冒充成功。
- 对固定真实 Build/locale 中全部可加载文件做结构普查，按文件族、magic/version、section 布局、字段压缩组合、稀疏/定长记录、copy/relationship/string/array 特征、规模与访问模式生成覆盖矩阵。
- 算法不在 Shape 阶段定死：针对普查得到的每个等价类进行候选算法实测，在相同正确性、顺序和资源边界下选择综合成本最优实现，并保留可重复的选择证据与保守回退。
- 生成基线、优化后成绩、算法选择、覆盖缺口、资源消耗和正确性结论的最终性能报告。
- 自动枚举 Cobra CLI command tree 的全部叶子命令，并生成 independent cold 与 shared-Build cold、warm、repeat-warm 的可执行矩阵；不维护可能漏项的手写命令清单。
- 机器与外部源校准：CPU/GOMAXPROCS/内存、顺序与随机磁盘、本地 WoW 安装读取、CDN/GitHub/DBD 网络、BLTE/WDC/DBC/DBD/BLP/SHA-256/JSON/文件写入吞吐，以及句柄与连接预算。
- 可替换 `FileLocator`/`ArchiveLocator`：同时支持本地 CASC 与 CDN 两类定位器；本地安装中存在目标 Build 时默认优先使用本地数据，本地没有、损坏或 Build 不匹配时 fallback 到 CDN。CDN 路径继续优先研究并解析 `file-index`、`patch-file-index`、`archive-group`、`patch-archive-group` 和 `archives-index-size`，建立 FileDataID 到 content/encoding key 再到 archive/offset/size 的直接定位链。
- 每条命令的真实执行 DAG、critical-path 理论下界、TTFB/稳态吞吐/结束开销、动态内存预算、请求/连接/网络/磁盘/CPU/GC/峰值资源计量。
- Build、Verify、Archive 完成后推送仓库、通过远端 CI，并发布新的公开 npm 版本。

# Non-goals

- 不引入 MCP、HTTP 服务或常驻服务端；wowdata 继续是 CLI-only 工具。
- 不提供 SQL 语言、外部数据库协议、事务写入、WAL、分布式执行或大规模并行数据库能力。
- 不改变 WoW 数据的语义、中文本地化内容或目标解析规则。
- 不以跳过完整性校验、降低输出数量或复用错误 Build 来换取性能。
- 不以固定 5 秒、10 秒等与输入规模无关的秒数代替动态理论下界和吞吐模型。
- 不通过持久化每个 Build 的整表解码副本或全量派生快照换取热缓存成绩。
- 不处理与下载、解析、查询、缓存、批量导出和并发编排无关的功能重构；本地优先不削弱 CDN 优化范围，CDN 仍作为 fallback 与独立 benchmark 路径继续达到同一性能标准。

# Acceptance examples

- 输入正式服、国服、最新 Build、`zhCN`、副本名“虚影尖塔”和首领序号 1，一次 CLI 调用返回首领及全部去重技能信息，并向指定目录生成每个技能独立命名的有效 PNG 与 manifest；若 HOME `.env` 中的本地 WoW 安装含匹配 Build，默认优先走本地数据，否则自动 fallback CDN。
- 22 个技能映射到 19 个唯一 FileDataID 时，源文件只读取和解码 19 次，但生成 22 个可验证输出。
- 不同技能或 Aura 即使共享相同 FileDataID，也作为独立语义条目分别使用自己的 ID 和本地化名称生成文件并写入 manifest；FileDataID 只用于底层内容去重。
- 相同 Build 的第二次执行只复用已验证的网络原始 payload 与必要完整性元数据，不重新下载未变化对象；DBD/DB2/CASC 的解析计划、索引和查询结果仍由新引擎在进程内重建并计入成绩。
- 空缓存局部查询不得调用 `GetAllRows` 或在业务层扫描整张 Spell、SpellEffect、SpellMisc、JournalEncounterSection 表。
- Encounter 必须从 FirstSectionID 沿 FirstChildSectionID/NextSiblingSectionID 按需遍历；Spell 引用按去重 BFS frontier 批量扩展。
- 同一 archive 内相邻请求必须按额外字节成本合并 Range，并让下载、解压、解析、解码和写出形成带背压流水线。
- 大文件首次下载使用有界并发 Range 分片；下载中断后再次执行自动从已验证分片继续，不从零重传完整对象。
- 多个独立表或图标使用配置限定的 worker pool 并发；同一缓存对象只产生一个写入者，不同对象不被目标级粗粒度锁串行化。
- 下载或解码中途失败时不发布半成品；重试能复用已验证缓存并返回稳定的机器可读错误。
- 现有单 ID 命令、JSON 字段和退出状态继续工作；新增批量或复合命令不要求现有调用方迁移。
- 全量 golden 运行自动枚举实际 Cobra 命令树和 corpus 分类矩阵；任何命令、成功/错误契约或真实文件等价类缺少基线与优化后结果时，回归判定失败并在报告中列出缺口。
- 对每个真实文件等价类，正确性通过的候选算法必须在同机、同输入、同缓存状态下比较 p50/p95、CPU、allocs、峰值内存、解码量和磁盘 I/O；胜出算法按文件特征自适应选择，小样本使用低调度开销路径，未知类型使用可诊断的正确性优先回退。
- 已知 FileDataID 的普通点查询优先走 locator 直接定位链；格式不支持时只做有界 archive-index 探测，不得默认下载并扫描全部 1348 个 archive index。
- offline 范围内自动枚举的 CLI 叶子命令必须存在 independent cold、shared-Build cold、warm、repeat-warm 记录或明确非目标原因；目标数据命令仅覆盖本地安装实际存在的 Build/locale，neutral 与破坏性命令在隔离 HOME/cache/install 中执行。远程 source case 不进入本次正式封存分母。
- 每个固定输入至少运行 10 次；small/medium/large 输入均进入矩阵，输出与 golden/哈希逐项一致，repeat-warm 的 cache delta 为 0。

# Constraints and invariants

- 缓存身份至少绑定 source、region、product、Build config key 和 locale；本地 source 还必须绑定规范化安装路径。不同 Build 或不同本地安装路径的内容不得互相覆盖。
- 缓存发布必须使用临时文件、原子替换和内容完整性证据；并发不得造成 manifest 丢更新。
- 并发度必须由统一调度器按 CPU、bandwidth-delay product、每任务 p95 工作集、句柄/连接预算、全局内存预算、CDN 限流与错误率动态约束；显式 workers 只覆盖自适应默认值，不允许 metadata、large-range、DB2 parse、BLTE decode、image decode/write 等 pool 嵌套造成并发乘法。
- Build、验证与归档期间允许调用子 Agent 并行编排相互独立的任务以提升执行效率；所有子 Agent 必须继承主 Agent 当前使用的模型与 thinking effort 设置。
- 已知 FileDataID 的读取和图标导出不得依赖 listfile；DBD、TACT Key 和领域表按真实依赖懒加载。
- 基准必须记录命令、输入、缓存状态、下载字节、缓存命中、阶段耗时、输出哈希和退出状态。
- scheduler tournament 必须按资源类使用真实依赖边界：metadata 用按需点查询，large-range 用真实按需大对象数据命令，chunk/resume 用复用生产 downloader 的单对象 Range→`.part`→SHA-256→原子发布链。full `warmup` 只允许评估 `warmup/corpus` 自身，不得用于选择点查询或复合命令的默认并发、Range worker 或 chunk 参数；违反该口径的历史样本必须标记 rejected 且不得进入最终 selection。
- 正式命令矩阵的 current 与冻结 baseline 参数都必须移除弃用的 `--auto-warmup` 兼容旗标并从空 HOME 执行同一按需 DAG；旧 baseline 不支持按需执行时明确记录 unsupported 或 mismatch，不得为取得比较成绩插入全量预热。
- 主要动态评分为 p50 wall time 不超过 `1.25 × T_floor`、p95 不超过 `1.50 × T_floor`，p99/max 不得出现无解释长尾；`required unique bytes / transferred bytes >= 95%`、重复 payload bytes 为 0、持久化解析结果读取为 0。任意大小的 file export、db2 stream、warmup 使用 `fixed_overhead + bytes / target_throughput` 评估并分别报告 TTFB、稳态吞吐和结束开销。
- 当前观测基线：热缓存 `db2 rows` 约 8.4 秒；22 个 spell fan-out 超过 304 秒并超时；19 个唯一图标导出约 270.5 秒。
- 冷、热实测都必须报告解码行数、全表扫描次数、HTTP 请求数、Range 合并率、峰值内存、磁盘增量和缓存放大率；只改善缓存命中的方案不能通过。
- `analyze/` 是本 change 的可再生分析工作区，可以保存下载 corpus 引用、格式清单、剖析、候选算法结果、golden 实际输出和原始 benchmark 记录；必须默认排除版本控制并执行配额/TTL/清理，不能复制同一大 payload，也不能成为生产命令的正确性或性能依赖。
- 正式 cold/warm/repeat-warm 测量必须声明并清理会影响解析成绩的 `analyze/` 中间结果；正式 benchmark 仅运行本地 source 与 neutral case。允许保留的 warm 状态仅是本地执行所需的规格批准状态，不得读取持久化解析结果。

# Decisions

- 用户要求创建 Native change，对下载、解析、并发和编排进行全量、大幅性能优化。
- 性能结果将作为 DeepSWE 公开基准，必须由仓库内可重复执行的基准和真实产物证明。
- 该结果同时作为公开基准测试竞赛成绩提交；竞赛运行协议、环境封印、输入/输出哈希、原始 timing、报告和复现脚本必须可公开审计，禁止通过隐藏缓存、预解析结果或改变输入口径取得成绩。
- 遵循已有 canonical CLI-only 契约，并采用向后兼容的新增批量能力，不删除现有命令。
- 采用 cold、warm、repeat-warm 双轨硬门槛；网络传输与本地处理分别报告，固定 SLA 是通过条件。
- 下载必须提供有界并发和自动断点续传，并在恢复后执行完整内容校验再发布缓存对象。
- 新缓存格式首次启用时只清理旧的可重建缓存并完整重建；不迁移或复用旧缓存，也不删除配置、Profile、Build 选择、托管二进制或用户导出文件。
- 未显式设置 workers 时采用自适应高性能策略，根据 CPU、内存和任务类型分别计算下载、DB2 装载和图片解码 worker pool，并受统一全局上限约束；显式 workers 必须覆盖默认策略。
- 不读取、复用或分析 Claude 的实现，也不运行 Claude 对照编排；只依据本项目规格、基准和剖析结果独立实现。
- 批量查询或导出发生单项失败时保留所有已验证成功项，在 manifest 中逐项记录状态并以退出码 1 结束；重试只处理缺失或失败项。
- 重复导出时，语义身份、Build、FileDataID、格式和 SHA-256 全部匹配的已有文件直接复用；不匹配或损坏的文件通过原子替换修复。
- 图标内容身份与技能/Aura 语义身份分离：共享 FileDataID 不得合并、覆盖或丢失不同名称的逻辑输出。
- 性能实现以冷路径算法和单进程查询 DAG 为核心，不持久化整表解码结果；下载缓存只保存单份内容对象、单个原位续传 `.part` 和小型状态元数据。
- DB2 引擎只存在于 `internal`，使用适合单机只读分析的向量化批次、投影/过滤下推、late materialization、主键与 relationship 访问、轻量计划选择和有界并发；不建设大规模并发框架。
- `db2 rows/search/foreign-key/stream` 以及 spell、encounter、item、creature、decor 等领域服务必须通过同一引擎执行，不得保留绕过引擎的整表扫描快路径。
- DB2 引擎只有在真实 Build 全表 corpus 差分、跨 Build/locale schema 测试、race/fuzz、取消泄漏和资源上限验证全部通过后才能被判定为成熟；接口存在或单一样例跑通不算完成。
- 所有现有 CLI 命令都必须进入回归矩阵，比较优化前后 JSON、退出码、stderr 契约和导出文件哈希；不能只验证新增复合命令。
- 性能验收必须使用本机真实 World of Warcraft 安装、真实 Build 和真实文件执行 cold、warm、repeat-warm 与并发实测；mock 只用于单元测试，不作为性能成绩。远程 CDN 性能、网络抖动与断点恢复不再作为本次 Verify/Archive 完成门禁。
- `fixtures/golden` 中的冻结小型产物应版本化；大规模真实 corpus、原始运行输出和可再生中间体放入受控的 `analyze/`，由内容哈希 manifest 保证可重建与可核验，避免挤占仓库或重复占用磁盘。
- “最优”通过经验选型门禁定义：每个已发现等价类至少比较所有合理且复杂度/内存边界不同的候选；若候选在统计噪声内持平，选择内存更低、实现更简单且尾延迟更稳者。不得仅凭理论复杂度或单个样本固定全局算法。
- 最终报告必须同时包含优化前冻结基线和优化后结果、环境与 Build 身份、样本量、p50/p95、加速比、CPU/内存/分配/磁盘/网络、golden 通过率、文件族覆盖矩阵、算法选择依据、失败与回退、复现命令及原始证据路径。
- 公开竞赛报告必须额外提供固定 benchmark protocol、版本/构建信息、硬件与 OS 摘要、隔离缓存初始化方式、随机性控制、重复次数、统计汇总和可公开复核的原始证据索引；任何不可复核的成绩不作为最终成绩。
- 用户已确认按上述完整目标规格进入 Build；该确认覆盖高级数据库工程架构、全量 golden/文件族回归、自适应候选算法实测、网络原始缓存边界、`analyze/` 产物治理和公开基准竞赛报告。
- 用户补充确认最终目标是全部 CLI 叶子命令在当前环境与动态资源预算下逼近 cold DAG 理论下界，而非只优化 Encounter 或满足固定 10 秒；优先验证 file-index/archive-group 直接定位链，并要求实现、实测、全量回归、报告、Verify、Archive、远端 CI 与 npm 发布完整闭环。
- 用户确认数据命令已经按需准备依赖，网络参数选型不得先做全量预热。`warmup` 继续作为独立 CLI 叶子命令验证，但其 corpus 成绩不能反向覆盖 point/composite 调度默认值。
- 用户补充确认：HOME `.env` 支持本地 World of Warcraft 安装目录；默认未显式 source 时自动寻找/使用本地匹配 Build，本地没有或不可用才走 CDN fallback。CDN 实现仍保留，但 2026-08-06 用户将正式验证范围修订为 offline（local + neutral）；远程 source 不再进入本次性能封存和完成门禁。
- 用户于 2026-08-06 明确要求停止约 4000 个远程/混合样本的长时间正式运行，改为只跑本地数据与 neutral CLI case；四种缓存协议和每个 case 10 次保持不变。当前矩阵对应 3 个 local case 与 28 个 neutral case，共 31 cases、1240 candidate samples。
- 用户要求 wowdata Skill 在 setup 前自动寻找魔兽世界安装路径，最多 1 分钟；找不到才询问用户，并把确认目录写入 HOME `.env`。
- 用户补充确认：新增 all-build/all-leaf integration benchmark/test layer，覆盖每个目标 Build 的全部 CLI 子叶及丰富真实输入，用于替代现有 golden 作为主覆盖面；原有 golden 继续保留为轻量冻结语义基线。
- Build catalog 当前包含正式 CN retail 与 Classic Era US 固定目标，以及本地 `.build.info` 已验证的 retail/classic/titan/anniversary/classic-era Build；catalog 更新必须附带 `.build.info` 原文哈希和发现路径，避免 Build 身份漂移。

# Open questions

- 无。

# Verification expectations

- 建立隔离缓存目录的 local independent-cold/shared-Build-cold/warm/repeat-warm 端到端基准，并至少报告 p50/p95；远程 CDN benchmark 不进入本次完成门禁。
- 对单查询、批量 Spell、完整 Encounter 和批量 PNG 导出分别记录阶段耗时。
- 通过 tracing 和测试断言证明局部目标场景全表扫描次数为 0，并记录实际解码行数和请求 DAG。
- 枚举全部现有命令及主要参数分支，保存基线响应并执行优化前后语义 diff；差异必须被规格明确允许。
- 使用本机真实安装和固定本地 Build 运行端到端 benchmark，保存原始 JSON、timing、磁盘字节、系统资源和输出哈希证据。
- 验证成功缓存的磁盘放大率、活动续传临时空间和 derived metadata 均不超过规格硬上限。
- 使用至少两个真实 Build、两个 locale 和全部可加载 DB2 表建立 corpus，对旧 reader 与新引擎做 schema、行 ID、投影值、relationship、copy table 和错误结果差分。
- 对 WDC header/section/compression/string/array/relationship 输入运行 fuzz，对查询取消、并行读、snapshot close 和 buffer reuse 运行 race 与泄漏测试。
- 使用 `go test ./... -count=1`、`go test -race` 的并发相关包测试和 npm 测试验证回归。
- 使用故障注入验证下载中断、并发写入、损坏缓存、Build 更新和进程终止后的恢复。
- 在下载 25%、50% 和 90% 时终止进程，验证重试仅请求缺失分片、最终哈希正确且临时状态可清理。
- 验证输出技能集合、FileDataID、PNG 签名、尺寸、哈希、数量及现有 JSON/退出状态兼容性。
- 自动生成并审计文件分类清单：每个实际文件必须归入唯一等价类，每个等价类必须有 integration benchmark 覆盖、golden/差分抽样、算法 benchmark 和损坏/边界样本；未知、无法解析或未覆盖条目使验证失败，不能从分母中静默删除。
- 对所有通过正确性门禁的合理候选算法执行可重复 tournament；报告各候选原始成绩、统计样本数、选择阈值与最终 dispatch 规则，并用 holdout 文件验证规则没有只拟合训练 corpus。
- 运行完整 golden manifest，要求命令契约、DB2 投影/关系/copy/string/array 值、导出哈希和失败语义 100% 通过；任何规格未授权差异均阻塞归档。
- 在 `docs/comet/changes/end-to-end-performance/verification.md` 或其引用的版本化项目报告中记录最终基线与优化表现；`analyze/` 原始证据通过 SHA-256 manifest 引用，不把大型可再生 corpus 提交进 Git。
- 自动枚举 CLI 叶子命令并生成按命令排序的总报告；每项包含 baseline、optimized、`T_floor`、p50/p95/max、效率、CPU、heap/working set、GC、请求/连接、网络/磁盘字节、缓存状态、输出哈希、critical path 和回归结论。
- 机器校准必须保存原始命令和结果；`T_floor = process_start + critical_path_RTT + max(required_unique_network_bytes/network_throughput, required_disk_bytes/disk_throughput, required_CPU_work/parallel_CPU_throughput) + unavoidable_output_cost`，可重叠阶段不得简单相加。
- 审计每个网络 tournament 的 workload DAG 与目标资源类；报告必须列出被淘汰或因 full-warmup 污染而 rejected 的候选证据，并证明最终 metadata、large-range、chunk/resume selection 没有隐藏全量预热前提。
- 报告单列仍高于 `1.5 × T_floor` 的命令及准确 critical path；全部门禁通过后才推进 Verify/Archive、推送并等待远端 CI 成功，最后发布 npm 并校验 registry 版本与安装结果。

## 分层回归协议

- `online-small` 远程回归保留为可选诊断，不属于本次 Verify/Archive 的必跑项或性能分母。
- 本地离线回归使用 `tools/benchmark/run-tiered-command-matrix.ps1 -Tier offline-large`，选择所有实际参数包含 `--source local` 的可执行 corpus，默认每个 case 运行 10 次、覆盖 independent-cold、shared-build-cold、warm、repeat-warm；该层用于充分验证本地 CASC、DB2/WDC 解码、关系查询、导出和缓存稳定性。
- 最终性能封存使用 `SourceMode=offline`，覆盖全部 local + neutral case、四种协议和每个 case 10 次；当前矩阵共 1240 candidate samples，只依赖离线本地执行结果。
- case 来源必须从实际执行参数中的 `--source local|remote` 解析；没有显式 source 的 neutral/help/维护 case 不得被误计入 local 或 remote 性能分母，必须单独记录覆盖状态。
- 分层报告必须分别记录 source mode、case 数、样本数、网络字节/请求、cache delta、失败与 fallback；online-small 的网络失败不能被离线大回归的本地结果掩盖。
