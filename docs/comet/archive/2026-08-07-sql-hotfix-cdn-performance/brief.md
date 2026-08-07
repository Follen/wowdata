# Outcome

交付一个可版本化、可安装且更精简的 wowdata Skill，并把 `wowdata sql` 作为首选原子查询接口写入 Skill；同时针对 Static SQL、独立 Hotfix 查询和 Blizzard CDN 下载三条链路完成可复现的性能优化、正确性回归与基线对比，目标性能提升 50% 以上。

# Scope

- 以仓库内 `skill/wowdata/` 为 Skill 唯一版本化源，按 skill-creator 规范减薄 `SKILL.md`，把 CLI 命令表、SQL 方言/示例、Hotfix 查询、输出格式和故障处理细节移动到一层 `references/`；同步校验 `agents/openai.yaml` 和安装投影。
- Skill 明确支持 `wowdata sql` 的原子查询：inline SQL、`--file`、`--stdin`、`--param`、JSON/JSONL/CSV、EXPLAIN/ANALYZE，以及何时优先 SQL、何时保留固定领域命令；Hotfix 继续使用独立 `wowdata hotfix query`，不得伪装成 SQL catalog。
- 优化 Static SQL 基础原子查询的 CLI 端到端与引擎热路径，覆盖 point、batch、relationship、projection、selective/bounded scan、join、aggregate、distinct、top-k、streaming 和旧固定命令的 typed-query 适配。
- 优化 Hotfix 本地 DBCache 点查/范围查、sidecar 构建与复用、Wago offline parse/filter/cache，以及有界 Wago live cold/warm 查询；保持 SQL/Hotfix 双向解耦。
- 只使用本机 WoW 客户端与冻结本地缓存执行 SQL 全量功能、差分、旧命令 golden/command matrix 和性能回归；SQL 验证不得访问 CDN、Wago、Raidbots 或其他网络源。
- Hotfix 执行完整本地 fixture/DBCache/Wago offline 测试，并执行有界 Wago live cold/warm 兼容与缓存验证。
- 优化 Blizzard 远端 CDN 的 metadata、range、archive、root/encoding、BLTE 和落盘/续传链路；确定性 baseline/candidate 性能门禁只使用本地 HTTP fixture，目标下载物理路径时间降低 50% 以上。Blizzard 公网只保留 2026-08-07 已完成的唯一一次兼容/身份/预算观察，后续 Build/Verify 不追加 post-fix、配对或 repeat live。
- 扩展并固化 benchmark/report，使 baseline 与 candidate 绑定 binary SHA、Git revision、机器、网络区域、Build、对象 hash、缓存语义、样本顺序、p50/p95、CPU、内存、请求数、传输字节、结果 hash 和退出码。
- `docs/comet/` 下本 change 的 brief、完整规格、verification、证据索引与最终 archive 必须随实现纳入 Git，不得被 `.gitignore`、仓库 exclude 或提交范围遗漏。

# Non-goals

- 不把 Hotfix 接入 Static SQL catalog，不恢复 `effective.*` 或 `hotfix.records` SQL。
- 不改变现有 CLI 命令名、参数、JSON schema、退出码和领域格式化契约；允许修复有冻结输入和回归测试证明的旧查询错误或非确定性，且必须在差分报告中单列。
- 不为了 SQL 回归从 CDN 下载 DB2/DBD/CASC；缺少本地 fixture 时先生成并冻结本地测试输入。
- 不以降低校验强度、跳过 SHA/Build 身份、扩大不受控并发或隐藏失败来换取性能数字。
- 不承诺对所有网络、所有地区和任意 CDN 时段都固定提速 50%；正式结论绑定同机、同输入、配对顺序和证据环境。
- 不在测试中大规模请求 Blizzard CDN，不抓取完整产品/Build/大批 archive，不用高重复次数或全量 warmup 放大网络时间与流量；完整协议、续传、失败和边界测试使用本地 HTTP fixture/模拟服务器完成。
- 不在 Shape 阶段直接修改全局安装 Skill；实现先修改仓库源，验证后再通过受控安装/同步证明投影一致。

# Acceptance examples

- 用户要求查询多个 DB2 表并做过滤/连接时，Skill 直接生成 `wowdata sql`，使用明确 target、参数绑定和输出格式；简单稳定领域结果仍可选择固定命令。
- `wowdata sql "SELECT ID, Name_lang FROM SpellName WHERE ID=:id" --param id=1 --source local ...` 在网络 provider spy 下调用数为 0，并与冻结 baseline/旧 `db2 rows` 等价结果 hash 一致。
- 本地 SQL 矩阵覆盖全部 11 类原子 case、完整旧命令 golden/command matrix 和 independent-cold/shared-build-cold/warm/repeat-warm；除已确认的正确性修复外保持结果等价，所有差异逐项解释并以回归测试锁定。
- Hotfix DBCache 点查不把完整历史载入内存；sidecar 可复用且损坏时安全重建；本地 DBCache/Wago offline 物理路径作为性能门禁，Wago live 只验证一页精确过滤、coverage、response SHA 和 repeat-warm 不新增网络请求或 cache bytes。
- CDN 的确定性本地 HTTP fixture range/resume 物理路径在当前 main `aa0fd3c` 与 candidate 间交错执行，candidate 的正式目标时间至少降低 50%，且 p95、重复字节、取消请求、内存和失败率满足门禁；Blizzard live 只核对 2026-08-07 已执行的唯一一次兼容/身份/预算记录，不要求 candidate 修改后再次执行。任何未来显式 live 都受 5 分钟、请求/字节预算与单对象上限保护，超限对象在 payload GET 前拒绝。
- Skill 源通过 `quick_validate.py`，`agents/openai.yaml` 与 `SKILL.md` 一致，npm dry-run 包含完整 Skill references，安装投影与仓库源 hash 一致。

# Constraints and invariants

- 冻结基线为 `main` 提交 `aa0fd3c293560effcf37338444f54169904c5311`；candidate 位于 `comet/sql-hotfix-cdn-performance`。
- 工作目录固定为 `D:\Code\wow\wowdata\.worktrees\sql-hotfix-cdn-performance`。
- 当前 SQL 基线：point warm p50 约 224.7 ms，relationship 约 322.9 ms，join 约 675.0 ms，top-k 约 2729.3 ms；完整报告为 `internal/hotfix/testdata/reports/local-db2-performance-report.json`。
- 当前 Wago live cold 单页约 1095 ms，repeat-warm 约 0.52 ms；Wago 是补充来源，live 网络波动必须用配对/交错样本控制。
- 性能优化前必须生成新的冻结 baseline，不能只复用旧报告；baseline/candidate 必须使用同一 runner、同一输入与同一缓存语义。
- SQL、Hotfix、CDN 三类报告独立，不用一个域的超额提升抵消另一个域的退化。
- 所有优化保持结果 hash、错误码、coverage/provenance、原始响应 SHA 和缓存身份语义。
- 并发、内存、文件句柄、连接数和临时磁盘必须有界；取消与第一个错误必须传播；不得产生重复 payload。
- CDN live runner 必须有总计时器、请求数/传输字节预算和 fail-fast；达到 5 分钟硬上限立即取消并把超时记为失败，不能继续后台请求。

# Decisions

- 使用 Native worktree 隔离：分支 `comet/sql-hotfix-cdn-performance`，目标分支 `main`。
- Skill 版本化源是仓库内 `skill/wowdata/`；全局 `.agents/skills/wowdata` 仅作为安装投影验证。
- 主 `SKILL.md` 只保留触发、目标解析、SQL/固定命令/Hotfix 的选择规则、执行安全边界和结果读取；完整命令与方言细节进入一层 references。
- SQL 回归只走本地客户端/冻结本地缓存，不访问 CDN。
- Hotfix 保持独立查询体系，并同时覆盖本地与 Wago offline/live。
- CDN 性能只使用 Blizzard CDN，不用第三方镜像替代正式下载路径。
- Comet 正式文档与归档是交付物的一部分，和代码、Skill、测试报告一起提交；`.comet/config.yaml` 保持版本控制，Runtime 管理的临时选择/锁文件不手工编辑。
- 性能门禁按 SQL 物理算子、Hotfix 本地路径、CDN 本地 fixture 三个域分别计算固定代表矩阵的物理路径 p50；每个域 candidate 相对冻结 baseline 的物理路径 p50 至少降低 50%。SQL CLI 端到端矩阵另外验证正确性、退出码、结果 hash 和无意外回退，并单列进程启动与 DB2/DBD 装载成本；Wago/Blizzard live 只作有界协议与缓存语义观察，不用网络波动抵消本地性能数字。
- CDN 正式性能回归与 post-fix 验证全部采用本地 HTTP fixture 的固定对象和交错 baseline/candidate 样本；唯一一次既有 live 仅保留兼容、身份、SHA、预算与退出码证据，不追加公网请求。未来若由用户另行显式运行 live，runner 仍强制 5 分钟总 deadline、请求/字节预算与 fail-fast。
- 用户已重新确认变更后的 Build 合同：拒绝在测试中追加大规模或重复 CDN 公网请求；本 change 只核对唯一一次既有 live，并以本地 fixture 完成性能与 post-fix 协议覆盖。
- 兼容性按接口与正确语义判断，不要求复制 baseline 的已证实错误：`WHERE` 必须先于 `LIMIT`，`db2 search` 保持大小写无关，`decor list` 在截断前按稳定键排序；这三类差异必须在报告中明确标为 correctness fix，不计作未解释回归。

# Open questions

- 无。

# Verification expectations

- Skill：`quick_validate.py`、metadata regeneration/check、reference link/check、npm test、`npm run pack:check`、安装投影 hash 对比和真实提示词 smoke。
- SQL：全量 `go test ./... -count=1`、`go test -race ./... -count=1`、`go vet ./...`、DB2 fuzz、11 类本地功能/性能矩阵、完整 local command matrix、golden compare、旧固定命令与 SQL 差分、网络调用为 0。
- Hotfix：包测试/race、DBCache 多版本 fixture、sidecar/磁盘二分、Wago offline SHA corpus、Wago live bounded cold/warm、缓存稳定、分页/漂移/协议变化错误。
- CDN：本地 HTTP fixture 使用 baseline/candidate 独立二进制完整覆盖 range/resume/cancel/hash/原子发布、固定对象、交错顺序以及 p50/p95、请求/取消/重复字节、CPU/内存与失败率；公网只核对 2026-08-07 既有唯一 live 的 Build、对象、SHA、预算和退出码，不新增样本。最新改动后的真实边缘节点行为未复验，作为已接受的剩余风险明确记录。
- 最终报告必须给出每个域的 baseline、candidate、加速比、样本量、复现命令和原始证据路径；未达到的 50% 项不得写成通过。
