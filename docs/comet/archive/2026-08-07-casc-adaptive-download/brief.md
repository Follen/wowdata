# Outcome

让 CASC 远端下载在不同 CDN、对象大小和网络条件下自动选择更合适的请求策略，减少不必要 RTT 与 Range 请求，同时保持断点续传、内容校验、缓存身份和失败回退行为不变。

# Scope

- 为大对象下载增加基于对象大小、历史测量和当前响应的自适应 Range worker 策略；不得简单放大固定并发。
- 对已有稳定缓存身份的不可变 CASC 对象消除重复 HEAD；小对象直接 GET，避免 HEAD + 单 Range 的额外往返。
- 合并可安全合并的相邻 Range 请求，减少请求数；保留每段校验、断点状态、取消、重试和完整性验证。
- DBD 冷缓存使用轻量 Git Ref revision API，并在 revision 确定后并发预取 manifest 与已知表 definition；Definition 与 DB2 文件读取并发，热缓存保持零请求。
- 增加本地 HTTP fixture、功能回归、断点续传回归和性能基准，验证 CDN 请求数、P50/P95、失败率、内存和结果字节完全一致。

# Non-goals

- 不修改 DB2/Hotfix/SQL 查询语义或 WoWDBDefs 内容来源。
- 不强行提高默认并发到 8/16；以端到端 P50/P95、失败率和内存预算共同决定。
- 不绕过 CASC 内容身份、ETag/Last-Modified、SHA-256 或缓存配额校验。
- 不在回归测试中大规模请求公网 CDN；性能测试使用本地 HTTP fixture，公网仅保留受预算限制的兼容性探针。

# Acceptance examples

- 相同 CASC 对象与 fixture 下，优化前后解码字节、SHA-256、缓存 key 和断点恢复结果一致。
- 已完成且身份未变化的对象再次读取不发送 HEAD；小对象使用单次 GET。
- 相邻缺失 Range 能合并时，请求数减少且返回内容按原偏移重建完全一致；不能合并时保持原分段语义。
- 自适应策略在单连接更快的网络上不强制使用 4+ workers，在多连接确有收益时才提升并发。
- 本地基准覆盖单连接、2/4/8 workers、1/4/8 MiB chunk、断点恢复、Range 被忽略和失败重试；所有功能测试通过，P50/P95 与请求数证据写入 verification.md。

# Constraints and invariants

- 内容身份必须绑定 CDN URL、Content-Length、ETag/Last-Modified、Build/CDN key 和分块参数；身份漂移必须丢弃旧分块状态。
- 任一 Range 响应必须校验状态码、Content-Range、长度和分块 hash；完整对象仍需最终长度和内容校验。
- 首个不可恢复错误或上下文取消必须停止继续排队，并安全保存可恢复状态。
- 全局 scheduler、连接/文件句柄/内存预算和现有 cache quota 仍是硬边界。

# Decisions

- 默认使用自适应策略；固定策略仅作为 fixture 对照和回退路径。
- 优先优化 RTT/请求数和端到端 wall time，不以单纯吞吐或 worker 数作为唯一目标。
- 现有 1 MiB/4 MiB chunk 与 4 worker 作为候选，不预先承诺其中任一固定值。
- Adaptive 大对象 Range 使用独立 HTTP/1.1 多连接 transport，固定路径继续使用 HTTP/2；是否提高 worker 由真实 P50/P95 门禁决定。

# Open questions

- 无。

# Verification expectations

- `go test ./... -count=1`、`go test -race ./...`、`go vet ./...`。
- 本地 CASC HTTP fixture 功能、断点、Range 合并/回退和内容 hash 回归。
- 本地性能矩阵记录 baseline/optimized 的 P50、P95、请求数、失败率、峰值工作集和输出 SHA。
- 公网 CDN 仅执行单次、≤5 分钟、受请求/字节预算限制的兼容性探针。

# Performance gate

- 记录 50% wall-time 改善作为优化目标；真实 CDN 对照必须使用同一对象、同一网络环境和相同结果 SHA，并如实记录 P50/P95、请求数、失败率和峰值工作集。
- 当前实测改善未达到 50% 时，若功能、完整性、失败率、内存预算和回退行为无回归，按已确认的实测结果验收，不阻塞归档；未达目标作为已知限制保留在 Verify 报告中。
- 若真实 CDN 上请求数下降但 wall-time 变差，必须调整策略或回退，不得标记为优化完成。
- DBD/WoWDBDefs 远端访问单独记录冷/热缓存请求数、耗时、响应字节和 SHA，不与 CASC 指标混算；同样记录 50% 目标和实际结果，未达目标不阻塞归档但必须明确披露。
