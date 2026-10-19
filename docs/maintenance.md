# 仓库与文档维护

## 源码和生成物

| 分类 | 处理方式 |
| --- | --- |
| `cmd`、`internal`、`web/src`、迁移和锁文件 | 保留，属于可构建源码 |
| 当前单元/集成/安装验收脚本和夹具 | 保留，仍有明确验证职责 |
| `third_party/gost-x`、许可证、patch/manifest | 保留；上游文件按补丁维护规范处理 |
| `docs/validation/VERSION` | 保留有效固定版本证据；标明日期与限制 |
| `.local` | 本地临时工具、数据库、下载、截图和实验目录，可重建 |
| `web/node_modules`、Umi 生成目录 | 依赖和框架缓存，不入库 |
| `bin`、`web/dist`、`dist` | 构建或发布产物，不是源码 |
| `.env`、设备身份、密钥和运行目录 | 私有配置/秘密，不提交，不按普通文档清理 |

## 本轮整理

移除原始需求稿、旧计划稿及被实际实现替代的可行性长文；有效约束已整理到概览、架构和决策文档。早期 `docs/research` 原生 GOST 探针与一次性结果已由当前官方 GOST 集成测试取代。

旧的中文主题入口被新的主题文档取代，最小启动明确接入使用者的既有数据库/反代。历史验证证据迁入 `docs/validation/0.1.1`，删除空输出和依赖本机临时路径的原始日志引用。

保留当前 v0.1.1 发布归档和可用运行程序，清理旧 v0.1.0 归档/MSI、重复解压 staging、WiX 调试产物、不完整 npm 安装、临时数据库/工具/下载。已有发布归档保持原始字节与校验值；它的文档是当时版本快照，当前文档以本目录为准，不静默改写旧包。

后续开源发布新增 MIT/第三方源码材料与许可检查；本地旧 v0.1.1 包不符合新的完整分发材料规范，只保留作历史证据，不直接上传为新的开源发行包。新包按 [发布指南](releasing.md) 重新生成，不能只给旧 ZIP 改名。

用户已有 `.env` 仅增加忽略规则，保留原文件；不读取或写入其中的秘密。系统级工具与工作区之外的下载/缓存不属于本轮自动删除范围。

## 可重复清理

`scripts/clean.ps1` 使用 PowerShell 7，仅处理明确生成目录，可先用 WhatIf 预览。它校验绝对路径位于仓库、拒绝目标/祖先路径及目录项中的链接；独立文件以有界工作数删除，等待全部完成后只移除空目录，不使用递归删除或跟随链接。`-DeleteWorkers` 可在 1–16 间调整，默认 8。

```powershell
./scripts/clean.ps1 -WhatIf
./scripts/clean.ps1 -Dependencies -LocalTools -Staging -WhatIf
./scripts/clean.ps1 -Dependencies -LocalTools -Staging
```

| 开关 | 范围 |
| --- | --- |
| 无开关 | Umi 生成目录 |
| `-Dependencies` | web/node_modules |
| `-LocalTools` | .local；先停止本地数据库和测试进程 |
| `-Staging` | dist 下版本解压目录、bin 下 WiX 调试文件及 installer-licenses.zip 中间包；保留发布 ZIP 与校验文件 |
| `-OldRelease X.Y.Z` | 指定旧版本完整/源码归档及摘要、staging、MSI 与 wixpdb；也支持 rc/ci 后缀 |
| `-BuildOutputs` | bin、dist、web/dist，包含当前发布包，按需显式选择 |
| `-FrontendOutput` | 仅 web/dist，清理后重新构建前端 |

不要在构建或服务写入目录时同时清理。清理失败时检查实际剩余路径，不能用全盘扫描或跨 shell 拼接删除命令兜底。

## 文档检查与更新

```sh
node scripts/check-docs.cjs
git diff --check
```

检查脚本只依赖 Node 标准库，检查主题文档/本地引用、REST 路由的 OpenAPI 覆盖、内部组件引用和已移除入口。它不检查远端网站在线情况，也不证明文档命令已在所有系统运行。

新增 API 同步更新 OpenAPI 和接口说明；新增参数同步更新配置表；改变状态/授权同步更新架构、安全、协议和有效测试。历史验证不得改写成新代码已通过，测试日志清理前先保留可追溯、脱敏的必要证据。

## 本轮整理验证（2026-10-19）

| 检查 | 结果 |
| --- | --- |
| 文档检查 | 23 个维护的 Markdown 文件、100 个本地引用、20 个 REST 路由及 OpenAPI 内部引用通过 |
| YAML | OpenAPI 与 CI 文件可解析，未发现重复映射键 |
| Go | `go test -count=1 ./...`、`go vet ./...` 通过；本轮未配置外部集成变量，不把 SKIP 计为 PostgreSQL/GOST/浏览器重新验收 |
| 前端 | 临时目录 `npm ci`、类型检查、2 个测试及生产构建通过，旧 chunk 清理后写回新 dist |
| 清理脚本 | WhatIf、链接拒绝、只读嵌套文件、有界并行删除及目标外哨兵保留检查通过 |
| GOST | 六个补丁文件的摘要与 manifest 一致，第三方实现未改动 |
| 发布归档 | 保留的 v0.1.1 zip 摘要与原 sha256 文件一致，未重写历史包 |
| 私有配置 | 用户原有 .env 保留并被 Git 忽略，未读取秘密内容 |

最终一批清理统计为 56,110 个文件、1,977.1 MiB；此前已移除的部分依赖不计入该批统计。当前有效测试、固定版本证据、10 个运行程序/MSI 产物与原始发布归档保留。
