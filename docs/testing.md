# 测试指南

## 验证层次

| 层次 | 覆盖 | 前提 | 未执行时的表达 |
| --- | --- | --- | --- |
| Go 单元/模块 | 模型、vault、退避、授权、配置与恢复逻辑 | Go | 不代替网络或安装测试 |
| PostgreSQL | 迁移、事务、并发、约束及真实控制仓储 | TEST_DATABASE_URL | 缺少变量会 SKIP |
| 官方 GOST 集成 | TCP/UDP、固定目标、WSS、控制故障和撤销 | GOST_BINARY | 缺少变量会 SKIP |
| 前端 | TypeScript、API 客户端测试、生产构建 | Node + npm ci | 构建不等于浏览器操作通过 |
| 浏览器 | 真实 Chrome 的管理操作与 Provider Bind | Web dist、Chrome、Node、官方 GOST | 显式启用才执行 |
| 系统安装 | MSI/systemd、身份、重启、修复、升级与卸载 | 可丢弃 Windows/Linux 环境 | 仅构建/读取 MSI 表不算通过 |
| 规模/长期 | Runtime 数量、并发业务与持续资源趋势 | 显式开关、时间和外部程序 | 短测不冒充 24 小时或生产容量 |

## 普通检查

```sh
go test -count=1 ./...
go vet ./...
node scripts/check-docs.cjs
```

普通 Go 运行可能跳过外部依赖用例，要检查输出，不能只报告命令退出码。当前业务测试位于 `internal/**/*_test.go`；第三方模块有其上游测试，不属于根模块 `./...` 的全量执行范围。

## 真实数据库与官方 GOST

```sh
export TEST_DATABASE_URL='postgres://TEST_USER:TEST_PASSWORD@127.0.0.1:5432/TEST_DB?sslmode=disable'
export GOST_BINARY='/absolute/path/to/official/gost-v3.3.0'
go test -race -count=1 -v ./...
```

Windows PowerShell 使用 `$env:TEST_DATABASE_URL=...`、`$env:GOST_BINARY=...`。race 需要支持的 C 工具链；本机 Windows 环境没有满足条件时可在 WSL 原生文件系统中运行。

数据库测试创建随机 schema 并清理；账号需要对应建 schema/DDL 权限，仍应使用独立测试数据库。官方 GOST 从固定发布下载并验证 release 校验清单；测试检查 `gost v3.3.0` 版本，不能用项目修改程序互测替代官方 Visitor。

主要测试证据包括：

- `internal/store/postgres_test.go`：迁移、回滚、并发和约束。
- `internal/control/*_test.go`：设备签名、token 消费/恢复、TLS/幂等/CSRF、策略错误 hash、恢复版本。
- `internal/edge/engine_test.go`：角色、TunnelID、generation、快照和活动连接撤销。
- `internal/integration/native_test.go`：官方 TCP/UDP、固定目标及实时撤销。
- `internal/integration/daemon_test.go`：真实 IPC Daemon 注册、TLS WSS、数据转发、控制停止/恢复。
- `internal/retry/retry_test.go`：假时钟验证精确等待、持久状态、清零、取消和时钟回拨。

## 前端与浏览器

先运行 `npm ci`、typecheck、test、build。已构建 Web 和准备官方 GOST 后：

```sh
ALTASCI_BROWSER=1 go test ./internal/integration \
  -run TestDaemonWSSRegistrationDataAndControlOutage -v -count=1 -timeout 5m
```

还需环境中的 GOST_BINARY、Node.js 和 Chrome；可通过 CHROME_BINARY 指定浏览器。该分支使用临时内存控制库与本机测试服务，完成登录、注册申请、隧道创建/编辑/启停/删除、真实 Provider Bind、Visitor 展示/YAML 下载/撤销、审计、Edge 和退出。

`scripts/browser-smoke.mjs` 也可针对独立测试控制端执行，需要 SMOKE_ORIGIN、SMOKE_USERNAME、SMOKE_PASSWORD；SMOKE_MUTATIONS=1 会变更资源，只能用于测试账号和测试环境。截图默认写入 `.local`，不作为源码必需材料。

## 规模与 24 小时运行

```sh
ALTASCI_STRESS=1 go test ./internal/integration -run TestScale -v -count=1 -timeout 5m
ALTASCI_STRESS=1 ALTASCI_SOAK_SECONDS=86400 go test ./internal/integration -run TestScale -v -count=1 -timeout 25h
```

两条都还需 GOST_BINARY。规模用例在同一 Go 测试进程内创建 100 Runtime、1000 反向连接，再由一个官方 gost 进程提供 100 并发 TCP 业务连接。默认本机 WS，不经过真实 TLS 反代；不是 100 独立主机/客户端进程测试，也不覆盖混合 UDP 长期压力。

Go heap 不包含其他进程和操作系统 RSS。长期记录应包含机器条件、代理/TLS、载荷、内存/句柄/连接趋势与故障注入时间；不能将可配置 86400 参数写成已经完成 24 小时验证。

## CI 与系统验收

`.github/workflows/ci.yml` 定义文档/前端/许可检查、Linux PostgreSQL/官方 GOST/浏览器/race/sqlc，以及 Windows 测试、MSI 内容校验与完整分发 ZIP 打包。标签触发 release.yml 复用同一 CI 后发布，详见 [发布指南](releasing.md)。仓库存在 workflow 不证明已在远端执行成功；MSI 内容校验也不等于实际安装通过。

真实平台安装使用 [系统验收手册](system-acceptance.md) 中的受控环境。当前安装测试脚本和 `internal/integration/systemfixture` 仍服务于有效验收，不能作为“旧测试”删除。早期仅探索原生 GOST 通用认证的调研探针已经由这些测试取代。

## 报告格式

记录版本/提交、日期、操作系统/架构、依赖与外部程序版本、具体命令、结果/跳过项、故障与修复、产物 SHA-256、未覆盖范围。原始输出放入对应 `docs/validation/VERSION`，禁止混入 token、私钥或真实密码。历史格式见 [v0.1.1](validation/0.1.1/README.md)。
