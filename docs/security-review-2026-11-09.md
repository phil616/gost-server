# 依赖安全复查（2026-11-09）

基线：`8856d32`。通过 GitHub Dependabot API 读取到 8 条 open 告警，并补充扫描根 Go 模块、本地 GOST tunnel 路径及前端锁文件。此前 [2026-10-29 记录](security-review-2026-10-29.md) 保留为历史证据，本记录更新依赖处置结果。

## 修复

| 依赖 | 修改 | 公告与原因 |
| --- | --- | --- |
| github.com/andybalholm/brotli | 根模块 1.1.1、本地 GOST 1.0.6 → 1.2.7 | [GO-2026-6888](https://pkg.go.dev/vuln/GO-2026-6888)：畸形压缩流可导致过量内存分配；扫描发现可达解压路径 |
| github.com/klauspost/compress | 两个模块均从 1.19.0 → 1.20.2 | [GO-2026-6865](https://pkg.go.dev/vuln/GO-2026-6865)：畸形帧可触发 panic；扫描发现可达压缩/解压路径 |
| decode-uri-component | 0.2.2 → 0.5.1，通过 overrides 固定 | [GHSA-vcc3-ghjq-m6fr](https://github.com/advisories/GHSA-vcc3-ghjq-m6fr)、[GHSA-qr28-fpjm-wq7f](https://github.com/advisories/GHSA-qr28-fpjm-wq7f)，对应 GitHub #56 / #72；0.5.0 仍受高危二次复杂度拒绝服务问题影响 |
| follow-redirects | 1.16.1 → 1.16.3，通过 overrides 固定 | [GHSA-mrwp-f3q8-3g9p](https://github.com/advisories/GHSA-mrwp-f3q8-3g9p)：跨 realm 敏感头配置可能被丢弃，跨站重定向泄露自定义凭据；本次 npm 扫描新增高危，尚未出现在这 8 条 GitHub 告警中 |

解码器 0.5.1 使用 ESM default 导出，而 Umi 的 `query-string 6.14.1` 使用 CommonJS。保留查询参数接口，仅通过 [兼容补丁](../web/patches/README.md) 将调用改为读取 `.default`。`patch-package --error-on-fail` 在安装时应用；Docker 和 PowerShell 临时构建均携带补丁。新增测试覆盖 Umi 参数解析、序列化与导航，以及在独立进程中有超时限制的两类恶意解码输入。

本地锁文件已移除 #56 / #72 对应的受影响版本；另外 6 条 GitHub 告警仍保留。没有在远程执行 dismiss、提交或推送；告警需在修复合入默认分支、GitHub 重新扫描后更新。

## 保留项

| 包 / GitHub 告警 | 适用范围与保留理由 |
| --- | --- |
| react-router 6.30.6 / #55、#69 | 两个中危公告要求 7.18.0。当前 Umi 仍依赖 6.x 接口；没有启用 SSR，也未向 Link/useNavigate 传入不可信跳转地址。生产产物含此包，保留残余风险并随 Umi 路由兼容升级处理 |
| @hono/node-server 1.19.17 / #71 | 中危静态路径中间件绕过，修复分支为 2.x。来自未启用的 Utoo 构建器；生产 HTTP 服务由 Go 提供，不使用其 serveStatic |
| esbuild 0.18.20 / #16 | 中危开发服务器跨源读取，来自旧 esbuild-kit 加载器。当前构建不调用其 serve API；开发服务仅限本机，暂不跨版本替换加载器 |
| deepmerge 4.3.1 / #70 | 低危原型污染，尚无已发布修复版本；仅构建工具使用，输入为仓库资源 |
| elliptic 6.6.1 / #28 | 低危且尚无已发布修复版本；构建工具的加密 polyfill，设备认证与加密在 Go 中实现 |
| glob / minimatch | 本次 npm 扫描新增中危取消遍历失效和模式放大公告，分别为 [GHSA-3g3m-2fjr-6pw4](https://github.com/advisories/GHSA-3g3m-2fjr-6pw4)、[GHSA-g3hv-75wg-m249](https://github.com/advisories/GHSA-g3hv-75wg-m249)。受影响版本来自未启用的 Mako 构建器，不处理网络输入；暂不跨主版本替换其 glob API |
| x/crypto/openpgp | 无修复的废弃公告，未导入或调用该包；保留必要的 x/crypto 模块 |

Webpack 生产模块清单中，上述保留的 npm 包只有 React Router 进入当前前端产物。npm 的依赖传播告警不等于独立漏洞数量：修复前为 36 项（5 high、19 moderate、12 low），修复后为 32 项（0 high、0 critical、20 moderate、12 low）。本次没有使用强制 audit fix 或为消除告警升级整套路由框架。

## 验证

- 根模块 Linux / Windows amd64 `govulncheck v1.9.0`：均为 0 个可达漏洞；另有 1 个未调用的模块级公告。扫描工具以项目 Go 1.27.3 构建，Windows 使用原生 Linux 扫描器设置 `GOOS=windows` 分析。
- 本地 GOST `handler/tunnel`、`connector/tunnel` 扫描：未发现漏洞。
- `go test -mod=readonly -race -count=1 -timeout 10m ./...` 通过；未配置 `TEST_DATABASE_URL`、`GOST_BINARY`，依赖外部数据库及官方 GOST 的测试按条件跳过。
- 前端类型检查、16 项测试和 Webpack 生产构建通过；`npm audit --package-lock-only` 无 high / critical。
- Docker `web-build` 阶段构建通过：在干净环境执行 `npm ci` 自动应用补丁，随后通过类型检查、16 项测试和生产构建。未构建完整运行时镜像或执行 Windows 安装验收。
- `go vet ./...`、`go build -mod=readonly ./cmd/...`、分发许可证收集/校验及文档引用检查通过。

以上依赖于执行时的公告数据库与静态调用分析，不覆盖未知漏洞，也不等于生产部署或完整系统验收。
