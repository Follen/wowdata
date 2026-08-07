# CASC 自适应下载规格

## Capability
CASC 远端对象读取根据对象大小、缓存状态和网络表现选择下载策略，减少 RTT 与 Range 请求，同时保持内容身份和恢复语义。

## Behavior
- 有效 resume state 且身份稳定时跳过重复 HEAD；无法确认身份时保留 HEAD。
- 小对象走单次 GET，大对象走受预算约束的 Range 分块。
- 缺失分块按偏移排序；相邻区间可在上限内合并，写回仍按原始偏移映射。
- 每块记录 hash 与完成状态；URL、大小、ETag、Last-Modified、chunk 或策略变化时旧状态失效。
- Range 被忽略、Content-Range 错误或长度不符时回退到现有完整 GET/重试路径。

## Observability
记录 strategy、workers、chunk size、planned/merged ranges、requests、reused/downloaded bytes、P50/P95、失败/回退原因和输出 SHA-256。

## Acceptance criteria
1. baseline 与 optimized 输出字节和 SHA-256 一致。
2. 热 resume 不重复 HEAD，小对象请求数为 1。
3. 可合并区间请求数低于未合并基线，不能合并时保持分段语义。
4. 恢复、身份漂移、Range 忽略、短响应、超时、取消和重试测试通过。
5. 性能矩阵不劣于固定策略；报告明确 `P50 improvement = (baselineP50-optimizedP50)/baselineP50*100%`。
