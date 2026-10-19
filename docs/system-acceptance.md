# 系统安装与转发验收

本手册仅用于可丢弃环境中的安装生命周期。使用已有服务器、数据库与反代进行前台联调，请先读 [快速开始](getting-started.md)，不需要执行本页的系统安装或签名策略调整。

本流程会安装、重启和卸载应用，只能在可丢弃的测试环境运行。Windows 使用 Sandbox，Linux 使用 WSL 测试发行版或独立虚拟机。构建与 race 测试需要源码检出目录；运行发布包不需要测试夹具。

## Windows Sandbox

要求 Windows Sandbox 已启用、Go 与 WiX 4 可用，并准备经官方发布 SHA-256 校验的 gost v3.3.0 Windows amd64 程序。未签名 MSI 的 Sandbox 验收会在客体内设置 `VerifiedAndReputablePolicyState=0` 并执行 `CiTool.exe -r`；脚本拒绝在非 `WDAGUtilityAccount` 用户下运行。不要在宿主机单独执行该脚本或修改宿主机签名策略。

在项目根目录执行以下 PowerShell 命令。`$gostBinary` 替换为官方程序路径；使用本地 WiX DLL 时，给两条构建命令补上 `-WixDll /path/to/wix.dll`。

```powershell
$gostBinary='C:\tools\gost.exe'
& $gostBinary -V
$testRoot=Join-Path $env:TEMP ('altasci-sandbox-'+[guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path "$testRoot/input","$testRoot/results" | Out-Null
go build -o bin/altascitunnel.exe ./cmd/altascitunnel
go build -o "$testRoot/input/systemfixture.exe" ./internal/integration/systemfixture
./scripts/build-msi.ps1 -Version 0.1.0
./scripts/build-msi.ps1 -Version 0.1.1
Copy-Item bin/AltasCITunnel-0.1.0-x64.msi,bin/AltasCITunnel-0.1.1-x64.msi "$testRoot/input/"
Copy-Item $gostBinary "$testRoot/input/gost.exe"
Copy-Item scripts/test-windows-sandbox.ps1 "$testRoot/input/"
@"
<Configuration>
  <VGpu>Disable</VGpu>
  <Networking>Enable</Networking>
  <MemoryInMB>4096</MemoryInMB>
  <MappedFolders>
    <MappedFolder><HostFolder>$testRoot\input</HostFolder><SandboxFolder>C:\input</SandboxFolder><ReadOnly>true</ReadOnly></MappedFolder>
    <MappedFolder><HostFolder>$testRoot\results</HostFolder><SandboxFolder>C:\results</SandboxFolder><ReadOnly>false</ReadOnly></MappedFolder>
  </MappedFolders>
  <LogonCommand><Command>powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\input\test-windows-sandbox.ps1</Command></LogonCommand>
</Configuration>
"@ | Set-Content "$testRoot/acceptance.wsb"
Start-Process WindowsSandbox.exe -ArgumentList ('"{0}\acceptance.wsb"' -f $testRoot)
Write-Host "Results: $testRoot/results"
```

等待 `results/result.json` 生成，确认 `passed=true`。保留 transcript、install/repair/upgrade/uninstall 的 MSI 详细日志。失败时读取 `failure.txt`，不能把进程启动或 MSI 构建成功视为验收通过。关闭自己启动的 Sandbox 后，客体内测试服务和信任证书一起销毁。

脚本实际执行：安装 → LocalService 与命名管道 → 注册 Ed25519 身份 → 官方 GOST TCP/UDP 经 WSS 访问固定目标 → 杀死服务验证 SCM 自动恢复 → MSI 修复 → 0.1.0 升级到 0.1.1 → 再次验证 TCP/UDP → 卸载 → 检查服务、程序、身份、配置与 PATH 清理。控制与 Edge 夹具只监听客体 loopback，测试证书仅导入客体根证书库；没有跳过 TLS 校验。

`internal/integration/systemfixture` 是测试工具，不属于生产启动程序。它使用内存控制库，Windows MSI 验收不冒充 PostgreSQL 安装验收。

## Linux / WSL

本次实际环境为 WSL2 Debian 13.7、systemd、PostgreSQL 17.11、Nginx 1.26.3、Go 1.27.2 和 GCC 14.2；Windows 上原有 PostgreSQL 18.6 的数据库测试另已通过。Nginx 使用未经修改路由的 `deploy/openresty.conf`，只把域名替换为 localhost。此结果不包含 OpenResty 二进制或公网证书。

先依照 README 在独立测试环境部署真实 PostgreSQL、control、edge 及 `altascitunnel` 三个 systemd 服务。为测试管理员 `acceptance` 设置一次性密码，control 对外地址为 `https://127.0.0.1`，保持默认 Secure Cookie。代理监听 443，测试证书必须有 `IP:127.0.0.1` SAN；将 CA 导入该测试系统的信任库，使客户端服务正常验证证书。Edge 与 control 分别使用 `altasci-edge`、`altasci-control` 用户以及共享 `altasci` 组；密钥和控制环境文件仅授权给该组。客户端由安装脚本创建独立 `altascitunnel` 用户。

首次运行前，客户端服务应已安装但尚未注册；`/opt/altasci` 下的控制程序和数据库应仅用于本次验收。源码目录中的 `bin/linux-amd64/altascitunnel` 必须是本轮构建产物。用官方发布的 gost v3.3.0 并校验 release 的 checksums.txt，然后在 Linux root shell 中运行：

```sh
export ACCEPTANCE_PASSWORD='本次测试管理员密码'
python3 scripts/test-linux-system.py \
  --origin https://127.0.0.1 \
  --ca /etc/altasci/tls/fullchain.pem \
  --gost /absolute/path/to/official/gost \
  --project /absolute/path/to/source \
  --results /absolute/path/to/results
```

脚本以非零状态报告失败，并生成 `result.json`；通过时记录 22 项检查。它会创建注册身份与 TCP/UDP 隧道，停止/启动控制服务，杀死并等待客户端自动恢复，覆盖安装客户端，重启 Edge，撤销 Visitor，保留身份卸载/重装，最后 `--purge` 清除客户端目录。Visitor 连接使用控制 API 实际导出的 `-F` 参数，服务地址不显式写 `:443`，因此覆盖本轮发现的默认端口问题。

测试结束后，停止自己创建的 control、edge 和代理服务，删除本次配置、测试 CA、测试数据库与专用账号。不要把这些可丢弃身份或测试证书保留在其他环境中。服务模板的用户、路径和权限见 [运行维护](operations.md)，其余验证入口见 [测试指南](testing.md)。

## Linux race 与数据库测试

把源码放在 WSL 原生文件系统，而不是在 `/mnt/d` 中安装依赖。使用 `TEST_DATABASE_URL` 指向独立测试库；测试在库内使用随机 schema。执行：

```sh
export TEST_DATABASE_URL='postgres://TEST_USER:TEST_PASSWORD@127.0.0.1:5432/TEST_DB?sslmode=disable'
export GOST_BINARY='/absolute/path/to/official/gost'
go test -race -count=1 -v ./...
go vet ./...
```

确认数据库和官方 GOST 的用例确实运行，不能把缺少变量时的 SKIP 记成通过。v0.1.1 的 race 与规模用例实际范围见 [固定版本记录](validation/0.1.1/README.md)。24 小时浸泡、真实公网和受防火墙限制的跨主机网络仍需另行验收。
