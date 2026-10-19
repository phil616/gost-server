import { Alert, Collapse, Space, Tabs, Typography } from "antd";
import { windowsRegistrationCommand, windowsStatusCommand } from "../registration";

function Command({ label, text }: { label: string; text: string }) {
  return (
    <div>
      <Typography.Text strong>{label}</Typography.Text>
      <Typography.Paragraph copyable={{ text, tooltips: ["复制命令", "已复制"] }}>
        <pre className="code-block">{text}</pre>
      </Typography.Paragraph>
    </div>
  );
}

export default function RegistrationGuide({ command }: { command?: string }) {
  const windowsCommand = command ? windowsRegistrationCommand(command) : undefined;
  return (
    <Tabs
      defaultActiveKey="windows"
      items={[
        {
          key: "windows",
          label: "Windows · 已安装 MSI",
          children: (
            <Space orientation="vertical" size="middle" style={{ width: "100%" }}>
              <Typography.Paragraph>
                在要注册的 Windows 设备上，以管理员身份打开 PowerShell。
                MSI 已创建自动启动的 AltasCITunnel 服务，安装完成后仍需执行注册。
                以下命令使用默认安装目录，无需刷新 PATH 或切换当前目录。
                自定义安装目录时，请修改 $agent 指向实际的 altascitunnel.exe。
              </Typography.Paragraph>
              {windowsCommand ? (
                <Command label="1. 复制并执行：检查服务、注册设备" text={windowsCommand} />
              ) : command ? (
                <Alert
                  type="error"
                  showIcon
                  title="无法生成 PowerShell 命令"
                  description="请确认平台配置的服务器地址是 wss://域名[:端口]，不含路径、查询参数或账号信息，再重新生成注册申请。"
                />
              ) : (
                <Typography.Paragraph type="secondary">
                  先填写上方设备名称并生成注册命令，此处会自动填入服务器地址和一次性 token，
                  同时检查并启动本机服务；无需手工拼接参数。
                </Typography.Paragraph>
              )}
              <Command label="2. 单独检查：服务状态与注册结果（可重复执行）" text={windowsStatusCommand} />
              <Alert
                type="info"
                showIcon
                title="如何确认注册完成"
                description={
                  <>
                    服务应为 Running。Enrollment accepted 仅表示本机服务接受了请求，
                    不代表已连接到平台。等待几秒后重复执行检查命令，确认 JSON 中
                    client_id 非空、state 为 online，再回到设备列表刷新，确认同一设备在线。
                    注册完成后无需重复执行注册命令。
                  </>
                }
              />
              <Collapse
                items={[
                  {
                    key: "errors",
                    label: "服务未启动、注册未上线时如何检查？",
                    children: (
                      <>
                        <Typography.Paragraph>
                          找不到服务或程序：检查 MSI 是否安装在当前设备及实际安装目录。
                          权限不足：重新以管理员身份打开 PowerShell。
                          local service unavailable：确认服务为 Running，再执行 status；
                          检查命名管道权限，不要另外启动 run 进程。
                        </Typography.Paragraph>
                        <Command label="单独启动已有服务" text="Start-Service -Name AltasCITunnel -ErrorAction Stop" />
                        <Typography.Paragraph>
                          已提交但未上线：重复执行 status，查看 error、state 和 retry。
                          token 过期、已使用或被撤销时，关闭弹窗后重新生成申请；
                          device already registered 表示已有身份，无需再次注册。
                        </Typography.Paragraph>
                        <Typography.Paragraph>
                          连接失败时检查设备网络、WSS 域名和端口、服务端反代与证书。
                          --server 是 wss://域名[:端口]，不能附加 /agent/v1/ws。
                          私有 CA 应配置在运行中的 Windows 服务所用的信任配置中；
                          单独给 register 增加 --ca-file 不会修改服务的证书信任。
                        </Typography.Paragraph>
                        <Typography.Paragraph>
                          MSI 服务运行期间不要再启动前台 run 实例，以免争用本机命名管道。
                          MSI 修复、升级保留身份，完整卸载会清除身份。
                        </Typography.Paragraph>
                        <Typography.Link href="https://github.com/phil616/gost-server/blob/main/docs/configuration.md" target="_blank" rel="noopener noreferrer">
                          查看服务与 CA 配置参考
                        </Typography.Link>
                      </>
                    ),
                  },
                ]}
              />
            </Space>
          ),
        },
        {
          key: "linux",
          label: "Linux · 已安装服务",
          children: (
            <>
              <Typography.Paragraph>
                先安装并启动 Linux systemd 服务，再在设备终端中执行。
                自定义数据目录或前台 run 模式请按使用指南操作，不能混用 Windows 命令。
              </Typography.Paragraph>
              {command && <Command label="注册到平台" text={`sudo ${command}`} />}
              <Command label="检查本机注册状态" text="sudo altascitunnel status" />
              <Typography.Paragraph>
                确认 client_id 非空、state 为 online，并在设备列表核实在线。
              </Typography.Paragraph>
              <Typography.Link href="https://github.com/phil616/gost-server/blob/main/docs/usage.md" target="_blank" rel="noopener noreferrer">
                查看 Linux 安装与前台调试指南
              </Typography.Link>
            </>
          ),
        },
      ]}
    />
  );
}
