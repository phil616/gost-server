# AltasCI Tunnel

基于 GOST v3 的单 Edge 网络穿透平台。管理员通过 Web 注册 Windows/Linux 设备、配置 TCP/UDP 隧道并发放凭据；设备主动建立 WSS 连接，访问方使用官方原生 gost。

原创部分采用 [MIT License](LICENSE)，署名 [\@phil616](https://github.com/phil616)。第三方代码保留原许可，详见 [许可证与分发约束](docs/licensing.md)。参与贡献见 [CONTRIBUTING](CONTRIBUTING.md)，漏洞报告见 [SECURITY](SECURITY.md)。

## 文档入口

完整目录见 [文档中心](docs/README.md)。

| 任务 | 文档 |
| --- | --- |
| 接入现有数据库和反代，最小启动 | [快速开始](docs/getting-started.md) |
| 注册设备、管理隧道、实际访问 | [使用指南](docs/usage.md) |
| 查命令、参数、环境和反代要求 | [配置参考](docs/configuration.md) |
| 理解系统和代码 | [系统架构](docs/architecture.md)、[技术实现](docs/implementation.md) |
| 理解存储、身份和授权 | [数据模型](docs/data-model.md)、[安全设计](docs/security.md) |
| 调用接口 | [接口说明](docs/api.md)、[OpenAPI](docs/openapi.yaml)、[管理协议](docs/agent-protocol.md) |
| 构建与验证 | [开发指南](docs/development.md)、[测试指南](docs/testing.md) |
| 开源许可、CI 产物和标签发版 | [许可证说明](docs/licensing.md)、[发布指南](docs/releasing.md) |
| 运行维护 | [维护手册](docs/operations.md)、[故障排查](docs/troubleshooting.md) |
| 设计取舍与验证范围 | [架构决策](docs/decisions.md)、[版本证据](docs/validation/README.md) |

## 交付组件

| 程序 | 职责 |
| --- | --- |
| `altasci-control` | Web、REST API、设备管理 WSS、PostgreSQL、策略协调和审计 |
| `altasci-edge` | GOST 数据入口、Provider/Visitor 授权、连接和策略快照 |
| `altascitunnel` | 设备身份、管理连接、配置应用、内嵌 GOST 和本地 IPC |

Control 与 Edge 同机分进程；客户端为单实例程序。目标是客户端需要连接的服务地址。管理在线、配置已应用、反向连接成功与目标服务健康分别判断。

## 最小启动

在发布包解压目录运行。示例为 Linux amd64；ARM64 将目录替换为 `bin/linux-arm64`。数据库和 HTTPS/WSS 反代使用已有体系。

```sh
export DATABASE_URL='postgres://USER:PASSWORD@DB_HOST:5432/TEST_DB?sslmode=disable'
mkdir -m 700 test-data
chmod +x bin/linux-amd64/altasci-control bin/linux-amd64/altasci-edge
bin/linux-amd64/altasci-control keygen --key test-data/secrets.key
bin/linux-amd64/altasci-control migrate
bin/linux-amd64/altasci-control admin-create --key test-data/secrets.key --username admin --password-stdin
```

输入至少 12 个字符的密码后回车。两个终端分别运行：

```sh
bin/linux-amd64/altasci-edge --listen 127.0.0.1:18080 \
  --admin "unix://$PWD/test-data/edge.sock" --policy "$PWD/test-data/policy.json"
```

```sh
# 同一目录；新终端也要设置 DATABASE_URL。
bin/linux-amd64/altasci-control serve --listen 127.0.0.1:18081 \
  --edge "unix://$PWD/test-data/edge.sock" --key "$PWD/test-data/secrets.key" \
  --public-url https://YOUR_TEST_ORIGIN --web "$PWD/web/dist"
```

反代精确路径 `/ws` 到 `127.0.0.1:18080`，`/agent/v1/ws` 和其他路径到 `127.0.0.1:18081`。前两个路径支持 WebSocket，保留原始 Host（含非默认端口）。完整步骤见 [快速开始](docs/getting-started.md)。

## 构建与范围

Go 版本见 [go.mod](go.mod)，前端使用 Node.js 24 和 [锁文件](web/package-lock.json)。Windows 完整构建执行 `scripts/build.ps1`，Linux 执行 `scripts/build.sh`；生成、交叉编译与打包见 [开发指南](docs/development.md)。

当前支持单 Edge、内部管理员、IPv4 TCP/UDP；没有多租户、多 Edge、高可用、P2P、TUN、远程 shell 或自动更新。UDP 通过 TCP/WebSocket 承载，存在队头阻塞。

v0.1.1 的 WSL 22 项、Windows Sandbox 23 项和 Linux race 记录见 [固定版本验证](docs/validation/0.1.1/README.md)。普通 Go 测试可能跳过外部依赖用例；历史结果不代替当前代码的验收。

依赖许可见 [第三方声明](THIRD_PARTY_NOTICES.md)，GOST 变更见 [补丁维护](patches/README.md)。
