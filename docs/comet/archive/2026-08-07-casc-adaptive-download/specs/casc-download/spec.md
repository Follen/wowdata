# CASC 自适应下载规格

## Capability

CASC 远端对象读取根据对象大小、缓存状态和已验证网络表现选择下载策略，并在不改变内容身份和恢复语义的前提下减少网络往返与 Range 请求。

## Behavior

### Strategy selection

- 对稳定不可变对象优先读取本地完整缓存或有效 resume state。
- 有效 resume state 且 Content-Length/ETag/Last-Modified 未变化时，不发送重复 HEAD；若身份无法确认，必须保留 HEAD。
- 小对象走单次 GET；大对象才启用 Range 分块。
- worker 数和 chunk size 由受限策略选择，候选必须受 scheduler、内存、句柄、失败率和请求预算约束。

### Range planning

- 缺失分块按偏移排序。
- 相邻且合并后不超过策略上限的缺失区间可合并为一个 Range 请求。
- 响应必须按原始区间映射写回，不能因合并改变最终对象布局。
- 服务端忽略 Range、返回错误 Content-Range 或长度不符时，回退到现有完整 GET/重试路径。

### Recovery and integrity

- 每个完成区间记录 hash 和完成状态；进程中断后只重取无效区间。
- URL、大小、ETag、Last-Modified、chunk size 或策略版本变化时，旧 resume state 不得复用。
- 最终发布前校验完整长度、所有分块 hash 和内容身份；失败不得留下可被误认为完整对象的缓存条目。

### Observability

- 记录 strategy、workers、chunk size、planned ranges、merged ranges、requests、reused bytes、downloaded bytes、P50/P95 wall time、失败/回退原因和输出 SHA。
- 公网探针受总时长 5 分钟、请求数和响应字节预算限制；完整性能回归使用本地 fixture。

## Acceptance criteria

1. 功能 fixture 在 baseline 与 optimized 路径产生相同字节和 SHA-256。
2. 热缓存身份不产生重复 HEAD；小对象请求数为 1。
3. 可合并 Range 的请求数低于未合并基线，且重建内容一致。
4. 断点恢复、Range 忽略、超时、取消和重试测试全部通过。
5. 自适应矩阵不劣于固定策略的端到端 P50/P95，且不突破内存、句柄、失败率和公网预算边界。
6. 受限真实 CDN 探针中记录相对同对象 baseline 的端到端 wall-time P50/P95，50% 为目标值；若实际未达 50% 但功能、完整性、失败率、内存预算和回退行为无回归，按已确认实测结果验收，不阻塞归档。请求数下降但 wall-time 上升时仍须回到 Build 调整策略。
7. 真实 CDN 探针记录请求数、唯一/重复响应字节、失败数、P50/P95、峰值工作集、输出 SHA-256 和完整命令；单次探针总时长不超过 5 分钟。
8. WoWDBDefs/GitHub DBD 冷缓存与热缓存作为独立指标记录请求数、耗时、HTTP 状态、响应字节和定义 SHA-256；50% 为目标值，未达目标但无功能和资源回归时按确认结果验收，并不得把 DBD 命中当作 CASC 性能收益。
