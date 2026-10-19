# 开源许可与发布流程验证

日期：2026-10-19。范围是 MIT 授权、依赖分发合规材料、打包与 GitHub Actions 定义；不修改 GOST 数据协议或服务运行逻辑。

本机环境：Windows amd64、Go 1.27.0、Node 24、PowerShell 7、WiX 4.0.6 与临时 .NET 8 runtime。测试包使用 `0.1.2-ci.1` 等临时版本标签，不代表已推送或正式发版。

## 已执行

- 前端干净安装、类型检查、2 个 Vitest 用例与生产 Webpack 构建通过。
- Go `go test -count=1 ./...` 与 `go vet ./...` 通过；首次普通测试未配置外部变量，另行准备校验过的官方 GOST 与浏览器执行集成验证。
- 修复截图路径后，Windows 上设置 GOST_BINARY、ALTASCI_BROWSER=1 重跑 `go test -count=1 -timeout 10m ./...` 通过，包含官方原生 TCP/UDP、Daemon WSS 与真实 Chrome 管理操作，integration 包运行 31.314 秒；本次没有重新验证 PostgreSQL、Linux race 或系统安装。
- 发布依赖收集通过：171 个组件，含 72 个 Go 模块、98 个生产 npm 模块及 Webpack 生成运行时的许可；HCL v1.0.0、yamux v0.1.1 的原始 MPL 源码 ZIP 已包含。Go 标准库许可同时收集。
- `@stagewise/toolbar` 不在生产编译清单中；发布清单中没有 AGPL/GPL 或未知许可。
- 版本解析测试覆盖正式、预发布、MSI 上限及非法标签；许可反例测试覆盖 AGPL、缺正文、源码越界、缺失与篡改。Actions 文件通过 actionlint，Bash 步骤通过 WSL ShellCheck warning 级检查。
- Windows amd64、Linux amd64/arm64 的三个程序均实际交叉编译成功；分发校验器检查 PE/ELF 和目标架构。
- MSI 已实际构建并解开 CAB，程序/MIT/声明摘要及内嵌 MPL 源码检查通过；错误 ProductVersion 反例被拒绝。
- 完整 ZIP 已实际生成；逐文件 SHA-256、源码 ZIP、许可清单与 MPL 源码摘要检查通过。修改归档后外层摘要拒绝；重新计算外层摘要仍被内部文件摘要拒绝。

## 发现并修复

补跑 Windows 官方 GOST/浏览器用例时发现：全新工作区没有 `.local` 时，浏览器在业务操作完成后写截图报 ENOENT。`browser-smoke.mjs` 现按截图实际路径创建父目录，避免干净 CI 检出在最后一步失败。

## 验证边界

本地没有 Git remote；没有远端 Actions run、GitHub Release 或可见性修改。本次不执行 MSI 安装、WSL 系统生命周期、真实服务器反代和长期运行验收。后续按 [发布指南](../releasing.md) 推送后观察真实 CI 结果，按 [系统验收](../system-acceptance.md) 验证安装。

本轮新增的安装包检查会解开 MSI 的实际 CAB，核对程序、MIT 许可和第三方声明的 SHA-256，再从 CAB 内的 licenses.zip 检查许可和 MPL 源码；它验证安装包内容，不代表服务安装测试。
