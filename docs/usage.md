# 使用指南

## 前提

Control、Edge 和反代已按 [快速开始](getting-started.md) 启动。客户端能访问外部 WSS origin，客户端自己能连接要提供的 IPv4 服务。Visitor 使用官方 gost v3.3.0，准确兼容范围见 [验证记录](validation/0.1.1/README.md)。

## 以前台方式试用客户端

Linux：

```sh
chmod +x /path/to/altascitunnel
/path/to/altascitunnel run --data-dir "$HOME/altasci-client-test" --server wss://YOUR_TEST_ORIGIN
```

Windows PowerShell：

```powershell
.\altascitunnel.exe run --data-dir "$env:LOCALAPPDATA\AltasCI-Test" --server wss://YOUR_TEST_ORIGIN
```

使用私有测试 CA 时给 run 增加 `--ca-file /path/to/ca.pem`。Windows 已有服务运行时不能再启动前台实例，应使用另一台测试机器或先停止已有服务。

## 注册设备

登录 Web 的设备页面，创建注册申请。token 有效 10 分钟，只消费一次。保持上述 run 进程运行，另开终端执行：

```sh
altascitunnel register --data-dir /same/client/directory --token 'ONE_TIME_TOKEN'
altascitunnel status --data-dir /same/client/directory
```

服务模式可使用控制台生成的带 `--server` 命令。手工测试使用相同数据目录并由 run 提供服务器/CA 参数。看到“accepted”只说明本地 IPC 接受了请求，应继续确认 ClientID 非空且管理状态 online。

token 过期、撤销或输入错误时重新创建申请。成功注册后重启/升级保留目录即可，不需要重新注册。私钥丢失时旧 token 无法恢复原身份，应撤销旧设备并注册新设备。

## 创建和访问 TCP 隧道

在 Web 中选择设备，创建 TCP 隧道，填写客户端实际可达的目标，例如 `127.0.0.1:18090`。TunnelID 在创建时分配，不等连接成功才生成。

测试时可在客户端运行 HTTP 服务：

```sh
python3 -m http.server 18090 --bind 127.0.0.1 --directory /path/to/test-files
```

Windows 使用已安装的 Python 时可改为 `py -3 -m http.server ...`。等待隧道 connected，再发放 Visitor 凭据，展示/下载命令或 YAML，在访问方运行。导出命令包含实际用户名、秘密、TunnelID、路径和证书验证参数，不要手写虚构凭据替代它。

```sh
gost -L 'tcp://127.0.0.1:2222/203.0.113.9:65000' \
  -F 'tunnel+wss://USER:SECRET@YOUR_TEST_ORIGIN:443?path=/ws&tunnel.id=TUNNEL_ID&secure=true'
curl --fail http://127.0.0.1:2222/probe.txt
```

上例说明参数结构，实际以页面导出为准。占位目标用于证明固定目标生效，真实拨号仍为管理员配置的客户端目标。目标是 SSH 时，可用 `ssh -p 2222 USER@127.0.0.1` 访问。

使用私有 CA 时，在 `-F` 查询参数末尾增加 URL 编码的 `&caFile=/path/to/ca.pem`，或配置 YAML 的 dialer TLS CAFile。CA 文件必须存在于 Visitor 机器；不要用关闭证书验证解决名称或信任错误。

## 创建和访问 UDP 隧道

在客户端启动 UDP 回显器：

```python
import socket
s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
s.bind(('127.0.0.1', 18091))
while True:
    data, peer = s.recvfrom(65535)
    s.sendto(data, peer)
```

Web 创建 UDP 隧道，目标 `127.0.0.1:18091`，为这条隧道单独发放 Visitor。使用该隧道导出的 `-F`：

```sh
gost -L 'udp://127.0.0.1:1053/203.0.113.9:65000?keepalive=true&ttl=60s' -F 'EXPORTED_FORWARD_URL'
```

在访问方执行：

```python
import socket
s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
s.settimeout(10)
s.sendto(b'udp-test', ('127.0.0.1', 1053))
assert s.recvfrom(65535)[0] == b'udp-test'
print('UDP PASS')
```

UDP 运行在可靠 TCP/WebSocket 上，性能与丢包行为不同于原生 UDP。Visitor 的本地端口不需要在服务器公开。

## 编辑、停用和撤销

| 操作 | 预期 |
| --- | --- |
| 改名称 | 保存新资源版本，未变运行对象可保留 |
| 改目标/协议 | generation 与 Provider 更新，旧业务断开，按新目标恢复 |
| 停用隧道 | 新访问被拒绝，相关现有连接关闭 |
| 启用隧道 | 新版本恢复反向连接，无需重注册设备 |
| 删除隧道 | 逻辑资源删除，旧授权失效 |
| 撤销 Visitor | 旧凭据及现有会话失效，其他有效凭据不因此自动撤销 |
| 撤销设备 | 该设备的管理/隧道授权失效，原身份不能重新认证 |

“边缘策略已生效”与设备配置 applied、隧道 connected 分别查看。真实 HTTP/UDP 返回值是业务验证依据。

## 按需安装为服务

临时联调可一直用前台 run。需要测试安装行为时，Linux root 运行：

```sh
sh scripts/install-linux.sh /absolute/path/to/altascitunnel
sudo altascitunnel register --server wss://YOUR_TEST_ORIGIN --token 'ONE_TIME_TOKEN'
sudo altascitunnel status
```

脚本创建专用用户、复制固定路径和 systemd unit，再次执行会替换程序并重启服务。Windows 管理员安装对应版本 MSI 后运行 `%ProgramFiles%\AltasCITunnel\altascitunnel.exe` 的 register/status，现有终端可能需要刷新 PATH。

Linux 普通卸载保留身份：`sh scripts/uninstall-linux.sh`；彻底卸载使用 `--purge`。Windows MSI 完整卸载清除身份，修复/升级保留身份。需要自定义 CA 的服务配置、备份和权限见 [配置参考](configuration.md) 与 [运行维护](operations.md)。
