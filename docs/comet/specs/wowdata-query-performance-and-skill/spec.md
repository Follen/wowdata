# wowdata-query-performance-and-skill 完整目标规格

## 1. Skill 架构

仓库中的 `skill/wowdata/` 是发布与安装的唯一源。`SKILL.md` 保持精简，只包含目标解析、能力选择、执行边界与结果解释。命令表、SQL 方言、Hotfix 参数、表/locale/product 映射和详细故障处理放入 `references/`，所有引用最多一层。

Skill 必须把 `wowdata sql` 定义为复杂静态 DB2 查询的原子接口，覆盖 inline、file、stdin、parameters、json/jsonl/csv、EXPLAIN/ANALYZE。固定领域命令保留用于稳定领域输出。Hotfix 只能通过独立 `wowdata hotfix query` 使用。

Skill、references、`agents/openai.yaml`、npm 包内容和安装投影必须可验证一致。

## 2. Static SQL 性能

Static SQL 保持 Build-specific DBD binding、只读 static catalog、SQL/Hotfix 零耦合和现有结果语义。基础原子矩阵至少覆盖 point、batch、relationship、projection、selective scan、bounded scan、join、aggregate、distinct、top-k 和 streaming。

实现必须减少无关 target preparation、重复 schema/DBD/DB2 打开、全行 map 分配、重复表达式求值、非必要 materialization、全量排序和进程内冗余复制；物理算子与缓存必须有界、可取消、可度量。

## 3. 旧命令统一内核

`db2 rows/search/foreign-key/stream` 与 spell/encounter/item/creature/decor 的 DB2 数据访问继续通过 typed SQL AST 或同构 logical plan 进入统一 binder/planner/executor。`db2 schema` 可直接读取 metadata。file/icon 的 CASC/媒体行为不伪装成 SQL。

旧命令的命令名、参数、输出 schema、退出码、文件 hash 和领域格式保持不变。允许修复由冻结输入证明的旧错误或非确定性：过滤必须先于 limit，大小写无关搜索不得漏报，列表命令必须在 limit 前按稳定键排序。此类差异必须有定向回归并在 baseline/candidate 差分报告中单列，不得伪装成结果等价。

## 4. Hotfix 性能

Hotfix 继续拥有独立 query model、validator、source planner、filter、decoder 和 encoder。DBCache 查询优先磁盘点查/范围查与可复用 sidecar，不把完整历史常驻内存。Wago adapter 保留 raw HTML/embedded JSON、request identity、response SHA、coverage、精确 post-filter 和分页连续性。

优化覆盖本地 DBCache、sidecar build/open/reuse、Wago offline parsing/filtering、Wago cold fetch 和 warm cache hit，不允许 SQL provider 参与。

## 5. Blizzard CDN 下载性能

正式远端链路只使用 Blizzard CDN。优化可涉及 metadata 并发、连接与 Transport 复用、HTTP range 合并/分片、archive tail/root delta 读取、BLTE coalesce/decode、流式 SHA、续传状态和原子发布。

下载必须验证 Build/object identity 与最终 SHA；并发、连接、内存、句柄和临时空间有界；中断后只请求缺失分片；取消请求与重复 payload 必须计量。

测试不得通过全量产品遍历、完整 Build 下载、大批 archive 或高重复采样制造大规模 CDN 请求。range、resume、取消、错误响应、SHA 失败、临时状态清理和原子发布的完整矩阵必须由本地 HTTP fixture/模拟服务器验证。

确定性 50% 性能门禁与 post-fix CDN 回归全部使用本地 HTTP fixture，完整覆盖 range/resume/取消/失败矩阵；本 change 不再追加 Blizzard CDN 公网请求，只核对 2026-08-07 已执行的唯一一次兼容/身份/预算观察。该既有 live 不证明最新改动后的真实边缘行为，也不纳入性能门禁。runner 必须保留总 deadline、请求/传输预算、fail-fast 和单对象上限；未来显式 live 的总墙钟不得超过 5 分钟，超限对象必须在任何 payload GET 前拒绝。

## 6. 基准协议

冻结 baseline 为 `aa0fd3c293560effcf37338444f54169904c5311`。baseline 与 candidate 使用相同 runner、输入、机器、Build、locale、缓存状态、输出规范化和样本顺序。

SQL 使用本地客户端和冻结缓存，禁止网络。Hotfix 使用本地/DBCache/Wago offline，并附加有界 Wago live。CDN baseline/candidate 只在本地 HTTP fixture 上对固定对象交错执行；Blizzard 公网不追加样本，只核对唯一一次既有 live。未来显式公网运行仍受 5 分钟、请求/字节预算和 fail-fast 约束。

每个样本记录 binary SHA、Git revision、目标、输入 hash、缓存语义、wall/CPU、p50/p95、内存、alloc、磁盘与网络字节、请求数、结果 hash 和退出码。

## 7. 性能门禁

50% 提升按每域固定代表矩阵的物理路径整体 p50 判断。SQL 以进程内 top-k、hash-join、search 等物理算子矩阵，Hotfix 以本地 DBCache/sidecar/Wago offline 解析与查询矩阵，CDN 以本地 HTTP fixture 的固定 range/resume 矩阵作为性能门禁；三者分别计算 baseline 与 candidate 的整体 p50，candidate 必须至少降低 50%，三个域之间不得互相抵消。SQL CLI 端到端矩阵单独验证正确性、退出码、结果 hash、请求为零和无意外回退，并把进程启动与 DB2/DBD 装载固定成本单列；Wago/Blizzard live 只记录兼容性、coverage、SHA、请求/字节预算和缓存命中。

不要求每一个单独 case 的 p50/p95 都达到 2×，但关键 join/top-k/search、DBCache/sidecar/Wago offline、CDN fixture cold/resume 等物理路径 case 的 p95、正确性、资源上限和失败率不得回退。point/relationship 等已在亚微秒级的路径允许保持近似不变，但不得出现无意回退。报告必须同时列出逐 case 数值、live 兼容性和 CLI 固定成本，防止整体指标隐藏局部严重退化。

## 8. 回归

必须运行完整 Go test/race/vet、相关 fuzz、npm test/pack、Skill validator、11 类本地 SQL 矩阵、旧命令 local command matrix/golden/differential、Hotfix DBCache/Wago offline/live，以及本地 HTTP CDN 完整矩阵。CDN 50% 性能结论来自同机同输入的本地 HTTP fixture 配对基准；公网只核对 2026-08-07 唯一既有 live 的 Build、对象、SHA、预算和退出码，不要求或允许本 change 再发起 post-fix/配对/repeat live。

SQL 回归中任何 CDN/Wago/Raidbots 请求都导致失败。除规格明确允许并有测试证明的 correctness fix 外，功能结果必须等价；所有差异、未执行或未达标项目必须显式记录，不得计为通过。

## 9. Comet 文档版本控制

`docs/comet/` 下本 change 的 brief、完整目标规格、verification、正式报告引用和归档目录属于交付物，必须纳入 Git 并随实现提交。`.comet/config.yaml` 同样保持版本控制；Runtime 管理的选择、锁和事务状态只由 Comet CLI 更新，不手工改写。
