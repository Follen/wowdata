# Outcome

把 `@follenfang/wowdata` 的 GitHub Actions npm 发布从长期 `NPM_TOKEN` 攥有方式切换为 npm Trusted Publisher OIDC。GitHub tag 发布继续生成五个平台二进制、GitHub Release 和 npm 包，但仓库和 GitHub Secrets 不再需要 npm 发布 token。

# Scope

- 使用 npm 已配置的 Trusted Publisher：GitHub 仓库 `Follen/wowdata`，workflow 文件 `release.yml`，权限 `npm publish`。
- 修改 `.github/workflows/release.yml`，让 publish job 使用 GitHub OIDC 获取 npm 短期发布凭据。
- 保留 publish job 的 `id-token: write`，移除 `NODE_AUTH_TOKEN`、`NPM_TOKEN` 和 npm token secret 依赖。
- 在发布前安装明确支持 Trusted Publisher 的 npm 版本，并输出版本供日志审计。
- 保留 `npm publish --access public --provenance`、语义版本 tag 校验、五平台构建、校验和与 GitHub Release。
- 更新发布说明，写明 Trusted Publisher、版本不可重复和 `0.0.1` 已经发布的事实。

# Non-goals

- 不重新发布、覆盖或删除已经存在的 `@follenfang/wowdata@0.0.1`。
- 不创建新的 npm token，不保存 OIDC 临时凭据，不修改用户 npm 账号的 2FA 页面设置。
- 不改变 CLI、Skill、缓存、CASC/DB2 或 npm 安装行为。
- 不在本次 change 中推送 Git tag、创建 GitHub Release 或实际触发生产工作流。

# Acceptance examples

- 查看 `.github/workflows/release.yml` 时，publish job 有 `id-token: write`，但全文不存在 `NPM_TOKEN`、`NODE_AUTH_TOKEN` 或 npm 发布 secret。
- tag `v0.0.2` 触发工作流时，package 和 Skill 版本更新为 `0.0.2`，五个平台构建通过后，npm CLI 通过 Trusted Publisher OIDC 发布公开包并附带 provenance。
- 工作流使用 npm `11.18.0`；发布日志明确打印 Node/npm 版本，便于审计 OIDC 运行环境。
- `npm test`、`npm pack --dry-run` 和五平台交叉构建继续通过，证明 OIDC 改造没有改变包内容。
- 对已经发布的版本重复打 tag 时，npm 版本不可变规则仍生效，不尝试覆盖旧包。

# Constraints and invariants

- npm 包名保持 `@follenfang/wowdata`，公开访问；`0.0.1` 已发布且不可覆盖。
- Trusted Publisher 的仓库和 workflow 文件名必须与 npm 页面配置完全一致：`Follen/wowdata` 和 `release.yml`。
- OIDC 权限只授予 publish job；全局默认继续使用 `contents: read`，GitHub Release 所需 `contents: write` 也只授予 publish job。
- 发布脚本保持源码可读，不引入自定义凭据代理或外部发布服务。
- GitHub Actions 中 Node 22 满足 npm `11.18.0` 的运行要求。

# Decisions

- 发布身份：npm Trusted Publisher OIDC，不再使用长期 npm token。
- npm 版本：发布 job 固定安装 `npm@11.18.0`，避免 runner 自带旧 npm 不支持 OIDC。
- 供应链证明：保留 `--provenance`。
- npm 页面安全设置：Trusted Publisher 已由用户配置；建议使用禁止 bypass 2FA token 的最严格选项，但该页面状态不由仓库代码管理。
- 共享理解：用户确认把上述方案写入新 change 并直接实施。

# Open questions

- 无。

# Verification expectations

- 静态检查 workflow 的 trigger、Trusted Publisher tuple、最小权限、npm 固定版本、无 token secret 和 provenance 参数。
- 运行 `go test ./... -count=1`、`go vet ./...`、`npm test` 和 `npm pack --dry-run`。
- 交叉构建 Windows amd64、Linux amd64/arm64、macOS amd64/arm64。
- 不触发真实 tag 发布；OIDC 与 npm 账号绑定通过 npm 页面现有 Trusted Publisher 配置和 workflow 静态合同验证。
