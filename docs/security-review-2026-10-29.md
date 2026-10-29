# 安全审查与修复记录（2026-10-29）

## 范围与结论

审查基线：`b6dfe482fb64f0fdea90ea35da27305672981c56`。检查了 GitHub Dependabot 实际告警、根 Go 模块、实际使用的 GOST 路径、前端锁文件、管理员认证、设备注册与重连、凭据加密、隧道授权/撤销、固定目标、IPC、数据库查询、HTTP/WebSocket 边界和部署/构建脚本。

读取 GitHub API 时，当前返回 **67 条 open 告警**，最高编号为 68；没有取得编号 61 的记录。因此不把编号上限当作实际告警数量。原告警为 critical 3、high 18、medium 38、low 8。没有在 GitHub 上执行 dismiss、关闭或发布操作。

按公告的受影响版本范围逐项对照新锁文件和 Go 清单：**61 条已通过升级消除受影响版本，2 条公告已经上游撤回，4 条中低危暂缓**。这表示本地修改的处置结果；只有合入默认分支并重新扫描后，GitHub 告警状态才可能更新。

npm 的统计包含依赖链传播产生的元告警，不能与 GitHub 的公告数直接比较：修复前 55 项（critical 1、high 10、moderate 31、low 13），修复后 32 项（critical 0、high 0、moderate 19、low 13）。剩余直接公告涉及 6 个包、7 个公告，说明见下文。Webpack 生产模块及分发许可证清单表明，这六个包中只有 React Router 进入生产前端；其余五个未进入生产产物。

Go 修复前发现 27 个不同漏洞 ID，其中 16 个有静态调用路径；修复后 Windows/Linux 根模块均没有发现可达漏洞。仍有一个模块级的 openpgp 废弃公告，但本项目没有导入该包。静态可达不等于满足利用条件，也不能证明不存在未知漏洞。

## 已实施修改

| 部分 | 修改及理由 |
| --- | --- |
| Go 工具链 | 三个 go.mod 的最低版本及 Docker 构建镜像统一为 1.27.3，覆盖 HTTP、TLS、模板及证书信任更新；CI 从 go.mod 读取版本 |
| PostgreSQL | pgx 5.7.6 → 5.9.2，覆盖三条数据库驱动告警；继续使用参数化 SQL |
| gRPC | 1.82.1 → 1.83.2，同时修改根模块和 GOST 模块；1.83.1 仍落在部分公告的受影响范围内 |
| Go 扩展库 | x/crypto → 0.58.0、x/net → 0.62.0、x/text → 0.43.0，更新相关传递依赖并重新生成校验文件 |
| Umi / 测试工具 | Umi 4.7.3 → 4.7.28，Vitest 4.1.10 → 4.1.12，更新锁文件 |
| 前端传递依赖 | 在兼容分支固定 Axios 0.34.0、Immer 9.0.21、Vite 7.3.7、path-to-regexp 1.9.0、React Router 6.30.6、Babel 7、fast-uri 3.1.9 和 selector parser 6.1.5；保留原有 React 和 Pro Components 约束 |
| 登录 / CSRF | 登录开始验证 Origin；登录与已认证写请求均验证协议、完整 host:port 及 Origin 结构。生产环境拒绝 HTTP Origin，保留无 Origin 的非浏览器 API 用法 |
| 点击劫持 | 添加 X-Frame-Options: DENY 和 CSP frame-ancestors/base-uri 限制 |
| 设备认证资源耗尽 | 同时最多 64 个认证前连接，超限 503 + Retry-After；30 秒认证上下文覆盖数据库操作，成功和失败均释放名额，已认证设备不占用名额 |
| 慢速请求 | 普通 HTTP 请求读取最多 30 秒；Edge HTTP 请求头及 WebSocket 升级握手各最多 10 秒。长连接及事件流不增加全局写超时 |
| 部署端口 | 修复 deploy-docker.sh 的 0.0.0.0 发布，将两个未加密端口限制到 127.0.0.1，与既有文档及反代部署方式一致 |
| 持续检查 | 添加 Security 工作流，检查 Windows/Linux Go 可达漏洞、npm high/critical 以及部署安全回归；Dependabot 跟踪两个 Go 依赖图、npm 和 Actions |

## 暂缓及保留项

| 包 / 公告 | 评估与处理 |
| --- | --- |
| decode-uri-component 0.2.2 / #56 | 中危畸形百分号解码 DoS，经 Umi 的 query-string 引入；修复版本 0.5.x 切换到 ESM，直接覆盖会改变旧调用方的模块接口。本项目没有自行调用该解码器处理服务端请求，暂不进行不兼容覆盖。浏览器侧依赖仍有残余风险，应随 Umi 历史组件更新继续跟踪 |
| react-router 6.30.6 / #55 | 已消除旧重定向问题 #27，但较新的反斜杠重定向公告要求 7.18.0。本项目跳转使用固定路径，未把不可信输入传给 Link/useNavigate；暂不跨主版本替换 Umi 使用的路由接口。npm 还报告 GHSA-337j-9hxr-rhxg（SSR hydration 的 deserializeErrors 任意构造函数注入），本项目没有使用 SSR，但需继续跟踪，不能声称此包已无漏洞 |
| elliptic 6.6.1 / #28 | 低危且没有已发布修复版本；来自 Webpack 的 Node 加密 polyfill。本项目设备身份与凭据加密均在 Go 中实现，不使用它进行设备签名或密钥生成 |
| esbuild 0.18.20 / #16 | 中危开发服务器跨源读取；剩余版本来自旧 esbuild-kit 加载器，当前构建路径没有调用其 serve API。禁止将开发/测试服务器作为生产入口；保留兼容版本，等待上游加载器更新 |
| @hono/node-server 1.19.17 | npm 新增中危公告 GHSA-rmxm-3fg6-px4f，经未启用的 Utoo 构建器引入；正式 HTTP 服务由 Go 提供，未使用其 serveStatic。修复分支切换到 2.x/ESM，暂缓覆盖 |
| deepmerge 4.3.1 | npm 新增低危公告 GHSA-gcqx-w48m-w2h4，目前没有更新的兼容发布版本。通过 SVG 构建工具引入，构建输入为仓库资源；保留并跟踪 |
| sprintf-js / #62、braces / #60 | GitHub 公告分别在 2026-10-22 和 2026-10-23 撤回；不再按有效漏洞处理，不为已撤回公告强行升级主版本 |
| x/crypto/openpgp / GO-2026-5932 | 模块级无修复公告；本项目只使用相关模块的安全包，没有导入 openpgp，govulncheck 未发现调用。不能为消除此模块提示而删除必要的 Argon2 依赖 |

本次未发现密码明文持久化、字符串拼接不可信 SQL、Provider/Visitor 角色混用、Visitor 更改客户端固定拨号目标、未认证读取凭据或关闭 TLS 证书校验的生产路径。此结论限于所检查代码和测试覆盖。

## 运行边界

- 管理员仍可配置设备能访问的任意 IPv4 目标，这是现有产品授权边界；不提供多租户隔离。
- Edge 管理口依赖 Unix socket 权限；显式使用 loopback TCP 时，同机进程属于信任范围。
- 公网入口仍需要 HTTPS/WSS 反代及连接数、速率和请求大小限制；新增应用限制不能阻止持续占满认证名额或网络带宽的攻击。
- 部署脚本现在只发布回环端口。反代位于其他容器或远端主机时，需要按 Docker 文档配置受控网络；不可直接依赖原来的全网卡明文端口。
- 本次不测试生产环境、不操作既有数据库、不读取仓库外服务秘密。安全扫描不等同于渗透测试、容器基础镜像扫描或 Windows MSI 安装验收。

## 验证

| 检查 | 结果与限制 |
| --- | --- |
| Windows Go | go test -count=1 -timeout 10m ./...、go vet ./...、go build -mod=readonly ./cmd/... 通过；第一次全量测试未配置外部数据库和官方 GOST |
| Linux race + PostgreSQL | Go 1.27.3，独立 PostgreSQL 18 容器，go test -race -count=1 -timeout 10m ./... 通过；实际执行数据库迁移、事务约束和 Control 数据库认证用例。容器及匿名卷已清理 |
| 安全回归 | 跨站登录/写请求、协议及端口不匹配、合法 Origin 和 API 用法、点击劫持响应头、认证前连接名额耗尽及释放均通过 |
| Go 漏洞扫描 | govulncheck v1.9.0 对 Windows/Linux 根模块扫描均为 0 个可达漏洞；GOST tunnel 路径模块检查也无结果。并未声称全部未使用的 GOST 功能均已验证 |
| npm | npm audit --package-lock-only：0 critical、0 high；32 项中低危及传播告警按上述范围保留 |
| 前端 | 使用项目 build-web.ps1 在干净临时目录 npm ci，类型检查、2 个测试文件的 14 项测试及 Webpack 生产构建通过 |
| 许可证 | 新生产构建收集和验证 162 个分发依赖及必须携带的 MPL 源码，通过；没有把整个开发工具依赖树当作分发清单 |
| 脚本与文档 | 部署回环绑定及隔离选项回归、CNB SSH 脚本 9 项回归、发布工具 2 项测试、文档链接检查、git diff --check 均通过 |

补充使用经官方 SHA-256 校验的 GOST v3.3.0，在独立 PostgreSQL 18 容器上执行 Linux 全量 race 测试并通过；实际执行了原生 TCP/UDP、固定目标、WSS 设备注册及 Control 停机恢复用例。100 设备/1000 隧道压力测试因未设置 ALTASCI_STRESS=1 而跳过。未执行 Chromium 浏览器验收、完整 Docker 镜像重建或 Windows MSI 安装；这些仍应由现有 CI/版本验收覆盖。Windows 到 WSL 测试数据库的直接连接尝试因端口不可达失败，数据库回归改在 WSL 内执行并通过。原始扫描与执行日志位于被 Git 忽略的 security-audit/。

## GitHub 原告警逐项对照

以下结果根据本地清单及官方公告的版本范围判定，尚未改变 GitHub 的远端 open 状态。

| 告警 | 依赖 | 等级 | 当前版本 | 处置 |
| --- | --- | --- | --- | --- |
| [#1](https://github.com/phil616/gost-server/security/dependabot/1) / [GHSA-9jj7-4m8r-rfcm](https://github.com/advisories/GHSA-9jj7-4m8r-rfcm) | github.com/jackc/pgx/v5 | critical | 5.9.2 | 已升级消除受影响版本 |
| [#2](https://github.com/phil616/gost-server/security/dependabot/2) / [GHSA-j88v-2chj-qfwx](https://github.com/advisories/GHSA-j88v-2chj-qfwx) | github.com/jackc/pgx/v5 | low | 5.9.2 | 已升级消除受影响版本 |
| [#3](https://github.com/phil616/gost-server/security/dependabot/3) / [GHSA-xgrm-4fwx-7qm8](https://github.com/advisories/GHSA-xgrm-4fwx-7qm8) | github.com/jackc/pgx/v5 | critical | 5.9.2 | 已升级消除受影响版本 |
| [#4](https://github.com/phil616/gost-server/security/dependabot/4) / [GHSA-vp52-pcj8-j9qc](https://github.com/advisories/GHSA-vp52-pcj8-j9qc) | google.golang.org/grpc | high | 1.83.2 | 已升级消除受影响版本 |
| [#5](https://github.com/phil616/gost-server/security/dependabot/5) / [GHSA-qc2q-p7wx-3px3](https://github.com/advisories/GHSA-qc2q-p7wx-3px3) | google.golang.org/grpc | medium | 1.83.2 | 已升级消除受影响版本 |
| [#6](https://github.com/phil616/gost-server/security/dependabot/6) / [GHSA-2v4p-qf9q-27wj](https://github.com/advisories/GHSA-2v4p-qf9q-27wj) | google.golang.org/grpc | high | 1.83.2 | 已升级消除受影响版本 |
| [#7](https://github.com/phil616/gost-server/security/dependabot/7) / [GHSA-wf5p-g6vw-rhxx](https://github.com/advisories/GHSA-wf5p-g6vw-rhxx) | axios | medium | 0.34.0 | 已升级消除受影响版本 |
| [#8](https://github.com/phil616/gost-server/security/dependabot/8) / [GHSA-8jhw-289h-jh2g](https://github.com/advisories/GHSA-8jhw-289h-jh2g) | vite | medium | 7.3.7, 8.3.4 | 已升级消除受影响版本 |
| [#9](https://github.com/phil616/gost-server/security/dependabot/9) / [GHSA-c36v-fmgq-m8hx](https://github.com/advisories/GHSA-c36v-fmgq-m8hx) | immer | high | 9.0.21 | 已升级消除受影响版本 |
| [#10](https://github.com/phil616/gost-server/security/dependabot/10) / [GHSA-33f9-j839-rf8h](https://github.com/advisories/GHSA-33f9-j839-rf8h) | immer | critical | 9.0.21 | 已升级消除受影响版本 |
| [#11](https://github.com/phil616/gost-server/security/dependabot/11) / [GHSA-m6fv-jmcg-4jfg](https://github.com/advisories/GHSA-m6fv-jmcg-4jfg) | send | low | 0.19.2 | 已升级消除受影响版本 |
| [#12](https://github.com/phil616/gost-server/security/dependabot/12) / [GHSA-9wv6-86v2-598j](https://github.com/advisories/GHSA-9wv6-86v2-598j) | path-to-regexp | high | 1.9.0, 8.4.2 | 已升级消除受影响版本 |
| [#13](https://github.com/phil616/gost-server/security/dependabot/13) / [GHSA-9cwx-2883-4wfx](https://github.com/advisories/GHSA-9cwx-2883-4wfx) | vite | medium | 7.3.7, 8.3.4 | 已升级消除受影响版本 |
| [#14](https://github.com/phil616/gost-server/security/dependabot/14) / [GHSA-64vr-g452-qvp3](https://github.com/advisories/GHSA-64vr-g452-qvp3) | vite | medium | 7.3.7, 8.3.4 | 已升级消除受影响版本 |
| [#15](https://github.com/phil616/gost-server/security/dependabot/15) / [GHSA-vg6x-rcgg-rjx6](https://github.com/advisories/GHSA-vg6x-rcgg-rjx6) | vite | medium | 7.3.7, 8.3.4 | 已升级消除受影响版本 |
| [#16](https://github.com/phil616/gost-server/security/dependabot/16) / [GHSA-67mh-4wv8-2f99](https://github.com/advisories/GHSA-67mh-4wv8-2f99) | esbuild | medium | 0.18.20, 0.28.2 | 暂缓，见保留项 |
| [#17](https://github.com/phil616/gost-server/security/dependabot/17) / [GHSA-968p-4wvh-cqc8](https://github.com/advisories/GHSA-968p-4wvh-cqc8) | @babel/runtime | medium | 7.29.10, 8.0.7 | 已升级消除受影响版本 |
| [#18](https://github.com/phil616/gost-server/security/dependabot/18) / [GHSA-x574-m823-4x7w](https://github.com/advisories/GHSA-x574-m823-4x7w) | vite | medium | 7.3.7, 8.3.4 | 已升级消除受影响版本 |
| [#19](https://github.com/phil616/gost-server/security/dependabot/19) / [GHSA-jr5f-v2jv-69x6](https://github.com/advisories/GHSA-jr5f-v2jv-69x6) | axios | high | 0.34.0 | 已升级消除受影响版本 |
| [#20](https://github.com/phil616/gost-server/security/dependabot/20) / [GHSA-4r4m-qw57-chr8](https://github.com/advisories/GHSA-4r4m-qw57-chr8) | vite | medium | 7.3.7, 8.3.4 | 已升级消除受影响版本 |
| [#21](https://github.com/phil616/gost-server/security/dependabot/21) / [GHSA-xcj6-pq6g-qj4x](https://github.com/advisories/GHSA-xcj6-pq6g-qj4x) | vite | medium | 7.3.7, 8.3.4 | 已升级消除受影响版本 |
| [#22](https://github.com/phil616/gost-server/security/dependabot/22) / [GHSA-356w-63v5-8wf4](https://github.com/advisories/GHSA-356w-63v5-8wf4) | vite | medium | 7.3.7, 8.3.4 | 已升级消除受影响版本 |
| [#23](https://github.com/phil616/gost-server/security/dependabot/23) / [GHSA-859w-5945-r5v3](https://github.com/advisories/GHSA-859w-5945-r5v3) | vite | medium | 7.3.7, 8.3.4 | 已升级消除受影响版本 |
| [#24](https://github.com/phil616/gost-server/security/dependabot/24) / [GHSA-jqfw-vq24-v9c3](https://github.com/advisories/GHSA-jqfw-vq24-v9c3) | vite | low | 7.3.7, 8.3.4 | 已升级消除受影响版本 |
| [#25](https://github.com/phil616/gost-server/security/dependabot/25) / [GHSA-g4jq-h2w9-997c](https://github.com/advisories/GHSA-g4jq-h2w9-997c) | vite | low | 7.3.7, 8.3.4 | 已升级消除受影响版本 |
| [#26](https://github.com/phil616/gost-server/security/dependabot/26) / [GHSA-5j98-mcp5-4vw2](https://github.com/advisories/GHSA-5j98-mcp5-4vw2) | glob | high | 10.5.0, 7.2.3 | 已升级消除受影响版本 |
| [#27](https://github.com/phil616/gost-server/security/dependabot/27) / [GHSA-9jcx-v3wj-wh4m](https://github.com/advisories/GHSA-9jcx-v3wj-wh4m) | react-router | medium | 6.30.6 | 已升级消除受影响版本 |
| [#28](https://github.com/phil616/gost-server/security/dependabot/28) / [GHSA-848j-6mx2-7j84](https://github.com/advisories/GHSA-848j-6mx2-7j84) | elliptic | low | 6.6.1 | 暂缓，见保留项 |
| [#29](https://github.com/phil616/gost-server/security/dependabot/29) / [GHSA-43fc-jf86-j433](https://github.com/advisories/GHSA-43fc-jf86-j433) | axios | high | 0.34.0 | 已升级消除受影响版本 |
| [#30](https://github.com/phil616/gost-server/security/dependabot/30) / [GHSA-4w7w-66w2-5vf9](https://github.com/advisories/GHSA-4w7w-66w2-5vf9) | vite | medium | 7.3.7, 8.3.4 | 已升级消除受影响版本 |
| [#31](https://github.com/phil616/gost-server/security/dependabot/31) / [GHSA-fvcv-3m26-pcqx](https://github.com/advisories/GHSA-fvcv-3m26-pcqx) | axios | medium | 0.34.0 | 已升级消除受影响版本 |
| [#32](https://github.com/phil616/gost-server/security/dependabot/32) / [GHSA-3p68-rc4w-qgx5](https://github.com/advisories/GHSA-3p68-rc4w-qgx5) | axios | medium | 0.34.0 | 已升级消除受影响版本 |
| [#33](https://github.com/phil616/gost-server/security/dependabot/33) / [GHSA-xhjh-pmcv-23jw](https://github.com/advisories/GHSA-xhjh-pmcv-23jw) | axios | low | 0.34.0 | 已升级消除受影响版本 |
| [#34](https://github.com/phil616/gost-server/security/dependabot/34) / [GHSA-pmwg-cvhr-8vh7](https://github.com/advisories/GHSA-pmwg-cvhr-8vh7) | axios | high | 0.34.0 | 已升级消除受影响版本 |
| [#35](https://github.com/phil616/gost-server/security/dependabot/35) / [GHSA-w9j2-pvgh-6h63](https://github.com/advisories/GHSA-w9j2-pvgh-6h63) | axios | medium | 0.34.0 | 已升级消除受影响版本 |
| [#36](https://github.com/phil616/gost-server/security/dependabot/36) / [GHSA-xx6v-rp6x-q39c](https://github.com/advisories/GHSA-xx6v-rp6x-q39c) | axios | medium | 0.34.0 | 已升级消除受影响版本 |
| [#37](https://github.com/phil616/gost-server/security/dependabot/37) / [GHSA-6chq-wfr3-2hj9](https://github.com/advisories/GHSA-6chq-wfr3-2hj9) | axios | high | 0.34.0 | 已升级消除受影响版本 |
| [#38](https://github.com/phil616/gost-server/security/dependabot/38) / [GHSA-pf86-5x62-jrwf](https://github.com/advisories/GHSA-pf86-5x62-jrwf) | axios | high | 0.34.0 | 已升级消除受影响版本 |
| [#39](https://github.com/phil616/gost-server/security/dependabot/39) / [GHSA-vf2m-468p-8v99](https://github.com/advisories/GHSA-vf2m-468p-8v99) | axios | medium | 0.34.0 | 已升级消除受影响版本 |
| [#40](https://github.com/phil616/gost-server/security/dependabot/40) / [GHSA-5c9x-8gcm-mpgx](https://github.com/advisories/GHSA-5c9x-8gcm-mpgx) | axios | medium | 0.34.0 | 已升级消除受影响版本 |
| [#41](https://github.com/phil616/gost-server/security/dependabot/41) / [GHSA-62hf-57xw-28j9](https://github.com/advisories/GHSA-62hf-57xw-28j9) | axios | medium | 0.34.0 | 已升级消除受影响版本 |
| [#42](https://github.com/phil616/gost-server/security/dependabot/42) / [GHSA-m7pr-hjqh-92cm](https://github.com/advisories/GHSA-m7pr-hjqh-92cm) | axios | medium | 0.34.0 | 已升级消除受影响版本 |
| [#43](https://github.com/phil616/gost-server/security/dependabot/43) / [GHSA-898c-q2cr-xwhg](https://github.com/advisories/GHSA-898c-q2cr-xwhg) | axios | medium | 0.34.0 | 已升级消除受影响版本 |
| [#44](https://github.com/phil616/gost-server/security/dependabot/44) / [GHSA-pjwm-pj3p-43mv](https://github.com/advisories/GHSA-pjwm-pj3p-43mv) | axios | high | 0.34.0 | 已升级消除受影响版本 |
| [#45](https://github.com/phil616/gost-server/security/dependabot/45) / [GHSA-3g43-6gmg-66jw](https://github.com/advisories/GHSA-3g43-6gmg-66jw) | axios | high | 0.34.0 | 已升级消除受影响版本 |
| [#46](https://github.com/phil616/gost-server/security/dependabot/46) / [GHSA-c27g-q93r-2cwf](https://github.com/advisories/GHSA-c27g-q93r-2cwf) | vite | high | 7.3.7, 8.3.4 | 已升级消除受影响版本 |
| [#47](https://github.com/phil616/gost-server/security/dependabot/47) / [GHSA-j5f8-grm9-p9fc](https://github.com/advisories/GHSA-j5f8-grm9-p9fc) | axios | high | 0.34.0 | 已升级消除受影响版本 |
| [#48](https://github.com/phil616/gost-server/security/dependabot/48) / [GHSA-p92q-9vqr-4j8v](https://github.com/advisories/GHSA-p92q-9vqr-4j8v) | axios | high | 0.34.0 | 已升级消除受影响版本 |
| [#49](https://github.com/phil616/gost-server/security/dependabot/49) / [GHSA-hfxv-24rg-xrqf](https://github.com/advisories/GHSA-hfxv-24rg-xrqf) | axios | high | 0.34.0 | 已升级消除受影响版本 |
| [#50](https://github.com/phil616/gost-server/security/dependabot/50) / [GHSA-4x5r-pxfx-6jf8](https://github.com/advisories/GHSA-4x5r-pxfx-6jf8) | @babel/core | low | 7.29.6, 7.29.7 | 已升级消除受影响版本 |
| [#51](https://github.com/phil616/gost-server/security/dependabot/51) / [GHSA-fx2h-pf6j-xcff](https://github.com/advisories/GHSA-fx2h-pf6j-xcff) | vite | medium | 7.3.7, 8.3.4 | 已升级消除受影响版本 |
| [#52](https://github.com/phil616/gost-server/security/dependabot/52) / [GHSA-v6wh-96g9-6wx3](https://github.com/advisories/GHSA-v6wh-96g9-6wx3) | vite | medium | 7.3.7, 8.3.4 | 已升级消除受影响版本 |
| [#53](https://github.com/phil616/gost-server/security/dependabot/53) / [GHSA-mmx7-hfxf-jppx](https://github.com/advisories/GHSA-mmx7-hfxf-jppx) | axios | medium | 0.34.0 | 已升级消除受影响版本 |
| [#54](https://github.com/phil616/gost-server/security/dependabot/54) / [GHSA-7q8q-rj6j-mhjq](https://github.com/advisories/GHSA-7q8q-rj6j-mhjq) | axios | medium | 0.34.0 | 已升级消除受影响版本 |
| [#55](https://github.com/phil616/gost-server/security/dependabot/55) / [GHSA-wrjc-x8rr-h8h6](https://github.com/advisories/GHSA-wrjc-x8rr-h8h6) | react-router | medium | 6.30.6 | 暂缓，见保留项 |
| [#56](https://github.com/phil616/gost-server/security/dependabot/56) / [GHSA-vcc3-ghjq-m6fr](https://github.com/advisories/GHSA-vcc3-ghjq-m6fr) | decode-uri-component | medium | 0.2.2 | 暂缓，见保留项 |
| [#57](https://github.com/phil616/gost-server/security/dependabot/57) / [GHSA-82fw-gwwq-j7x9](https://github.com/advisories/GHSA-82fw-gwwq-j7x9) | @vitest/mocker | medium | 4.1.12 | 已升级消除受影响版本 |
| [#58](https://github.com/phil616/gost-server/security/dependabot/58) / [GHSA-82fw-gwwq-j7x9](https://github.com/advisories/GHSA-82fw-gwwq-j7x9) | vitest | medium | 4.1.12 | 已升级消除受影响版本 |
| [#59](https://github.com/phil616/gost-server/security/dependabot/59) / [GHSA-9fr6-4gfg-395g](https://github.com/advisories/GHSA-9fr6-4gfg-395g) | axios | medium | 0.34.0 | 已升级消除受影响版本 |
| [#60](https://github.com/phil616/gost-server/security/dependabot/60) / [GHSA-vfj7-8cjw-p6xm](https://github.com/advisories/GHSA-vfj7-8cjw-p6xm) | braces | high | 3.0.3 | 上游已撤回 |
| [#62](https://github.com/phil616/gost-server/security/dependabot/62) / [GHSA-hp3w-g68c-fv3c](https://github.com/advisories/GHSA-hp3w-g68c-fv3c) | sprintf-js | medium | 1.0.3 | 上游已撤回 |
| [#63](https://github.com/phil616/gost-server/security/dependabot/63) / [GHSA-698v-4r3x-3vcv](https://github.com/advisories/GHSA-698v-4r3x-3vcv) | vite | medium | 7.3.7, 8.3.4 | 已升级消除受影响版本 |
| [#64](https://github.com/phil616/gost-server/security/dependabot/64) / [GHSA-587h-7wmj-h5pm](https://github.com/advisories/GHSA-587h-7wmj-h5pm) | vite | high | 7.3.7, 8.3.4 | 已升级消除受影响版本 |
| [#65](https://github.com/phil616/gost-server/security/dependabot/65) / [GHSA-r825-w8mm-5pxw](https://github.com/advisories/GHSA-r825-w8mm-5pxw) | vite | medium | 7.3.7, 8.3.4 | 已升级消除受影响版本 |
| [#66](https://github.com/phil616/gost-server/security/dependabot/66) / [GHSA-rq7h-c2jc-7f22](https://github.com/advisories/GHSA-rq7h-c2jc-7f22) | vite | medium | 7.3.7, 8.3.4 | 已升级消除受影响版本 |
| [#67](https://github.com/phil616/gost-server/security/dependabot/67) / [GHSA-9jrq-w75r-8gcw](https://github.com/advisories/GHSA-9jrq-w75r-8gcw) | vite | low | 7.3.7, 8.3.4 | 已升级消除受影响版本 |
| [#68](https://github.com/phil616/gost-server/security/dependabot/68) / [GHSA-qxg6-89mg-rqv8](https://github.com/advisories/GHSA-qxg6-89mg-rqv8) | @vitest/mocker | medium | 4.1.12 | 已升级消除受影响版本 |
