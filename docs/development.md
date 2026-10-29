# 开发与构建

## 工具与版本

Go 工具链要求见 `go.mod`（当前 `go 1.27.3`）；前端使用 Node.js 24、npm 和锁定的 `web/package-lock.json`。Windows MSI 使用 WiX 4.0.6 及其兼容 .NET runtime。需要真实数据库时使用 PostgreSQL 18；v0.1.1 还在 Linux PostgreSQL 17.11 验证过兼容性。

前端准确版本见 `web/package.json`，不要根据框架主版本自行更新锁文件。普通运行发布包无需 Go、Node、WiX。CI 配置为执行要求，不是 CI 已执行的证明。

## 目录

```text
cmd/          三个交付程序
internal/     业务、运行时、存储及当前测试
web/          React 控制台与锁文件
deploy/       可选 systemd、反代、MSI 与日志模板
scripts/      构建、生成、打包、验收及文档检查
docs/         当前文档和固定版本证据
third_party/  保留许可证的 GOST 固定基线
patches/      可审查补丁与摘要
bin/ dist/    被忽略的本机构建/发布产物
```

`web/go.mod` 是防止根模块 `go test ./...` 递归扫描 JavaScript 依赖中 Go 示例的边界文件，不是额外后端服务。

## Go 构建

```sh
go test ./...
go vet ./...
go build -trimpath -o bin/altasci-control ./cmd/altasci-control
go build -trimpath -o bin/altasci-edge ./cmd/altasci-edge
go build -trimpath -o bin/altascitunnel ./cmd/altascitunnel
```

Linux shell 交叉编译示例：

```sh
mkdir -p bin/linux-amd64
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o bin/linux-amd64/altasci-control ./cmd/altasci-control
# 对 altasci-edge、altascitunnel 重复；ARM64 使用 GOARCH=arm64。
```

PowerShell 中设置 `$env:GOOS`、`$env:GOARCH` 后构建，结束时恢复或删除本次设置，避免后续测试意外执行错误平台产物。交叉编译不代表对应平台运行测试通过。

## 前端

```sh
cd web
npm ci --no-audit --no-fund
npm run typecheck
npm test
npm run build
```

开发模式 `npm run dev` 将 `/api` 代理到本机 Control 18081；Control 以 `--dev-http` 在本机开发时提供非 Secure Cookie。实际集成仍验证 HTTPS/WSS。

开发服务器仅绑定回环地址（Linux 使用 `HOST=127.0.0.1 npm run dev`；PowerShell 先设置 `$env:HOST='127.0.0.1'`），不要将开发/测试服务器发布到公网或作为生产入口。依赖安全复查可运行 `npm audit` 和 `go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...`；已接受的残余风险见 [安全审查记录](security-review-2026-10-29.md)。

Windows 推荐 `./scripts/build-web.ps1 -Verify`，它在系统临时目录干净安装依赖、验证、构建，收集许可，清理旧 `web/dist` 后复制新产物，避免遗留旧 chunk。使用 `-KeepWorkdir` 才保留工作目录。根项目盘的小文件性能问题不应通过提交 node_modules 或临时安装器解决。

## 一体构建脚本

| 脚本 | 输出与限制 |
| --- | --- |
| `scripts/build.ps1` | Windows 三个 exe、前端与 bin 校验；不自动生成所有 Linux 架构 |
| `scripts/build.sh` | 当前平台三个程序、前端、依赖许可与摘要 |
| `scripts/build-msi.ps1` | 附带许可及 MPL 源码的 MSI；解开实际 CAB 校验版本和材料，不执行安装；先构建生产前端/许可 |
| `scripts/package.ps1` | 强制 Windows/Linux amd64/arm64 完整，附生产前端、源码、文档、许可与校验值 |

MSI 示例：

```powershell
go build -trimpath -o bin/altascitunnel.exe ./cmd/altascitunnel
./scripts/build-web.ps1 -Verify -Version 0.1.2
./scripts/build-msi.ps1 -Version 0.1.2
# 使用非全局 WiX 时，可增加 -WixDll C:/path/to/wix.dll。
./scripts/package.ps1 -Version 0.1.2
```

示例版本不代表仓库已经发布该版本。先完成 bin 中其他所需程序、Linux 架构和 Web 构建，再打包；前端目录可通过 `-FrontendDist` 指定。release staging 目录存在时脚本拒绝覆盖，避免静默覆盖已有产物。

MSI 构建会解开实际 CAB，校验版本、程序/许可摘要及内嵌 MPL 源码；打包会校验前端版本、平台架构、逐文件摘要和许可材料。这些内容验证不代替安装/业务测试或签名。当前 MSI/程序签名需外部流程完成，签名后重新生成摘要。发布包携带独立源码 ZIP，解开它可取得 Go 程序、测试夹具和锁文件；构建依赖仍需准备。

完整 push CI、版本命名、产物及标签自动发布见 [发布指南](releasing.md)，上游分发义务见 [许可证说明](licensing.md)。生产 Web 的版本由 ALTASCI_VERSION 注入，本地 build-web.ps1 使用 -Version 传入，默认 dev；打包要求与目标包版本一致。

## 数据库代码与 GOST 补丁

```sh
go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.30.0 generate
node scripts/gost-patches.cjs
```

前者使用 `sqlc.yaml` 及查询/迁移生成 `internal/store/db`。后者对 checksum 固定的 GOST 模块基线生成六文件补丁和 SHA-256 清单，需要 Go 模块缓存、Git 和 Node。不要整体格式化第三方代码；升级兼容流程见 [补丁说明](../patches/README.md)。

`scripts/collect-licenses.cjs` 从三个交付平台链接的 Go 模块及生产 Webpack 模块收集许可和必要源码。缺少生产模块清单、实际依赖目录或完整许可时构建失败；整个 npm 安装树不等于分发清单。运行构建脚本完成收集。

## 提交前检查

运行文档检查、适用的 Go/前端测试，并说明外部用例是否实际运行。修改授权、GOST、IPC 或安装动作时增加对应集成/平台验收；仅整理文档和生成物不冒充重新完成系统验收。

`.env`、设备身份、数据库、`.local`、依赖目录和构建产物被忽略。有效测试和固定版本证据保留在源码中，清理规则见 [仓库维护](maintenance.md)。
