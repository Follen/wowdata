# wowdata

`wowdata` 是一个纯 Go 的 World of Warcraft 数据 CLI，用于查询和导出 CASC、DB2、Listfile、图标、法术、副本、物品、生物、装饰物和视频数据。

## 安装

```bash
npm install -g @follenfang/wowdata
```

npm 会完成两件事：

- 把当前平台的 Go CLI 安装到 `~/.wowdata/bin`，并提供全局 `wowdata` 命令。
- 把轻量 Skill 安装到 `~/.agents/skills/wowdata`。Skill 只有说明和 Reference，不包含可执行文件。

支持 Windows amd64、Linux amd64/arm64、macOS amd64/arm64。

## 目标必须明确

CLI 没有地区、产品、Build 或语言默认值。需要游戏数据的命令必须传完整目标：

```bash
wowdata db2 rows SpellName --id 123 \
  --source remote \
  --region cn \
  --product wow \
  --build latest \
  --locale zhCN
```

也可以先保存完整 Profile：

```bash
wowdata profile set retail-cn \
  --source remote \
  --region cn \
  --product wow \
  --build latest \
  --locale zhCN

wowdata db2 rows SpellName --id 123 --profile retail-cn
```

Skill 的规则不同：用户没指定时，Skill 显式传入中国区 `cn`、最新 Build `latest` 和简体中文 `zhCN`；产品没有默认值，无法从用户话语或上下文确定时，Skill 会先询问用户。

## 直接查询

普通任务直接运行对应原子命令，不需要先执行预热：

```bash
wowdata spell info --spell-id 123 --profile retail-cn
wowdata file lookup --file-data-id 134400 --profile retail-cn
wowdata icon export --file-data-id 134400 --format png --output output/icon.png --profile retail-cn
```

CLI 会在同一次命令中：

1. 解析明确的产品和 Build。
2. 检查远端内容身份与本地 SHA-256。
3. 复用有效缓存，下载缺失或过期内容。
4. 完成查询并返回最终结果。

准备和下载进度写入 stderr，最终结构化结果写入 stdout JSON。多个独立 CDN 对象和清单默认使用 4 个 worker 并发下载；可以通过 `wowdata cache config --workers N` 调整。

## 输出和退出码

CLI 可以直接用于 Agent，也可以作为普通 Shell 命令组合：

- 成功结果为 `"ok": true`，进程退出码为 `0`。
- 业务错误为 `"ok": false`，错误详情仍是 stdout 中的 JSON，进程退出码为 `1`。
- 准备和下载进度只写入 stderr，不会污染 stdout JSON。
- 参数拼写错误等尚未进入命令处理器的错误写入 stderr，并返回退出码 `1`。

因此脚本既可以判断退出码，也可以继续解析结构化错误：

```bash
if wowdata db2 rows SpellName --id 133 \
  --source remote --region cn --product wow --build latest --locale zhCN \
  >result.json; then
  jq '.data.rows' result.json
else
  jq '.error' result.json
fi
```

## 发现与提前准备

查看某个区域当前提供的真实产品和 Build 组合：

```bash
wowdata casc products --source remote --region cn
```

需要提前下载时再显式使用 `warmup`：

```bash
wowdata warmup \
  --source remote \
  --region cn \
  --product wow \
  --build latest \
  --locale zhCN
```

## 本地目录

```text
~/.wowdata/
├── bin/       CLI 二进制
├── config/    用户配置
├── profiles/  完整数据目标
├── builds/    不可变 Build 快照
├── cache/
│   ├── casc/
│   ├── dbd/
│   ├── listfile/
│   ├── tact/
│   └── manifests/
├── state/     安装和最近状态
├── tmp/       下载临时文件
└── locks/     跨进程下载锁
```

缓存默认上限为 20GB。每个 Profile 的当前 Build 和上一个 Build 不参与自动清理，更老的 Build 按最久未使用顺序清理。

```bash
wowdata cache status
wowdata cache verify
wowdata cache prune
wowdata cache clear
wowdata cache config --max-gb 30 --workers 6
```

远端不可用时，CLI 可以使用已经通过完整性校验的离线缓存；缺失、损坏和未完成的缓存不会被使用。

## 诊断和维护

```bash
wowdata doctor
wowdata update
wowdata update --version 1.2.3
wowdata uninstall
wowdata uninstall --keep-data
```

`doctor` 只读检查 CLI/npm、目录、Profile、缓存完整性和基础网络，不自动修改数据。

`uninstall` 默认删除 CLI、托管 Skill 和整个 `~/.wowdata`。使用 `--keep-data` 时保留配置、Profile、Build 和缓存。安装遇到非本包托管的旧 Skill 时会先备份，不会直接覆盖。

## 命令

```text
warmup
db2 schema/rows/search/foreign-key/stream
spell info/auras/summons
encounter get
file lookup/search/extension/get/exists/encoding/export
icon export
casc info/products/diagnose
item get/models/geosets/textures
creature display/model
decor list/get
video demux
profile list/show/set/remove
cache status/verify/prune/clear/config
doctor
update
uninstall
```

运行 `wowdata --help` 或 `wowdata <command> --help` 查看完整参数。

## 从源码构建

```bash
go test ./... -count=1
go build -trimpath -o dist/wowdata ./cmd/wowdata
npm test
```

Go 负责数据解析和命令执行；Node.js 只负责 npm 安装、启动桥接和包生命周期，所有脚本都在 `npm/` 中可审计。

## 发布

推送语义版本 tag 会触发 GitHub Actions：

```bash
git tag v1.2.3
git push origin v1.2.3
```

工作流会测试 Go 和 npm、构建五个平台、生成 `SHA256SUMS`、创建 GitHub Release，并发布同版本 `@follenfang/wowdata`。

npm 发布使用绑定到 `Follen/wowdata` 和 `release.yml` 的 Trusted Publisher OIDC，不需要在 GitHub Secrets 中保存 `NPM_TOKEN`。发布 job 固定使用 npm `11.18.0`，并生成 npm provenance。

npm 版本不可覆盖：`0.0.1` 已经发布，后续正式发布从更高的新版本 tag 开始，例如 `v0.0.2`。

## License

AGPL-3.0-or-later
