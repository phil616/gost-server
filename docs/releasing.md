# CI、打包与 GitHub Release

## 自动化入口

| 事件 | 工作流 | 行为 |
| --- | --- | --- |
| 任意分支 push | CI | 文档、前端、依赖许可、Linux 集成/race/浏览器、Windows 测试，通过后打包 |
| Pull Request | CI | 同上；仅只读仓库权限，不发布 Release，不读取发布秘密 |
| 手动 Run workflow | CI | 对选定分支重复验证并生成临时产物 |
| 推送 v 开头标签 | Release → 复用 CI → publish | 严格检查版本、完整构建验证、创建草稿、上传全部文件、最后公开 |

当前工作流：[CI](../.github/workflows/ci.yml)、[Release](../.github/workflows/release.yml)。Actions 固定到核对过的 commit SHA，由 [Dependabot](../.github/dependabot.yml) 每月提出依赖更新；更新仍须走 CI 与许可审查。

普通 CI 的包版本为 `0.0.0-ci.RUN_NUMBER.RUN_ATTEMPT`，MSI 内部数字版本为 `0.0.0`。这些是短期测试包，不能代替正式版本或证明部署验收通过。`frontend-internal` 是 CI 任务间传递生产 Web/许可的中间产物，保留 3 天；对外分发下载 `release-packages`，保留 14 天。组织或仓库策略可能进一步缩短保留期。

## 首次公开仓库

1. 在 GitHub 创建 public 仓库，或在已有仓库 Settings 中由所有者修改可见性。
2. 提交当前项目改动，配置自己的 remote 后 push。此步骤可能包含前一轮文档整理的改动，提交前检查 diff。
3. 启用 Actions。构建只需默认只读 GITHUB_TOKEN，Release 的 publish 任务单独请求 contents:write；无需 PAT、生产数据库或部署密钥。组织策略必须允许该任务写 Release。
4. 观察第一次真实工作流结果及 `release-packages` 中的 ZIP。工作流存在和本地验证不等于已在 GitHub 成功执行。
5. 可在仓库 Security 中启用 Private Vulnerability Reporting，并按需要设置默认分支和版本标签保护规则。

本轮没有自动创建远端仓库、推送提交、改可见性或创建真实版本标签。

## 正式版本和预发布

```sh
# 提交并推送已审查、已验证的改动后：
git tag -a v0.1.2 -m "AltasCI Tunnel 0.1.2"
git push origin v0.1.2
# 预发布示例：v0.1.3-rc.1
```

标签必须是 `vMAJOR.MINOR.PATCH`，可带 SemVer 预发布后缀，例如 `-rc.1`。不接受前导零或 `+build`。受 MSI 约束，major/minor ≤255，patch ≤65535。预发布自动标记 prerelease，不标记 latest；正式版本标记 latest。请按版本顺序发布正式标签，避免补发旧标签抢占 latest。

MSI 使用前三段数字，因此同一 `1.2.3` 的 rc 与正式版本共享 MSI ProductVersion。需要在同一测试机切换时先卸载旧测试版本；这套编号不提供完整的 MSI 预发布升级排序。

发布任务在测试/打包完成后创建草稿，将两个 ZIP 和两个 SHA-256 文件上传后公开。失败的草稿可重跑同一工作流补齐；已公开版本拒绝覆盖，修复使用新标签。标签不是任意分支文本插入 shell：版本先校验，再通过环境变量传参。publish 不执行构建脚本，也没有生产部署动作。

自动发布说明由 [GitHub 分类配置](../.github/release.yml) 和 PR 标签生成。签名、Windows 安装、跨服务器反代及长期运行验收仍是独立事项。

## 发布产物

```text
altasci-VERSION.zip
  altasci-VERSION/
    bin/                   Windows exe/MSI、Linux amd64/arm64
    web/dist/              生产控制台及许可材料
    docs/ deploy/ scripts/ patches/
    LICENSE THIRD_PARTY_NOTICES.md README.md
    CONTRIBUTING.md SECURITY.md
    licenses/              原许可、清单、sources/ 下的 MPL 模块 ZIP
    sources/               项目源码 ZIP
    release-manifest.json  版本、提交、dirty、平台、时间和源码路径
    checksums.txt          除自身外每个文件的 SHA-256
altasci-VERSION.zip.sha256
altasci-VERSION-source.zip
altasci-VERSION-source.zip.sha256
```

下载 Actions 的 `release-packages` 后先解开 GitHub 外层包装，再校验里面两个实际 ZIP。Linux：

```sh
sha256sum -c altasci-VERSION.zip.sha256
sha256sum -c altasci-VERSION-source.zip.sha256
unzip altasci-VERSION.zip
cd altasci-VERSION
sha256sum -c checksums.txt
chmod +x bin/linux-amd64/altasci-control bin/linux-amd64/altasci-edge bin/linux-amd64/altascitunnel
```

Windows 可运行 `Get-FileHash -Algorithm SHA256` 比较摘要，也可在源码检出中执行：

```powershell
./scripts/verify-package.ps1 -Archive dist/altasci-VERSION.zip
```

SHA-256 用于检测内容损坏/变化，不等同于独立签名或来源证明。当前 MSI/exe 未代码签名，CI 不修改机器签名策略。

## 本地生成同等结构产物

使用 PowerShell 7、go.mod 指定的 Go、Node 24 和 WiX 4.0.6。先构建前端：

```powershell
./scripts/build-web.ps1 -Verify -Version 0.1.2
$env:CGO_ENABLED='0'
foreach ($platform in @('windows-amd64','linux-amd64','linux-arm64')) {
    $env:GOOS,$env:GOARCH=$platform.Split('-')
    $output=if($env:GOOS -eq 'windows'){'bin'}else{"bin/$platform"}
    New-Item -ItemType Directory -Path $output -Force | Out-Null
    foreach($app in @('altasci-control','altasci-edge','altascitunnel')) {
        $extension=if($env:GOOS -eq 'windows'){'.exe'}else{''}
        go build -mod=readonly -trimpath -o "$output/$app$extension" "./cmd/$app"
        if($LASTEXITCODE -ne 0){throw 'Build failed'}
    }
}
Remove-Item Env:GOOS,Env:GOARCH,Env:CGO_ENABLED
./scripts/build-msi.ps1 -Version 0.1.2
./scripts/package.ps1 -Version 0.1.2
```

版本仅为示例。MSI 编译必须有完整许可目录，可用 `-LicenseDirectory` 指定；`-WixDll` 支持本地工具路径。package.ps1 要求三个目标平台完整，不再静默忽略 Linux 文件。拒绝覆盖已有同名 staging/归档，改版本或按 [清理规则](maintenance.md) 处理。

源码归档使用 Git 枚举现有、未忽略的源文件，包含未提交改动；不再错误地仅归档 HEAD 而漏掉本次修改。dirty 状态如实写入元数据，GitHub CI 要求构建后检出保持干净。文件清单会拒绝环境秘密、依赖安装目录、链接等异常材料。

## 测试边界

CI Linux 配置 PostgreSQL 18、官方 GOST v3.3.0、Chrome、真实 Web 与 Go race；Windows 使用官方 GOST 跑测试后构建 MSI。Linux arm64 是交叉编译产物，当前没有原生 arm64 运行验证。GitHub Windows runner 不执行安装服务的 MSI 生命周期测试；按 [系统验收](system-acceptance.md) 在可丢弃环境单独验证。

本次本地实施的验证记录见 [开源发布流程验证](validation/open-source-release.md)，不要把它或 v0.1.1 历史验证当成远端 Actions 已执行的记录。
