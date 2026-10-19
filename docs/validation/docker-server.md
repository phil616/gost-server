# Docker 单镜像服务端验证

日期：2026-10-19。对象：本地工作树的 Dockerfile、Compose、`altasci-server` 入口与显式容器业务监听配置；不代表已经发布镜像或部署到用户服务器。

## 环境与构建

- Windows 主机上的 WSL Debian 13，Linux amd64；本次准备并使用 Docker Engine 26.1.5、Compose 2.26.1、Buildx 0.13.1。
- Go 构建基础镜像 `golang:1.27.2-bookworm`、Node 24 bookworm slim、运行基础镜像 Distroless static Debian 13 nonroot。
- 实际构建 `altasci-server:docker-test`。首轮镜像约 48.5 MB，只包含 Control、Edge、Go 容器入口、生产 Web、项目源码归档和法律材料，不包含设备客户端、shell、Node/Go 工具链。
- 镜像构建中的前端 TypeScript、14 项 Vitest 测试和生产编译通过；分发许可审查通过，共 171 项保守组件清单，含两项 MPL 模块的校验源码 ZIP。

基础镜像标签会随上游更新；这些版本和大小描述本次测试环境，不是固定 digest 或逐字节重现承诺。归档源码直接来自当前构建上下文，不会把本地 Git HEAD 误当成所有未提交修改。

## 实际运行结果

执行 `scripts/test-docker.py --image altasci-server:docker-test`，使用随机命名的网络、持久卷和独立 PostgreSQL 18 容器模拟外置数据库：

| 检查 | 结果 |
| --- | --- |
| 空卷直接 serve 拒绝启动 | PASS |
| UID 65532 初始化密钥、执行真实 PostgreSQL 迁移 | PASS |
| 重复 init 保留密钥，keygen 拒绝覆盖 | PASS |
| 镜像内 MIT 署名、许可清单、MPL 源码 SHA-256 与项目源码归档 | PASS |
| 无设备客户端、shell、node_modules 或本地秘密进入交付源码 | PASS |
| Web 页面、数据库 ready、管理员登录与 Secure Cookie | PASS |
| 保留实际外部 origin/端口的注册申请，CSRF 验证 | PASS |
| Control → Edge Unix socket 同步及策略 revision | PASS |
| SIGTERM 正常停止、容器重建后密钥与策略快照保留 | PASS |
| 杀死 Edge 后 Control 一起退出，容器失败退出 | PASS |
| 实际仓库 Compose 读取含 `$` 密码的环境文件 | PASS |
| Compose 回环端口发布、只读根文件系统与 unless-stopped 自动重启 | PASS |
| 服务不可用时 healthcheck 返回失败 | PASS |

脚本结束后删除本次容器、网络、数据卷和 PostgreSQL 夹具的匿名卷。它不读取用户 `.env`/`compose.env`，不连接已有数据库，不修改宿主机反代。

## Go 与文档检查

`go test -count=1 -timeout 5m ./...` 在 Windows 上通过，指定官方 Windows GOST v3.3.0，包含原有真实 TCP/UDP 与 WSS Daemon 集成；PostgreSQL 测试缺少 `TEST_DATABASE_URL` 时按现有规则跳过，本次真实 PostgreSQL 另由 Docker 测试覆盖。

Linux 构建阶段运行入口与 host 模块测试；另在 Linux Go 构建镜像内执行 `go test -race -count=1 ./cmd/altasci-server ./internal/host` 通过，覆盖两子进程 SIGTERM、启动失败回收、异常退出、单次命令成功和强制停止超时。Windows 的 POSIX 生命周期测试明确跳过。`go vet`、文档本地引用检查、Compose 配置及工作流 YAML 解析通过。

## 边界

本次容器实际运行环境为 Linux amd64，没有把交叉编译当作 ARM64 运行验收。Docker 测试不运行外部 HTTPS/WSS 反向代理或设备客户端，也不代表已在用户域名上完成业务验收；原有 Go 集成覆盖业务协议回归，外部接入仍按 [Docker 指南](../docker.md) 和 [使用指南](../usage.md) 联调。

未执行新的 Windows Sandbox/MSI 生命周期、24 小时压力或多主机容量测试。没有推送镜像仓库、发布版本、运行远端 GitHub Actions 或进行生产部署。
