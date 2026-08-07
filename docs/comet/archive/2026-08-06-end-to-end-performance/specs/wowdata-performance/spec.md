# wowdata 端到端性能规格

## 目标

wowdata 必须在保持 CLI-only、数据正确性、Build 隔离和现有命令兼容性的前提下，对远程目标解析、CASC 下载与缓存、DB2 装载与关系查询、领域聚合及批量导出提供可观测、可恢复且有界并发的高性能执行路径。

## 本地优先与基准分层

CLI 必须支持在 HOME `.env` 中配置本地 World of Warcraft 安装目录。未显式传入 `--source` 时，目标解析先检查该本地安装是否包含请求的 product 与 Build；匹配时使用 `source=local`，不触发 CDN 网络请求。不匹配、目录不存在、文件缺失或本地数据损坏时，自动 fallback 到 `source=remote` CDN 路径，并在可观测字段中记录 fallback 原因。

显式 `--source remote` 必须始终走 CDN，不被 `.env` 改写；显式 `--source local` 可以省略 `--path`，由 `.env` 补齐，若仍无路径则返回稳定错误。local source 的身份必须包含规范化安装路径、product、Build config key 和 locale。`casc info` 与 `casc diagnose` 对 local source 只能读取 `.build.info`、Build config 与 CDN config，不得扫描本地 index、encoding 或 root；full `warmup` 与真实数据命令才加载必要数据。
显式 `--source remote` 必须始终走 CDN，不被 `.env` 改写；显式 `--source local` 可以省略 `--path`，由 `.env` 补齐，若仍无路径则返回稳定错误。local source 的身份必须包含规范化安装路径、product、Build config key 和 locale。`casc info` 与 `casc diagnose` 对 local source 只能读取 `.build.info`、Build config 与可选 CDN config，不得扫描本地 index、encoding 或 root；缺失 CDN config 不得阻塞 metadata-only 结果，格式损坏仍记录为 fallback 原因；full `warmup` 与真实数据命令才加载必要数据。

CDN 优化仍属于实现范围，但不再属于本次正式性能封存和 Verify/Archive 完成门禁。正式验收只使用 `offline` 范围：本机真实 local source 与不依赖 source 的 neutral case。远程 source 可作为后续可选诊断运行，不计入本次性能分母。

## Integration Benchmark 与 Golden 分层

现有 `fixtures/golden` 和 `internal/golden` 保留为轻量冻结语义基线，用于快速确认核心输出、错误契约和导出哈希没有漂移。新增 integration benchmark/test layer 必须成为主覆盖面：从真实 Cobra command tree 自动枚举每个 CLI 叶子命令，并按每个目标 Build、source 路径、locale、成功路径、缺失参数、无结果、代表性错误和真实输出 artifact 生成测试矩阵。

integration benchmark 必须比现有 golden 更丰富，用于主动发现读取 bug、奇怪字段无法解码、DB2/DBC schema 或 compression 组合遗漏、CASC/BLTE/BLP 边界错误、CLI 子叶未覆盖、领域聚合逻辑错误和性能退化。它必须记录 stdout/stderr/exit code、输出文件哈希、阶段 timing、CPU、heap/working set、GC、请求/连接、网络/磁盘字节、扫描/解码行数、cache delta 和 source fallback。任一命令子叶、Build 或文件等价类缺少 integration 覆盖时，最终报告必须列为 coverage gap；未知、损坏或暂不支持输入必须有稳定错误或修复证据，不能从分母静默移除。

integration benchmark 替代现有 golden 作为全量性能与 bug 回归主协议，但不得删除原有 golden。golden 继续用于小型、快速、版本化的冻结样本；integration benchmark 可把大型真实输出、corpus、原始运行结果和中间分析放入 `analyze/`，并通过 SHA-256 manifest、配额、TTL 和清理命令管理。

集成矩阵的规范为 `wowdata.integration-benchmark-matrix.v1`，由 `tools/benchmark/integration` 在运行时枚举 Cobra 叶子，读取 Build catalog（包括公开固定 Build 与本地 `.build.info` 发现的 Build），并展开 local/remote × independent-cold/shared-build-cold/warm/repeat-warm × success/missing-args/empty-result/representative-error。missing-args 和 representative-error 使用真实 CLI 解析/错误路径，可在无 corpus 时执行；success 或 empty-result 没有真实输入时必须保持 `coverageGap=true`，不得用 `--help`、预热结果或预生成输出填充。每个 cell 具有稳定输入哈希、隔离 HOME/cache/output/install 路径和 stdout/stderr/退出码/哈希/资源指标捕获字段。

## 目标解析与热启动

普通数据命令只解析用户指定的 product 和 region，不得为了单一目标探测全部已知 products。`latest` 的解析结果可以使用有明确失效规则的短期缓存，但执行结果必须记录最终解析的 Build 身份。

相同 Build、locale 和缓存身份的后续进程必须复用已经验证并发布的网络原始内容对象。热启动不得重复下载未变化对象，但生产缓存不得持久化 DBD 编译结果、CASC 解析结构、DB2 解码计划、行、关系索引或查询结果来替代本地引擎工作；这些步骤必须在进程内重建并计入解析与查询成绩。

## 并发下载与断点续传

远程大对象必须支持由 workers 配置约束的并发 HTTP Range 分片下载。分片大小和 worker 数必须有上限，不得因为单个任务无限创建请求、goroutine 或临时文件。

下载状态必须绑定规范 URL、Build/内容身份、期望长度以及可用的 ETag、Last-Modified 或内容键。每个完成分片必须记录范围和可验证状态；进程终止、网络失败或超时后，再次执行必须自动请求缺失或失效分片，而不是重新传输整个有效部分。

服务端不支持 Range、远端身份变化或本地分片状态不一致时，CLI 必须丢弃不可信的部分状态并安全回退到完整下载。所有分片完成后必须组合到临时对象，验证期望长度和内容身份，再通过原子替换发布；部分文件不得被普通缓存读取路径视为有效对象。

同一内容对象的多个进程必须共享一个下载发布者或等待其结果，不得重复下载相同字节。不同内容对象可以在全局并发预算内并行下载。机器可读 timing 必须报告总字节、已复用字节、新下载字节、分片数、重试数、worker 数和 Range 是否生效。

## 缓存格式升级

新缓存格式首次启用时必须清理旧的可重建缓存并从空缓存完整重建，不迁移或复用旧格式的 CASC 对象、下载分片、DBD/DB2 派生索引、listfile 或 TACT Key 缓存。清理范围只能是 wowdata 明确托管的 cache 和与旧缓存格式绑定的临时下载状态。

缓存格式清理不得删除配置、Profile、Build 选择状态、托管二进制、托管 Skill 或用户导出文件。清理必须受缓存格式版本标记约束，只在旧格式升级时执行一次；中断后重试必须保持幂等，并报告删除路径、回收字节和新格式版本。

## 依赖驱动准备

每个命令必须声明真实依赖，并只准备执行所需资源。已知 FileDataID 的 CASC 读取和图标导出不得加载 listfile；DBD manifest、DBD definition、TACT Key 和 DB2 表必须按命令和数据实际需要懒加载。

独立 DB2 表和独立缓存对象可以并发准备。并发度必须有界、可配置并在输出的 timing 信息中可追踪。

未显式配置 workers 时，CLI 必须根据可用 CPU、内存和任务类型采用自适应高性能并发预算。下载、DB2 装载和图片解码必须使用独立 worker pool，并受统一的全局并发和内存上限约束。用户显式设置的 workers 必须覆盖自适应默认值；执行结果必须报告每个 pool 的最终并发度和限制原因。

点查询的默认内存预算必须为 `min(max(384 MiB, availableRAM × 5%), 1 GiB)`，复合领域/导出为 `min(max(768 MiB, availableRAM × 8%), 2 GiB)`，corpus/warmup 为 `min(max(1 GiB, availableRAM × 15%), 4 GiB)`。调度器还必须同时受 CPU 核数、bandwidth-delay product、每任务 p95 工作集、文件句柄/连接预算、CDN 限流与错误率约束。

metadata、large-range、DB2 parse、BLTE decode、image decode/write 使用可独立计量的逻辑 pool，但共享一个全局资源调度器和内存令牌；禁止 pool 内再启动不受控 worker 造成并发乘法。执行结果必须报告每个 pool 的最终并发度、内存份额、限制原因和实际峰值。

## 查询计划与执行 DAG

复合命令必须在单一进程和单一目标初始化中构造依赖 DAG。节点至少区分目标解析、CASC 元数据、DBD definition、WDC 表、关系查询、Spell frontier、图标内容、解码和输出；相互独立的节点可以并发，依赖节点只能在输入就绪后启动。

同一节点或内容身份在一次执行中必须 singleflight，多个消费者共享结果。调度器必须支持 context 取消、背压、全局内存预算和公平 worker pool；不得通过为每个 ID 启动子进程实现并发。影响最终用户结果的关键路径节点优先于预取和低优先级导出节点。

执行计划必须输出机器可读摘要，包括节点数、关键路径、每节点等待/执行时间、解码行数、分配字节、HTTP 请求数和缓存命中。固定业务场景的计划必须可在测试中断言不存在意外全表扫描。

## WDC/DB2 算法

实现必须在 `internal` 提供内嵌、只读、Build snapshot 一致的 DB2 引擎。引擎是所有 DB2 和领域查询的唯一执行核心，不暴露 SQL、数据库监听端口或外部事务接口。

引擎必须包含以下边界：

- catalog：管理 Build、locale、table、DBD schema、字段类型、主键和 relationship 元数据；
- storage：以 `ReaderAt`/只读字节区间访问 WDC section、record、string table、copy table 和 relationship map；
- logical plan：表达 point lookup、multi-get、relation lookup、project、filter、search、order、limit、join、expand 和 traversal；
- physical plan：在主键定位、原生 relationship、紧凑临时索引和受控 scan 之间选择最低成本路径；
- executor：以 typed column batch 或等价紧凑批次执行算子，只在 CLI JSON 边界物化通用行；
- scheduler：在单机 CPU/内存预算内并行独立表、批次和解码阶段，支持取消、背压和错误传播。

引擎设计必须采用成熟只读查询引擎的通用原则：投影与过滤下推、late materialization、批量/向量化执行、紧凑列数据、稳定快照、基于可用索引和行数的轻量计划选择、顺序访问优先和有界 buffer reuse。实现不得引入 SQL parser、优化器搜索空间爆炸、事务写入、MVCC、WAL、分布式 shuffle 或面向服务器负载的大规模并发机制。

高级数据库工程方法必须参与内部架构与算法设计：catalog/schema 与 immutable snapshot 分离，logical/physical plan 分离，planner 使用可解释的特征/代价估计，executor 使用 typed column batch、late materialization、选择性算子融合和有界 buffer 生命周期；临时索引、关系访问结构和列批次只在进程内按查询生命周期存在。需要顺序扫描时必须显式记录原因，需要并行时必须由算子成本、数据规模、I/O/CPU 预算和背压共同决定。不得为了追求 benchmark 数字绕过快照一致性、内存所有权、取消传播或错误语义。

默认并发只面向单次 CLI 的少量独立表、批次和流水线阶段。查询规模较小时必须优先串行低开销路径；只有估算收益高于调度成本时才并行。worker 数同时受显式配置、自适应 CPU/内存预算和实际可运行节点数限制。

WDC reader 必须把 DBD schema 编译为可复用的字段解码计划，并支持字段投影下推。只请求 `ID,Name_lang` 时，不得先解码完整记录再丢弃其他字段。

WDC reader 必须保留按 record ID 定位记录和按原生 relationship map 定位外键记录的能力，并按请求懒解码字段。局部 ID 或关系查询不得调用 `GetAllRows`、构建全部 `map[string]interface{}` 行或在业务层扫描整表。

批量 ID 查询必须对 ID 去重、按底层 section/offset 排序读取并按稳定请求顺序组装结果。批量关系查询必须一次接受多个关系值，利用 relationship map 或本次表加载产生的紧凑倒排索引直接定位记录。

JournalEncounter 已提供 FirstSectionID，JournalEncounterSection 已提供 FirstChildSectionID 和 NextSiblingSectionID；Encounter 查询必须从这些指针按需读取章节树，而不是加载所有章节后筛选和重建父子关系。

Spell 查询必须以去重后的种子集合执行分层 BFS。每一层批量读取 Spell、SpellName、SpellEffect 和 SpellMisc 的目标记录及关系行，提取下一层 trigger/description 引用后继续；SpellCastTimes、SpellDuration 和 SpellRange 只能读取实际引用的 ID。解码量必须与访问子图大小相关，而不是与全表行数相关。

高频领域路径必须使用 typed row/column projection，避免反复构造 `map[string]interface{}`、反射式 JSON 结构和全字段对象。允许使用有界 buffer pool、预分配切片和批量编码，但不得因池化长期保留超过预算的大对象。

`db2 rows`、`db2 search`、`db2 foreign-key` 和 `db2 stream` 必须翻译为引擎逻辑计划。Spell、Encounter、Item、Creature 和 Decor 服务必须通过引擎 API 构造批量查询与 traversal，不得直接调用 `GetAllRows` 或自行维护第二套行扫描实现。

引擎必须提供内部 explain/stats 结构供 timing、测试和 benchmark 使用，至少报告逻辑算子、物理算子、索引选择、输入/输出行数、解码字段数、扫描行数、batch 数、分配字节和执行时间。该结构不要求新增用户可见查询语言，但必须能进入机器可读诊断和验证证据。

## DB2 引擎成熟性契约

### Snapshot 与生命周期

每次查询必须绑定不可变的 source、Build config key、locale、DBD revision 和已打开 table handle 集合。Snapshot 必须引用计数或采用等价的确定性生命周期；查询结束、取消或失败后必须释放 Reader、buffer、goroutine 和临时索引。Snapshot 关闭后访问必须返回稳定错误，不能 panic、悬挂或读取另一 Build 的数据。

并行查询可以共享不可变 table/storage 元数据，但每个查询的 cursor、batch、错误和取消状态必须隔离。表加载、关闭和查询并发执行必须通过 race 检测，不能依赖调用顺序避免数据竞争。

### 类型与 WDC 语义

引擎必须覆盖当前 reader 支持的所有 WDC/DBC 版本、section 布局、压缩模式、bitpacked 字段、pallet/common data、inline/non-inline ID、relationship、copy table、string table、数组、signed/unsigned 数值、float 和本地化字符串。

类型转换、缺失字段、零值、空字符串、加密 section、未知 schema、无匹配行和损坏记录必须具有明确且与现有 CLI 兼容的结果。Projection 不能改变数值符号、数组顺序、字符串内容或 ID 身份。

### 计划正确性与回退

Planner 的首要目标是正确性和有界资源，其次才是成本。主键、relationship 或临时索引不可用时，可以选择受控 scan，但必须保持 filter、order、limit 和重复输入语义，并在 stats 中记录回退原因。估算错误不得导致遗漏行、重复行、非确定顺序或无上限内存增长。

相同 snapshot、逻辑计划和输入必须产生确定的行顺序和字段顺序。并发度变化不能改变结果；需要恢复请求顺序的 multi-get 必须在物理读取排序后显式重排。

### Batch 内存所有权

每个 typed batch 必须定义所有权和有效期。零拷贝字符串或字节切片不得在底层 Reader、mmap、下载 buffer 或前一 batch 被释放或复用后继续暴露。跨 goroutine 传递必须只读或完成所有权转移。

Executor 必须设置 batch 行数、单 batch 字节数、查询总内存和临时索引内存上限。超限时应缩小 batch、关闭非必要并发或返回稳定资源错误，不得依赖 OOM 回收。

### 错误、取消与恢复

所有 operator 必须接受 context，并在阻塞 I/O、worker queue、解压、解码和输出边界检查取消。第一个致命错误触发计划取消后，其他节点必须停止并被回收；批量允许的单项错误必须按已确认的 partial-success 契约进入 manifest。

损坏 header、越界 offset、短读、非法压缩元数据、失效 relationship 和 DBD/WDC 不匹配不得 panic。错误必须包含 table、Build、section/record 或内容身份等可诊断上下文，同时避免输出敏感本地路径之外的信息。

### Schema 演进与兼容

Catalog 必须以 Build 和 DBD revision 选择 schema，不得跨 Build 静默复用解码计划。字段新增、删除、类型或数组长度变化必须使旧计划失效并重新编译；未知但不影响请求 projection 的字段不得迫使局部查询失败。

现有 DB2 CLI 和领域服务的 JSON 类型、字段存在性、空值约定、稳定顺序和错误码必须通过兼容层保持。内部 typed value 不得直接泄露为新的未约定 JSON 表示。

### 成熟性验证

必须使用至少两个真实 Build、两个 locale 以及每个 Build 中全部可加载 DB2 表建立 corpus。对相同 schema、ID、projection、filter、relationship、copy table 和代表性 scan，旧 reader/冻结基线与新引擎必须执行差分；规格未授权的差异为失败。

必须对 WDC header、section、compression、string、array、copy 和 relationship 输入运行 fuzz，并证明损坏输入只产生错误而不 panic。必须在 race 模式下覆盖共享 snapshot、并行 multi-get/relation lookup、取消、关闭和 buffer reuse，并检查 goroutine、文件句柄和临时内存泄漏。

必须提供 engine microbenchmark，至少测量 point lookup、batch get、relationship lookup、projected scan、search、small join 和 traversal 的 ns/op、allocs/op、bytes/op、扫描/解码行数。性能提升不能伴随 corpus 正确性或资源边界退化。

## CASC 解析与请求合并

实现必须提供可替换的 `FileLocator` 与 `ArchiveLocator` 接口。对每个 Build 先探测并解析 CDN config 中的 `file-index`、`patch-file-index`、`archive-group`、`patch-archive-group` 与 `archives-index-size`，按真实格式能力选择定位算法，优先建立 `FileDataID -> content/encoding key -> archive/offset/size` 的直接定位链。不得假定所有 Build 使用同一格式；未知或损坏结构必须产生可诊断结果并走保守回退。

支持直接定位的 Build，普通点查询不得下载完整 root、完整 encoding 或扫描全部 archive index。格式不支持或目标未命中时，回退必须按 locator 证据、目标 key 分桶和请求预算执行有界 archive-index 探测；不得默认获取全部 archive index。报告必须区分直接命中、增量定位、有界回退和全量准备的请求数、字节数与原因。

DBD manifest、Build/CDN config 与 locator 元数据必须在无依赖冲突时并行获取。已知命令依赖作为第一波请求；查询发现的 Spell、Icon 与 FileDataID 组成第二波增量计划。DB2 和资源请求按 archive/offset 排序，并依据 RTT、吞吐、额外字节与内存预算自适应合并 Range。

root、encoding 和 archive 索引解析必须使用紧凑键类型、预分配切片或开放寻址/排序索引，避免为数百万条记录分配字符串键和通用 interface map。可并行的独立 section 可以在内存预算内并行解析。

同一 archive 中相邻或重叠的 DB2、图标和其他内容范围必须由请求规划器排序，并在额外传输成本低于独立请求延迟成本时合并为较少的 HTTP Range。请求完成后以只读切片分发给消费者；合并阈值必须根据 RTT、吞吐、额外字节和全局预算自适应。

HTTP transport 必须复用连接，支持 HTTP/2 时启用多路复用，并对超时、429、5xx 和短读使用有上限的指数退避。下载、BLTE 解压、WDC 解析、BLP 解码和写出必须组成带背压的流水线，避免各阶段顺序物化完整中间结果。

## DB2 查询与关系索引

DB2 存储必须支持批量 record ID 和批量关系值查询。SpellEffect.SpellID、SpellMisc.SpellID、JournalEncounterSection.JournalEncounterID 等领域关键关系必须使用文件原生关系信息或本进程加载期建立的紧凑索引，业务查询不得通过读取完整表后在服务层筛选来回答局部请求，也不得读取持久化解析索引来取得热路径成绩。

Spell 关系遍历必须按 BFS frontier 批量读取当前层的名称、描述、效果、杂项、施法时间、持续时间和距离，仅继续加载实际发现的引用。Encounter 查询必须只读取目标首领的章节，并对技能 ID 去重后批量解析。

现有单 ID 查询必须保持可用；批量查询必须维持稳定顺序、明确重复 ID 行为，并为无效 ID 返回机器可读结果。

## 缓存与并发

缓存读取在对象已完整发布后必须无目标级写锁。写入协调必须细化到 Build 和内容对象；同一对象只允许一个发布者，不同对象能够并发下载、解析和发布。

所有缓存对象必须先写入临时路径，验证长度与内容身份后原子发布。完整性元数据不得依赖多个进程无协调地覆盖同一个整体 JSON 文件。进程崩溃、超时或取消不得留下会被后续读取当作有效缓存的半成品。

## 存储预算

生产缓存只能保存一份以内容身份命名的网络原始对象、必要续传状态和验证/寻址所需的小型元数据。Build 视图只保存内容引用，不复制原始 payload。DBD/DB2/CASC 的编译计划、解析结构、索引、完整解码行、查询结果、完整 JSON 快照以及每 Build 重复 payload 禁止持久化。

Range 续传必须优先使用单个预分配稀疏 `.part` 文件原位写入分片，并以 bitmap/区间 sidecar 记录完成范围；完成后在同一文件系统直接验证并 rename，不能为每个分片创建 payload 副本，也不能在拼装阶段再复制一份完整对象。

cache、resume 和 derived metadata 必须分别计量并共同服从现有 CacheMaxBytes 硬上限。成功稳定状态下，除唯一内容 payload 外的磁盘放大不得超过唯一 payload 的 5%，且 derived metadata 默认不得超过 `min(512 MiB, CacheMaxBytes 的 5%)`。活动下载允许一个目标对象大小的 `.part` 占用，但必须计入配额；空间不足时必须在下载前返回机器可读错误。

临时状态必须有 TTL。清理与裁剪只能在发生写入、跨越配额水位或显式缓存命令时运行，不得让纯读取热路径扫描整个缓存树。`cache status` 和 benchmark 必须报告各类别字节数、去重节省量、磁盘放大率和本次执行磁盘增量。小型元数据只能用于内容身份、完整性、续传和淘汰管理，不能包含可直接回答 DB2/DBD/CASC 解析或查询的派生结构。

## 批量领域命令

CLI 必须提供单进程批量 Spell 查询、批量图标导出和 Encounter 复合导出能力。复合 Encounter 执行必须能够通过副本名称与首领序号定位目标，返回完整技能数据，并向指定目录导出每个技能的图标与 manifest。

批量图标导出必须按 FileDataID 去重读取和解码，但去重不得改变上层语义基数。不同技能或 Aura 即使共享相同 FileDataID，也必须作为独立语义条目，使用各自的语义类型、记录 ID 和本地化名称生成独立命名输出并分别写入 manifest。共享图标允许一次下载和一次解码后扇出到多个输出，不得按 FileDataID 合并、覆盖或遗漏逻辑条目。

输出 manifest 必须至少包含 Build、locale、首领 ID、语义类型、技能或 Aura ID、本地化名称、FileDataID、输出路径、格式、尺寸、字节数和 SHA-256，并明确表达多个语义条目到同一 FileDataID 的多对一关系。默认文件名必须以语义记录 ID 和本地化名称为主体；清理非法路径字符后发生冲突时，必须添加稳定且可重复的语义类型或序号后缀。

批量任务发生单项查询、下载、解码或写出失败时，必须保留其他已经完整验证的成功输出，在 manifest 中为每一项记录成功或稳定错误，并以非零退出状态结束。再次执行相同任务时必须只重试缺失项和失败项，不得删除或重新计算仍与 manifest、Build 和内容哈希一致的成功项。

重复导出时，只有语义类型、语义 ID、本地化名称、Build、FileDataID、格式和 SHA-256 均与 manifest 匹配的已有文件才能直接复用。不匹配、缺少 manifest 证据或内容损坏的同名文件必须写入临时路径并原子替换，不得盲目跳过或直接覆盖正在使用的有效文件。

## Skill 编排

wowdata Skill 必须优先选择一次完成用户业务目标的批量或复合命令，不得在批量能力存在时为每个技能或文件启动独立 CLI 进程。PowerShell 中逗号分隔参数必须作为单一带引号参数传递。

当运行旧版 CLI 时，Skill 可以回退到单次批量 DB2/stream 查询、FileDataID 去重和受锁模型约束的顺序导出，但不得用会被目标级锁串行化的多进程 fan-out 伪装并发。

## 可观测性与基准

CLI 必须提供机器可读的阶段 timing 与缓存命中信息，至少覆盖目标解析、CASC 初始化、索引恢复、DBD/DB2 装载、查询、下载、解码和写出。网络下载必须记录字节数、worker 数和持续时间。

仓库必须提供隔离缓存、固定输入和固定输出校验的端到端 benchmark，测量 local source 的 independent-cold、shared-Build-cold、warm 和 repeat-warm，并报告 p50/p95。neutral case 同样覆盖四种协议以验证隔离状态和稳定性。远程 CDN benchmark 不进入本次完成门禁。

每次正式运行前必须校准并记录物理核、逻辑核、GOMAXPROCS、可用内存、顺序/随机磁盘吞吐与延迟、CDN RTT/连接建立/HTTP 协议/单连接及多连接吞吐、GitHub/DBD 源 RTT 与吞吐、BLTE/WDC/DBC/DBD/BLP 解码吞吐、SHA-256/JSON/文件写入吞吐，以及文件句柄与连接预算。

网络调度校准必须按实际资源类隔离 workload。metadata 候选使用无需全量预热的按需点查询；large-range 候选使用真实按需数据命令触发的单个大 DB2 或资源对象；chunk/resume 候选使用与生产一致的单对象 Range 下载、原位 `.part`、完整写盘、SHA-256 和原子发布链。显式 full `warmup` 只评估 `warmup/corpus` 命令自身，不得用于选择 point-query 或 composite/export 的 metadata、large-range worker 或 chunk 默认值。

任何 scheduler tournament 若在目标数据命令前隐式执行 full `warmup`，或把无关 corpus payload、解析与工作集计入候选比较，必须标记为 rejected，不得进入 selection、理论下界或最终性能成绩。最终报告必须保存 rejected 原因、原始证据路径和替代 workload，并证明数据命令 cold 样本从空 HOME 独立完成真实按需 DAG。

正式 command matrix 的 current 与冻结 baseline 命令行都不得包含弃用的 `--auto-warmup` 兼容旗标；两者的 independent cold 必须从空 HOME 使用相同参数直接执行真实按需 DAG。冻结 baseline 若不支持按需执行，必须明确记录 unsupported 或 mismatch，不得通过独立 baseline 参数、setup 或隐式 warmup 注入全量预热。

每条命令必须构造真实执行 DAG，并按 critical path 计算 `T_floor = process_start + critical_path_RTT + max(required_unique_network_bytes / calibrated_network_throughput, required_disk_bytes / calibrated_disk_throughput, required_CPU_work / calibrated_parallel_CPU_throughput) + unavoidable_output_cost`。网络、解析、解码与写盘可以重叠时不得简单相加。

主要门槛为 p50 wall time 不超过 `1.25 × T_floor`、p95 不超过 `1.50 × T_floor`，p99/max 不得有无解释长尾；`required unique bytes / transferred bytes >= 95%`，重复 payload bytes 为 0，持久化解析结果读取为 0，repeat-warm cache delta 为 0，输出与 integration benchmark/golden/哈希完全一致。任意大小的 file export、db2 stream 与 warmup 使用 `T_actual <= fixed_overhead + bytes / target_throughput`，并分别报告 TTFB、稳态吞吐与结束开销。

正式协议使用 `SourceMode=offline`，只选择 local 与 neutral case。Independent cold 为每条命令创建全新 WOWDATA_HOME/cache/output，证明不存在隐藏 warmup 前提；Shared-Build cold 从空 cache 开始按真实本地工作流运行全部选中命令，拆分首次公共依赖成本与每条命令增量成本。每个固定输入至少运行 10 次并报告 p50/p95/max、CPU、峰值 heap/working set、GC、磁盘字节、阶段时间与理论下界效率；当前矩阵为 3 个 local case 与 28 个 neutral case，共 31 cases、1240 candidate samples。

实现与验证不得读取、复用或分析 Claude 的实现，也不得运行 Claude 对照编排来调整实现。性能工作必须只依据本项目的规格、真实剖析结果、固定 SLA 和可复现基准独立完成。

Build、验证与归档期间允许调用子 Agent 并行编排相互独立的任务以提升执行效率。所有子 Agent 必须继承主 Agent 当前使用的模型与 thinking effort 设置；主 Agent 负责整合结果并确保共享工作区中的修改、验证证据和 Comet 状态一致。

性能成绩必须来自真实 wowdata 二进制、本机真实 World of Warcraft 安装、固定本地 Build 和真实输出文件。mock、fake store 和内存 fixture 只能用于单元测试，不能作为 cold、warm 或并发性能证据。实测记录必须包含原始命令、环境摘要、缓存目录状态、p50/p95、CPU、峰值内存、磁盘增量、退出状态和输出哈希。

该 benchmark 作为公开基准测试竞赛提交时，必须发布固定 protocol、版本/构建、硬件与 OS 摘要、Build/locale、隔离缓存初始化、重复次数、随机性控制、原始 timing、输出哈希、golden 结果、失败/回退记录和复现脚本。评测方能够从公开证据重建成绩；隐藏预解析结果、改变输入口径、吞掉失败、降低输出质量或依赖未声明的持久化解析缓存都使成绩无效。实现保持独立研究，不读取或对比任何竞争实现。

## 全命令回归

仓库必须自动枚举 Cobra 暴露的全部现有命令，并为每个命令覆盖成功路径、主要参数分支、缺失参数、无结果和代表性错误。优化前的已发布版本或冻结基线产物必须与新实现使用相同 Build、locale 和输入运行。

命令覆盖必须由实际 command tree 生成，不得用手写列表充当完整性来源。offline 范围内每个叶子命令都必须有 local/neutral independent-cold、shared-Build-cold、warm、repeat-warm 记录或明确非目标原因；目标数据命令覆盖本机安装实际存在的 Build/locale。远程 source case 明确排除在本次正式分母之外。破坏性命令必须使用完全隔离的 HOME、cache、Profile 与测试安装目录。

回归比较必须覆盖 stdout JSON 的语义内容、字段存在性与类型、稳定顺序、stderr 进度契约、退出状态、导出文件字节或规范化哈希，以及缓存和 Profile 的用户可见状态变化。仅 timing、明确新增的可观测字段或规格授权差异可以不同；其他差异必须阻止验证通过。

file、icon、db2、spell、encounter、item、creature、decor、casc、profile、cache、doctor、update、uninstall、video 和 golden 命令组都必须纳入矩阵。涉及破坏性维护命令时必须使用隔离 HOME/cache 和测试安装，不能修改开发者真实用户数据。

真实端到端实测至少覆盖：空缓存首次下载、热缓存重跑、第二次热重跑、多个独立请求并发、同一对象竞争、25%/50%/90% 下载中断恢复、损坏 `.part`、不支持 Range 的服务端回退、缓存配额不足以及共享 FileDataID 的多个技能/Aura 输出。

## 正确性与兼容性

优化前后在相同 Build 和 locale 下必须返回相同的领域 ID、技能关系和本地化文本。现有命令、JSON 字段、退出状态及缓存 Build 隔离契约必须保持兼容。

性能优化不得通过跳过完整性验证、隐藏失败、减少结果、降低图片质量或复用错误目标实现。基准失败或输出哈希不匹配必须使验证失败。

## 全量 Golden 与文件族覆盖

回归系统必须从 Cobra 命令树、参数定义、目标 Build 清单和真实 corpus 自动生成 manifest，而不是维护一份容易过时的手写清单。每个现有命令至少覆盖成功、主要参数分支、缺失参数、无结果和代表性错误；破坏性命令使用隔离 HOME、cache、Profile 和测试安装。integration benchmark 比较必须覆盖 stdout JSON 的语义字段/类型/顺序、stderr 契约、退出状态、导出文件规范化哈希、用户可见缓存状态、source fallback 和性能指标；golden 比较保留轻量冻结样本职责。

固定真实 Build 和至少两个 locale 的全部可加载 DB2/DBC 文件必须先做结构普查。分类键至少包含文件族与 magic/version、WDC/DBC 版本、section 数量与记录布局、定长/稀疏记录、每字段 compression、pallet/common、copy、relationship、string、array、locale 变体、数据规模和请求访问模式；CASC root/encoding/archive、BLTE chunk、BLP 变体、视频/导出格式也必须纳入实际出现的文件族清单。每个实际文件只能归入一个稳定等价类；未知、损坏、加密或暂不支持的输入保留在 manifest 中并要求稳定错误或明确修复证据，不能静默从分母移除。

冻结的小型语义 golden 和哈希元数据存放在 `fixtures/golden` 并版本化；integration benchmark 的大 corpus、完整实际输出、分析中间体和原始 benchmark 存放在 `analyze/`，用内容寻址 manifest、配额、TTL 和清理命令管理。`analyze/` 默认不进入版本控制，不得复制生产 payload，也不得被生产 CLI 作为解析结果或索引缓存读取。正式 benchmark 必须声明其缓存状态，并在测量解析/查询时清除会改变成绩的中间体。

## 自适应算法选择与最优性证据

Shape/规格不得为所有文件固定单一解析或并发算法。对每个文件等价类和访问模式，先列出合理候选（例如顺序读取、分段读取、边界扫描后并行、列投影、late materialization、稀疏/密集/哈希索引和不同批次大小），所有候选先通过相同 golden、错误、顺序、取消和资源上限门禁，再在同机同输入同网络/缓存状态下重复测量 p50/p95、CPU、allocs/bytes、峰值内存、解码/扫描行数、I/O 和临时磁盘。

实现必须保存候选原始结果、样本量、置信/噪声处理、选择阈值和最终 dispatch 规则。dispatch 只依赖可观测文件特征与请求形状；小任务优先低调度开销，未知特征采用正确性优先回退。用未参与选择的 holdout 文件验证规则，防止只对训练 corpus 调参。统计上持平时选择内存更低、尾延迟更稳、实现复杂度更小者。所谓“绝对最优”仅在该固定 corpus、环境、SLA 和资源约束下以可复现的候选 tournament 证据定义；没有证据的理论最优声明不计入验收。

## 分析产物与最终报告

验证必须生成一份可审阅报告，至少包括：优化前冻结基线与优化后成绩、环境/Build/locale/缓存身份、命令和输入、样本数量与文件族覆盖矩阵、golden 通过率、候选算法原始表与 dispatch 规则、cold/warm/repeat-warm p50/p95、关键路径、CPU、allocs、峰值内存、解码/扫描行数、网络字节/请求/Range 合并、磁盘增量/放大率、失败与回退、断点恢复结果、已知限制和完整复现命令。报告引用 `analyze/` 中 SHA-256 manifest 及原始证据；任一缺失证据、覆盖缺口、语义差异或资源超限都使 Verify 失败。

总报告必须按自动枚举的命令排序，并为每项列出 baseline、optimized、`T_floor`、效率、内存、请求数、integration benchmark 结果和 golden 结果；单独列出仍高于 `1.5 × T_floor` 的命令及准确 critical path。全量 integration benchmark、golden、Go test、race、npm test、pack check、fuzz 和双 Build DB2 corpus 必须全部通过。

完成条件还包括 Comet Verify 与 Archive 成功、变更推送到远端、仓库 CI 全部通过，以及发布一个新的公开 npm 版本。发布后必须从 registry 读取并核对版本、tarball 完整性和干净环境安装/基础命令结果；任何发布或安装验证失败都不得记录为完成。
## 分层回归与性能封存

日常回归必须按实际执行 source 分层，避免把网络等待时间混入每次 bug 修复的反馈周期：

- `online-small` 远程层保留为可选诊断，不是本次 Verify/Archive 的必跑层，也不进入最终 p50/p95 或完成门禁。
- `offline-large` 只选择显式 `--source local` 且本机 Build catalog 证明有真实 corpus 的 case，默认每个 case 运行 10 次并覆盖 independent-cold、shared-build-cold、warm、repeat-warm。该层验证本地 CASC/DB2/WDC 读取、关系遍历、导出、缓存重复执行和资源边界。
- `formal-seal` 使用 `SourceMode=offline`，运行自动枚举得到的全部 local + neutral case、全部四协议和每个 case 至少 10 次；当前矩阵共 31 cases、1240 candidate samples，并以该结果作为本次最终性能报告和公开证据。
- runner 必须按 case 参数中的 `--source` 记录 `sourceMode`；neutral/help/维护 case 不得静默归入 local 或 remote，必须以独立覆盖记录表示。
- 三层均需输出独立报告和原始证据路径；报告必须分别统计网络失败/重试/fallback、唯一与重复 payload、cache delta、golden/输出 hash 和退出状态。
