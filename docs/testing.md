# 测试指南

## 常规检查

```sh
go test -count=1 ./...
go vet ./...
node scripts/check-docs.cjs
node --test scripts/release-tools.test.cjs
cd web
npm ci --no-audit --no-fund
npm run typecheck
npm test
npm run build
```

Go 测试覆盖模型、认证授权、配置恢复、IPC、重试、存储与进程生命周期；前端测试覆盖 API、注册命令和解码器安全回归。第三方模块的上游测试不在根模块 `./...` 范围内。

## 数据库与官方 GOST

使用独立 PostgreSQL 测试库和经官方校验清单验证的 gost v3.3.0：

```sh
export TEST_DATABASE_URL='postgres://TEST_USER:TEST_PASSWORD@127.0.0.1:5432/TEST_DB?sslmode=disable'
export GOST_BINARY='/absolute/path/to/official/gost-v3.3.0'
go test -race -count=1 -v ./...
```

PowerShell 使用 `$env:TEST_DATABASE_URL=...`、`$env:GOST_BINARY=...`。数据库账号需要建 schema/DDL 权限；测试使用随机 schema 并清理。缺少变量时相关用例会跳过，不能算作集成验证通过。

## 浏览器与容器

准备 Web 产物、Chrome、Node 和 GOST_BINARY 后，执行管理控制台与真实转发验证：

```sh
ALTASCI_BROWSER=1 go test ./internal/integration \
  -run TestDaemonWSSRegistrationDataAndControlOutage -v -count=1 -timeout 5m
```

可通过 CHROME_BINARY 指定浏览器。浏览器脚本位于 `internal/integration/browser-smoke.mjs`，由该集成测试启动。

Docker 验证需要本机 Linux Docker Engine：

```sh
docker build -t altasci-server:dev .
sudo python3 scripts/test-docker.py --image altasci-server:dev
```

脚本创建并清理独立数据库、网络和数据卷，覆盖初始化、密钥保留、镜像、Web/API、容器隔离与进程恢复。MSI/systemd 的安装、升级和卸载在独立环境手动验证。

CI 执行前端、文档、许可、PostgreSQL、官方 GOST、浏览器、race、Docker 和 Windows 构建检查，详见 [发布指南](releasing.md)。测试报告说明提交版本、执行命令、结果及跳过项；原始输出留在 CI artifacts 或本地忽略目录。
