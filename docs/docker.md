# Docker：单镜像运行服务端

本方案把 **Control、Edge 和已构建的 Web 控制台放在一个镜像、一个容器内**。PostgreSQL 使用你已有的数据库；HTTPS/WSS、证书和反代路由使用你已有的反向代理。镜像不运行数据库、反向代理或设备客户端。

## 前提与目录

在 Linux Docker Engine 上使用 Docker Compose v2、BuildKit，从仓库根目录执行本文命令。Windows/macOS 需要运行 Linux 容器。本次操作仅接入已有测试基础设施，不安装或接管宿主机服务。

构建上下文使用 Git 工作树或独立源码 ZIP 的解压目录。完整二进制发布包同时附带 Docker/Compose 文件供参考，但其根目录没有展开的 Go/npm 源码；请先解压 `sources/` 中的项目源码 ZIP，再进入源码目录构建。

| 文件 / 路径 | 用途 |
| --- | --- |
| [Dockerfile](../Dockerfile) | Node/Go 多阶段构建、许可收集、精简运行镜像 |
| [compose.yaml](../compose.yaml) | 唯一服务 `server`，两个业务端口、数据卷和运行时 tmpfs |
| [compose.env.example](../compose.env.example) | 外置数据库、外部域名、镜像版本和宿主机端口示例 |
| `/data/secrets.key` | 平台原始 32 字节加密密钥，必须与数据库配套保留 |
| `/data/policy.json` | Edge 最近确认的授权策略快照，重建后继续使用 |
| `/run/altasci/edge.sock` | 容器内管理 Unix socket，临时文件，不对外发布 |
| `/app/web/dist` | 构建好的前端，由 Control 直接提供 |

默认数据卷为 `<Compose项目名>_server-data`，本文件的项目名是 `altasci`，因此通常为 `altasci_server-data`。使用 `-p` 或 `COMPOSE_PROJECT_NAME` 会改变名称；后续命令必须保持同一项目名，避免误用新的空卷。

## 1. 配置外置数据库和外部 origin

```sh
cp compose.env.example compose.env
chmod 600 compose.env
# 用自己的编辑器修改 compose.env。
```

至少填写：

```dotenv
DATABASE_URL='postgres://USER:PASSWORD@DB_HOST:5432/TEST_DB?sslmode=verify-full'
ALTASCI_PUBLIC_URL=https://tunnel.example.com
ALTASCI_VERSION=dev
```

数据库须已创建，账号有迁移建表权限。连接串特殊字符需要 URL 编码；TLS 参数按你的数据库要求填写。`verify-full` 要求证书信任与数据库主机名匹配。只有在明确允许的本机/隔离测试链路中才改成 `sslmode=disable`。

`DB_HOST` 必须能从容器中访问。容器里的 `127.0.0.1` 是容器自身，不是宿主机或数据库服务器。如果数据库运行在其他容器，把两者接入同一已有 Docker 网络后使用其服务名；如果数据库只监听宿主机回环地址，普通 bridge 容器无法直接连接，需要调整测试数据库的可达地址或使用后文 host 网络方案。

`ALTASCI_PUBLIC_URL` 是浏览器和设备实际访问的 HTTPS origin，例如 `https://tunnel.example.com:18443`，不带尾斜杠、`/api` 或 `/ws`。入口进程把该变量转换成 Control 的 `--public-url`；直接运行 Control 时仍按 [配置参考](configuration.md) 传参。

本文始终显式使用 `--env-file compose.env`，避免读入你已有的根目录 `.env`。`compose.env` 已被 Git 忽略，也不进入 Docker 构建上下文。Compose 的 `config` 输出可能包含连接串，请勿公开。

## 2. 构建唯一服务端镜像

```sh
docker compose --env-file compose.env build server
```

默认得到 `altasci-server:dev`，版本可由 `ALTASCI_VERSION` 指定并同步显示在 Web 中。Dockerfile 只编译 `altasci-control`、`altasci-edge` 和容器入口 `altasci-server`，不包含设备客户端程序。

最终镜像使用 Distroless nonroot，以 UID/GID `65532:65532` 运行，不含 shell、Node.js、Go 编译器或包管理器。不要使用 `docker exec ... sh` 排障；使用日志、健康检查和下面的管理命令。

也可以不使用 Compose 构建：

```sh
docker build --build-arg VERSION=dev -t altasci-server:dev .
```

构建需要访问基础镜像仓库、Go 模块和 npm。Dockerfile 的 `GO_IMAGE`、`NODE_IMAGE`、`RUNTIME_IMAGE` 参数可替换为你可信的镜像镜像源或固定 digest；默认标签随基础镜像更新，不能宣称逐字节可重现。

## 3. 首次初始化并创建管理员

首次使用一个新的测试数据库与空数据卷时执行：

```sh
docker compose --env-file compose.env run --rm --no-deps server init
```

`init` 在密钥不存在时生成密钥，然后执行数据库迁移；已存在的有效密钥保持不变，损坏的密钥会报错，绝不自动覆盖。普通 `serve` 启动不会生成密钥、自动迁移或重置管理员。

**接入已有 AltasCI 数据库时，先恢复该数据库原来对应的 `secrets.key` 到数据卷，再执行迁移。不能给旧数据库生成一把新密钥。**

创建管理员，密码至少 12 个字符。下面的 Bash 读取不会把密码写入命令历史或容器环境变量：

```sh
read -r -s -p 'Administrator password: ' ADMIN_PASSWORD
printf '\n'
printf '%s\n' "$ADMIN_PASSWORD" | docker compose --env-file compose.env run \
  --rm --no-deps -T server admin-create --username admin --password-stdin
unset ADMIN_PASSWORD
```

创建管理员不是重置密码命令，账号已存在时会报错。初始化失败后检查错误，再重试同一个项目与数据卷。

## 4. 启动与现有反代接入

```sh
docker compose --env-file compose.env up -d server
docker compose --env-file compose.env ps
docker compose --env-file compose.env logs --tail=100 server
```

默认只发布到宿主机 `127.0.0.1`：

| 外部请求 | 宿主机上的反代后端 | 要求 |
| --- | --- | --- |
| 精确 `/ws` | `http://127.0.0.1:18080` | WebSocket Upgrade、保留路径、双向长连接 |
| 精确 `/agent/v1/ws` | `http://127.0.0.1:18081` | WebSocket Upgrade、保留路径、双向长连接 |
| 其他路径 | `http://127.0.0.1:18081` | Web、API；保留原始 Host，包括非默认端口 |

这里只给出路由契约，不指定反代产品，也不安装/配置 TLS。空闲超时应覆盖 15 秒心跳并留余量。内部 `/internal/v1/*` 只在 Unix socket 上，不得加入外部路由。

如果宿主机端口冲突，在 `compose.env` 修改 `ALTASCI_EDGE_PORT`、`ALTASCI_CONTROL_PORT` 并同步修改反代后端；容器内部端口仍是 18080/18081。

如果你的反代也运行在容器中，让它与 `server` 加入同一已有 Docker 网络，通过 `http://server:18080`、`http://server:18081` 访问。反代容器的 `127.0.0.1` 指向反代自身。可使用 Compose override 添加网络；此时宿主机端口发布可以移除，管理 socket 始终留在服务容器内。

## 5. 验证与健康状态

```sh
curl --fail http://127.0.0.1:18081/health/live
curl --fail http://127.0.0.1:18081/health/ready
docker compose --env-file compose.env exec -T server \
  /usr/local/bin/altasci-server healthcheck
```

Docker 健康检查同时验证：Control 能读取数据库、Edge 管理 socket 可用、Edge 数据端口可连接。空白库尚无授权策略时，Edge 自己的 `/health/ready` 可以是 503，因此容器健康检查使用其 `/health/live`，不把“尚无策略”误报为进程故障。

健康状态不证明外部 TLS/反代、设备在线或实际业务连通。还需从外部域名登录、注册测试设备并完成 [TCP/UDP 验证](usage.md)。Docker 的 `unhealthy` 本身不会触发 `restart`；进程退出才适用重启策略。

## 进程、停止与恢复

容器入口直接启动两个现有服务程序，并把日志写入标准输出/错误。任一子进程退出（即使退出码为 0），入口会停止另一个并以非零状态退出，由 Compose 重启整个容器。不会留下一个看似运行正常、实际只剩半个服务端的容器。

```sh
docker compose --env-file compose.env stop server
docker compose --env-file compose.env start server
docker compose --env-file compose.env down
```

`stop`/`down` 将 SIGTERM 转发给两个进程并等待退出，超过 15 秒才强制终止；Compose 留出 30 秒。单容器意味着 Control 故障也会使 Edge 一起重启，已有数据连接会中断；这与独立部署时 Control 停止而 Edge 可继续转发的边界不同。

上述命令保留数据卷和外部数据库。**`down -v` 会删除项目数据卷及其中的加密密钥，不要用于保留测试身份和授权数据的环境。** 数据库、密钥和策略快照备份须对应，见 [备份与恢复](operations.md)。

## 升级与管理命令

停止旧容器、备份数据库和数据卷，然后构建新镜像、显式迁移、启动：

```sh
docker compose --env-file compose.env stop server
# 在这里按自己的体系完成数据库和数据卷备份。
docker compose --env-file compose.env build server
docker compose --env-file compose.env run --rm --no-deps server migrate
docker compose --env-file compose.env up -d server
```

同一镜像还支持：

```sh
# 已有密钥时 keygen 会拒绝覆盖。
docker compose --env-file compose.env run --rm --no-deps server keygen
# 仅在恢复旧数据库的专门流程中使用，数值按恢复手册确定。
docker compose --env-file compose.env run --rm --no-deps server \
  recover-revisions --minimum-revision YOUR_RECOVERY_FLOOR
```

`recover-revisions` 的完整条件见 [恢复旧数据库](operations.md#恢复旧数据库)。不要把恢复命令当作正常启动步骤。不要同时启动多个容器对同一数据卷/数据库运行本项目服务端；当前是单 Control、单 Edge 架构。

## 非默认网络和私有数据库 CA

如果使用 Linux host 网络，可以直接复用宿主机回环监听的 PostgreSQL。此模式不使用 `-p` 映射，必须同时把两个业务 listener 改为回环地址，避免监听整个宿主机网卡：

先停止原 Compose 服务，确认镜像版本与实际卷名，并在当前 shell 显式导出 `DATABASE_URL`、`ALTASCI_PUBLIC_URL`，下面的 `--env` 才会将它们传入。

```sh
docker run -d --name altasci-server --network host \
  --env DATABASE_URL --env ALTASCI_PUBLIC_URL \
  -e ALTASCI_CONTROL_LISTEN=127.0.0.1:18081 \
  -e ALTASCI_EDGE_LISTEN=127.0.0.1:18080 \
  --mount source=altasci_server-data,target=/data \
  --tmpfs /run/altasci:uid=65532,gid=65532,mode=0700 \
  --read-only --cap-drop ALL --security-opt no-new-privileges \
  --stop-timeout 30 --restart unless-stopped altasci-server:dev
```

这个例子沿用已完成初始化的卷，不能与原容器并行启动。`compose.env.example` 的连接串采用单引号以适配 Compose；`docker run --env-file` 不解析这种引号，因此这里不混用环境文件。更推荐按自己的环境修改 Compose 网络配置。

数据库使用私有 CA 时，把 CA 文件只读挂载，例如 `/certs/db-ca.pem`，并在 `DATABASE_URL` 增加 `sslrootcert=/certs/db-ca.pem`，保持 `sslmode=verify-full`。该文件必须可被容器 UID 65532 读取。它是数据库连接的 CA，不负责外部 WSS 证书，也不会改变设备的信任配置。

## 权限、许可和排障

- named volume 自动继承镜像中的 `/data` 所有权。使用 bind mount 时，在宿主机预建私有目录并授予 UID/GID 65532 读写权限；不要用 `chmod 777` 处理身份目录。
- 找不到密钥：先核对项目名、卷挂载与初始化步骤；已有数据库时恢复原密钥。
- 数据库连接失败：检查容器内的可达地址、账号权限、URL 编码及数据库 TLS；`sslmode=disable` 不能修复网络不可达。
- 502/设备不上线：核对两条 WS 路径、完整 Host、外部 HTTPS origin、证书和反代超时。
- 镜像的业务 listener 通过显式 `--allow-non-loopback` 监听容器网卡；直接使用 Control/Edge 时默认仍只允许 loopback。Edge 内部管理接口不接受这一例外。

项目与第三方许可位于 `/usr/share/altasci/`，包含 MIT LICENSE、聚合许可清单和对应 MPL 模块源码；项目源码归档在 `/usr/share/altasci/sources/project-source.tar.gz`。可以用 `docker cp` 导出，不需要容器 shell。许可清单保守包含现有收集器审查的项目组件，不宣称是运行基础镜像的完整 SBOM；Distroless 自带的系统许可保留在基础镜像中。

构建、健康与进程测试范围见 [测试指南](testing.md)。镜像可通过 `docker save` / `docker load` 在测试服务器间转移，当前步骤不推送任何镜像仓库，也不执行生产部署。
