# Outcome
在不改变 CASC 下载结果、缓存身份、断点续传、校验和失败回退语义的前提下，优化远端对象下载路径，并以本地 fixture 给出 baseline 与 optimized 的 P50/P95、请求数、失败率、峰值内存和输出 SHA-256；最终结论必须明确 P50 优化百分比。

# Scope
- 对有效且身份稳定的 resume state 消除重复 HEAD；身份不完整或发生漂移时保留 HEAD 并丢弃旧状态。
- 小对象使用单次完整 GET；大对象继续使用受 scheduler/内存/句柄预算约束的 Range 下载。
- 按对象大小和网络测量选择 worker/chunk，合并可安全合并的相邻缺失 Range，并按原偏移写回。
- 保持每块 hash、取消、重试、Range 被忽略/长度错误/服务端错误的完整 GET 回退。
- 增加本地 HTTP fixture、断点恢复、身份漂移、Range 合并/不合并、失败回退和性能矩阵证据。

# Non-goals
- 不修改 DB2、Hotfix、SQL 查询语义。
- 不绕过 URL、大小、ETag、Last-Modified、SHA-256、缓存配额或 scheduler 限制。
- 不以盲目增加并发换取单次吞吐；不进行公网大规模 benchmark。

# Acceptance examples
- baseline 与 optimized 输出字节及 SHA-256 完全一致。
- 有效 resume state 不产生重复 HEAD；身份漂移重新确认并重取失效区间。
- 小对象请求数为 1；相邻缺失区间合并后请求数下降；跨已完成区间不得合并。
- Range 被忽略、Content-Range 错误、长度不符、超时和取消均保持原有回退/恢复语义。
- 自适应策略在固定策略基线之上不劣，报告 P50/P95、请求数、失败率、峰值工作集和输出 SHA，并计算 `P50 improvement = (baselineP50-optimizedP50)/baselineP50*100%`。

# Constraints and invariants
- 任何身份变化、分块参数变化或策略版本变化都不能复用旧 resume state。
- 写回必须保持原始对象布局；完整对象发布前必须通过长度和 hash 校验。
- 全局 scheduler、连接/文件句柄、内存、缓存配额和取消语义保持硬边界。

# Decisions
- 使用自适应策略作为默认路径，固定 1/4/8 MiB 与 1/2/4/8 workers 作为本地对照矩阵。
- 优先降低 RTT、请求数和端到端 P50；P95、失败率、内存和输出 SHA 同时作为门槛。
- 只在当前新工作树 `D:\Code\wow\wowdata\.worktrees\casc-adaptive-download-v2` 实施。

# Open questions
- 无。

# Verification expectations
- `go test ./... -count=1`
- `go test -race ./internal/casc ./internal/runtime ./internal/db2 ./internal/wowdata`
- `go vet ./...`
- 本地 HTTP fixture 的功能、恢复、回退和性能矩阵。
- verification.md 记录 baseline/optimized 的 P50/P95、优化百分比、请求数、失败率、峰值工作集和输出 SHA。
