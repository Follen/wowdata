# npm Trusted Publishing 完整目标规格

## 发布身份

### Requirement: GitHub OIDC Trusted Publisher

`@follenfang/wowdata` 的正式 npm 发布必须使用 npm Trusted Publisher 和 GitHub Actions OIDC。受信任发布者必须绑定 GitHub 仓库 `Follen/wowdata` 与 workflow 文件 `release.yml`。workflow 不得要求 `NPM_TOKEN`、`NODE_AUTH_TOKEN` 或其他长期 npm 发布凭据。

### Requirement: 最小权限

workflow 默认权限必须保持 `contents: read`。只有负责发布的 job 可以获得 `id-token: write` 和创建 GitHub Release 所需的 `contents: write`；测试与构建 job 不得获得 OIDC token 写权限或仓库写权限。

## 发布工具链

### Requirement: 可审计 npm 版本

发布 job 必须在发布前安装 `npm@11.18.0` 并打印 Node 与 npm 版本。该版本必须运行在满足其 engine 约束的 Node 22 环境中，确保 Trusted Publisher OIDC 行为不依赖 runner 预装的未知 npm 版本。

### Requirement: 公开包与 provenance

正式发布命令必须是等价于 `npm publish --access public --provenance` 的可审计命令。发布的包名必须为 `@follenfang/wowdata`，版本必须与触发 tag、Go 二进制版本和 Skill VERSION 一致。npm 的不可变版本规则不得被绕过。

## 发行完整性

### Requirement: 保持现有发布门禁

Trusted Publisher 改造不得跳过 Go 测试、npm 测试、五平台构建、语义版本 tag 校验、SHA-256 生成、npm pack 检查或 GitHub Release。只有测试和构建成功后才能进入 publish job。

### Requirement: 凭据零落盘

workflow、仓库文件、构建产物、日志和 npm 包不得保存 OIDC 临时令牌、npm token 或其他发布凭据。OIDC 凭据只能由 GitHub Actions 与 npm 在单次 publish job 中交换和使用。
