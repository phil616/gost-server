# 快速开始：使用现有数据库与反代

目标是在自己的 Linux 服务器上进行最小联调。使用已有 PostgreSQL、证书和 HTTPS/WSS 反代；只启动 Control 和 Edge，不要求安装数据库、容器或更换反代。

## 1. 准备发布包

解压对应版本发布包并进入其根目录。`bin/linux-amd64` 对应 x86_64，`bin/linux-arm64` 对应 aarch64。前端产物位于 `web/dist`。源码树通过不同方式构建时，二进制路径见 [开发指南](development.md)。

以下在 Bash 中执行：

```sh
export APP_BIN="$PWD/bin/linux-amd64"
export TEST_STATE="$PWD/test-data"
export DATABASE_URL='postgres://USER:PASSWORD@DB_HOST:5432/TEST_DB?sslmode=disable'
export PUBLIC_URL='https://YOUR_TEST_ORIGIN'
chmod +x "$APP_BIN/altasci-control" "$APP_BIN/altasci-edge"
mkdir -m 700 "$TEST_STATE"
```

数据库应为本次测试独立库，账号有建表/迁移权限。连接串中的特殊字符需要 URL 编码；SSL 参数按现有数据库要求填写，示例 `sslmode=disable` 不是跨网数据库的默认建议。

`PUBLIC_URL` 必须是浏览器、客户端和 Visitor 实际使用的 HTTPS origin，可包含 `:18443` 等非默认端口，不带 `/api` 或 `/ws`。程序不会自动读取根目录 `.env`，须显式导出环境变量或使用进程管理器注入。

## 2. 首次初始化

```sh
"$APP_BIN/altasci-control" keygen --key "$TEST_STATE/secrets.key"
"$APP_BIN/altasci-control" migrate
"$APP_BIN/altasci-control" admin-create \
  --key "$TEST_STATE/secrets.key" --username admin --password-stdin
```

输入至少 12 个字符的密码并回车。密钥命令拒绝覆盖已有文件。重启时保留密钥和数据库，不要每次重新 keygen 或重建管理员。

## 3. 启动两个进程

终端 A，在发布包根目录运行：

```sh
bin/linux-amd64/altasci-edge \
  --listen 127.0.0.1:18080 \
  --admin "unix://$PWD/test-data/edge.sock" \
  --policy "$PWD/test-data/policy.json"
```

终端 B，进入同一目录并重新导出 `DATABASE_URL`：

```sh
export DATABASE_URL='postgres://USER:PASSWORD@DB_HOST:5432/TEST_DB?sslmode=disable'
bin/linux-amd64/altasci-control serve \
  --listen 127.0.0.1:18081 \
  --edge "unix://$PWD/test-data/edge.sock" \
  --key "$PWD/test-data/secrets.key" \
  --public-url 'https://YOUR_TEST_ORIGIN' \
  --web "$PWD/web/dist"
```

首次测试可让两个进程由同一普通用户运行，确保状态目录和 socket 可访问。若使用不同账号，分配 socket 父目录/共享组权限，见 [运行维护](operations.md)。修改内部端口时同步修改反代后端。

## 4. 接入现有反代

| 外部请求 | 本机后端 | 处理要求 |
| --- | --- | --- |
| 精确 `/ws` | `127.0.0.1:18080` | 保留路径，支持 WebSocket Upgrade 与双向长连接 |
| 精确 `/agent/v1/ws` | `127.0.0.1:18081` | 保留路径，支持 WebSocket Upgrade 与双向长连接 |
| 其他路径 | `127.0.0.1:18081` | 普通 HTTP，包含 Web 静态资源和 REST |

保留完整外部 Host（包括非默认端口），使用与入口名称匹配且设备信任的证书，空闲超时应覆盖 15 秒心跳并留余量。不要把 `/ws` 的尾斜杠或管理 WS 前缀改写掉。

内部程序只允许 loopback；如果反代处于另一容器或主机，需通过已有本机转发层接入。直接传 `--listen 0.0.0.0:...` 会被拒绝。详细契约见 [配置参考](configuration.md#反代与网络契约)。

## 5. 检查启动

```sh
curl --fail http://127.0.0.1:18081/health/live
curl --fail http://127.0.0.1:18081/health/ready
curl --fail --unix-socket "$PWD/test-data/edge.sock" http://local/health/live
```

Control ready 证明能读取数据库；Edge live 证明内部服务运行。空白测试库尚无授权策略时，Edge `/health/ready` 可能为 503，首次有效策略确认后才就绪。

浏览器打开外部 origin 并登录。创建注册申请，再按 [使用指南](usage.md) 启动本机客户端、注册并进行 TCP/UDP 访问。未知路径可能回退为前端 HTML，`/api/v1/health` 不是健康接口。

## 6. 停止测试

前台 Ctrl+C 停止本次客户端、Visitor、Control 和 Edge。保留测试数据库、`secrets.key` 和设备身份目录可以下次继续；需要全新测试时只清理本次明确创建的状态，不删除共用数据库或现有反代配置。

本流程不安装系统服务。Windows MSI、Linux systemd 的生命周期验证属于 [系统安装验收](system-acceptance.md)，不应在自己的现有环境直接运行破坏性验收脚本。
