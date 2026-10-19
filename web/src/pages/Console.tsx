import { useEffect, useRef, useState } from "react";
import {
  Button,
  Card,
  Col,
  ConfigProvider,
  DatePicker,
  Descriptions,
  Drawer,
  Empty,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Row,
  Select,
  Space,
  Statistic,
  Switch,
  Table,
  Tag,
  Typography,
  message,
} from "antd";
import {
  CloudServerOutlined,
  DeploymentUnitOutlined,
  SafetyCertificateOutlined,
  AuditOutlined,
  ReloadOutlined,
  PlusOutlined,
  LogoutOutlined,
} from "@ant-design/icons";
import { PageContainer, ProLayout, ProTable } from "@ant-design/pro-components";
import type { ActionType, ProColumns } from "@ant-design/pro-components";
import {
  api,
  me,
  mutation,
  type Client,
  type Tunnel,
  type Credential,
} from "../api";
import "./console.css";

type List<T> = { items: T[]; total: number };
type Operation = { operation_id: string; resource_id: string; state: string };
type Secret = {
  username: string;
  secret: string;
  command: string;
  yaml: string;
};
type EdgeSession = {
  id: string;
  tunnel_id: string;
  kind: string;
  connected: boolean;
  opened_at: string;
  input_bytes: number;
  output_bytes: number;
};
message.config({ maxCount: 2 });
function bytes(value: number) {
  if (value < 1024) return `${value} B`;
  const units = ["KiB", "MiB", "GiB", "TiB"];
  const index = Math.min(
    Math.floor(Math.log2(value) / 10) - 1,
    units.length - 1,
  );
  return `${(value / 1024 ** (index + 1)).toFixed(1)} ${units[index]}`;
}
const colors: Record<string, string> = {
  online: "green",
  connected: "green",
  applied: "green",
  disabled: "default",
  offline: "default",
  pending: "blue",
  connecting: "blue",
  backoff: "orange",
  stale: "orange",
  revoked: "red",
  error: "red",
  unknown: "orange",
};
const names: Record<string, string> = {
  online: "在线",
  connected: "已连接",
  applied: "已生效",
  disabled: "已停用",
  offline: "离线",
  pending: "待同步",
  connecting: "连接中",
  backoff: "等待重试",
  stale: "心跳超时",
  revoked: "已撤销",
  error: "错误",
  unknown: "状态未知",
  expired: "已到期",
};
const status = (value: string) => (
  <Tag color={colors[value] || "default"}>{names[value] || value}</Tag>
);
function download(text: string) {
  const url = URL.createObjectURL(
    new Blob([text], { type: "application/yaml" }),
  );
  const a = document.createElement("a");
  a.href = url;
  a.download = "gost-visitor.yaml";
  a.click();
  URL.revokeObjectURL(url);
}

export default function Console() {
  const [section, setSection] = useState("devices");
  const [username, setUsername] = useState("");
  const [ready, setReady] = useState(false);
  const [edge, setEdge] = useState<{
    healthy: boolean;
    error: string;
    runtime: {
      revision: number;
      updated_at: string;
      sessions: EdgeSession[];
    };
  }>();
  const [deviceOptions, setDeviceOptions] = useState<Client[]>([]);
  const [registration, setRegistration] = useState<{
    command: string;
    expires_at: string;
  }>();
  const [registerOpen, setRegisterOpen] = useState(false);
  const [tunnelOpen, setTunnelOpen] = useState(false);
  const [editing, setEditing] = useState<Tunnel>();
  const [accessTunnel, setAccessTunnel] = useState<Tunnel>();
  const [credentials, setCredentials] = useState<Credential[]>([]);
  const [secret, setSecret] = useState<Secret>();
  const [issuing, setIssuing] = useState(false);
  const [expiresAt, setExpiresAt] = useState<string>();
  const [deviceDetail, setDeviceDetail] = useState<Client>();
  const [busy, setBusy] = useState(false);
  const [form] = Form.useForm();
  const action = useRef<ActionType>(null);
  useEffect(() => {
    me()
      .then((s) => {
        setUsername(s.username);
        setReady(true);
      })
      .catch(() => {});
  }, []);
  useEffect(() => {
    if (!ready) return;
    const refresh = () => {
      api<typeof edge>("/edge/status")
        .then(setEdge)
        .catch(() => {});
    };
    refresh();
    const timer = window.setInterval(refresh, 5000);
    return () => clearInterval(timer);
  }, [ready]);
  async function operation(fn: () => Promise<Operation>) {
    try {
      const op = await fn();
      message.info({
        key: op.operation_id,
        content: "操作已提交，正在同步边缘策略",
        duration: 2,
      });
      action.current?.reload();
      const deadline = Date.now() + 15000;
      const poll = async () => {
        const result = await api<{ state: string }>(
          `/operations/${op.operation_id}`,
        );
        if (result.state === "applied") {
          message.success({
            key: op.operation_id,
            content: "边缘策略已生效",
            duration: 2,
          });
          action.current?.reload();
        } else if (Date.now() < deadline)
          window.setTimeout(() => {
            poll().catch(() => {});
          }, 1000);
      };
      poll().catch(() => {});
      return true;
    } catch (e) {
      message.error((e as Error).message);
      return false;
    }
  }
  async function loadCredentials(t: Tunnel) {
    setAccessTunnel(t);
    try {
      const result = await api<List<Credential>>(
        `/tunnels/${t.id}/access-credentials?page_size=100`,
      );
      setCredentials(result.items);
    } catch (e) {
      message.error((e as Error).message);
    }
  }
  async function newTunnel(t?: Tunnel) {
    setEditing(t);
    form.resetFields();
    if (t) form.setFieldsValue(t);
    else
      form.setFieldsValue({
        protocol: "tcp",
        target_ip: "127.0.0.1",
        target_port: 22,
        enabled: true,
      });
    try {
      const devices = await api<List<Client>>("/clients?page_size=100");
      setDeviceOptions(devices.items.filter((c) => !c.revoked));
      setTunnelOpen(true);
    } catch (e) {
      message.error((e as Error).message);
    }
  }
  const deviceColumns: ProColumns<Client>[] = [
    {
      title: "设备名称",
      dataIndex: "name",
      render: (_, c) => (
        <Space orientation="vertical" size={0}>
          <Typography.Text strong>{c.name || "未命名设备"}</Typography.Text>
          <Typography.Text type="secondary" copyable>
            {c.id}
          </Typography.Text>
        </Space>
      ),
    },
    { title: "系统", render: (_, c) => `${c.os} / ${c.arch}` },
    {
      title: "控制连接",
      dataIndex: "state",
      render: (_, c) => status(c.state),
    },
    {
      title: "配置版本",
      render: (_, c) => (
        <Space>
          {c.applied_revision} / {c.revision}
          {c.applied_revision === c.revision ? (
            <Tag color="green">已同步</Tag>
          ) : (
            <Tag color="blue">待同步</Tag>
          )}
        </Space>
      ),
    },
    { title: "最后心跳", dataIndex: "last_seen", valueType: "dateTime" },
    {
      title: "操作",
      valueType: "option",
      render: (_, c) => (
        <Space>
          <Button
            type="link"
            onClick={async () => {
              try {
                setDeviceDetail(await api<Client>(`/clients/${c.id}`));
              } catch (e) {
                message.error((e as Error).message);
              }
            }}
          >
            详情
          </Button>
          <Popconfirm
            title="撤销此设备并断开所属隧道？"
            onConfirm={() =>
              operation(() => mutation(`/clients/${c.id}/revoke`))
            }
          >
            <Button type="link" danger disabled={c.revoked}>
              撤销设备
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];
  const tunnelColumns: ProColumns<Tunnel>[] = [
    {
      title: "隧道",
      render: (_, t) => (
        <Space orientation="vertical" size={0}>
          <Typography.Text strong>
            {t.name || t.protocol.toUpperCase()}
          </Typography.Text>
          <Typography.Text copyable type="secondary">
            {t.id}
          </Typography.Text>
        </Space>
      ),
    },
    {
      title: "目标服务",
      render: (_, t) => (
        <Space>
          <Tag>{t.protocol.toUpperCase()}</Tag>
          <Typography.Text code>
            {t.target_ip}:{t.target_port}
          </Typography.Text>
        </Space>
      ),
    },
    {
      title: "状态",
      render: (_, t) => (
        <Space orientation="vertical" size={0}>
          {status(t.state)}
          {t.last_error && (
            <Typography.Text type="danger" ellipsis style={{ maxWidth: 220 }}>
              {t.last_error}
            </Typography.Text>
          )}
          {t.state === "backoff" && t.next_retry_at && (
            <Typography.Text type="secondary">
              重试：{new Date(t.next_retry_at).toLocaleTimeString()}
            </Typography.Text>
          )}
        </Space>
      ),
    },
    { title: "活动访问", dataIndex: "active_connections" },
    {
      title: "启用",
      render: (_, t) => (
        <Switch
          checked={t.enabled}
          onChange={(enabled) =>
            operation(() =>
              mutation(`/tunnels/${t.id}`, { enabled }, "PATCH", t.version),
            )
          }
        />
      ),
    },
    {
      title: "操作",
      valueType: "option",
      render: (_, t) => (
        <Space>
          <Button type="link" onClick={() => newTunnel(t)}>
            编辑
          </Button>
          <Button type="link" onClick={() => loadCredentials(t)}>
            访问凭据
          </Button>
          <Popconfirm
            title="删除隧道并断开已有连接？"
            onConfirm={() =>
              operation(() => mutation(`/tunnels/${t.id}`, {}, "DELETE"))
            }
          >
            <Button type="link" danger>
              删除
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];
  if (!ready) return <div className="loading-shell">正在连接控制台…</div>;
  const titles: Record<string, string> = {
    devices: "设备管理",
    tunnels: "隧道管理",
    audit: "操作审计",
    edge: "边缘状态",
  };
  return (
    <ConfigProvider
      theme={{
        token: {
          colorPrimary: "#167a72",
          borderRadius: 8,
          fontFamily: "Inter, system-ui, sans-serif",
        },
      }}
    >
      <ProLayout
        title="AltasCI Tunnel"
        logo={
          <DeploymentUnitOutlined style={{ color: "#167a72", fontSize: 28 }} />
        }
        layout="side"
        navTheme="light"
        location={{ pathname: `/${section}` }}
        route={{
          routes: [
            {
              path: "/devices",
              name: "设备管理",
              icon: <CloudServerOutlined />,
            },
            {
              path: "/tunnels",
              name: "隧道管理",
              icon: <DeploymentUnitOutlined />,
            },
            { path: "/audit", name: "操作审计", icon: <AuditOutlined /> },
            {
              path: "/edge",
              name: "边缘状态",
              icon: <SafetyCertificateOutlined />,
            },
          ],
        }}
        menuItemRender={(item, dom) => (
          <a
            onClick={() => {
              setSection((item.path || "devices").replace(/^\//, ""));
              action.current?.reload();
            }}
          >
            {dom}
          </a>
        )}
        avatarProps={{ title: username }}
        actionsRender={() => [
          <Button
            key="logout"
            type="text"
            icon={<LogoutOutlined />}
            onClick={async () => {
              await api("/auth/logout", { method: "POST" });
              window.location.assign("/login");
            }}
          >
            退出
          </Button>,
        ]}
      >
        <PageContainer
          title={titles[section]}
          subTitle="单一边缘 · 安全连接 · 集中管理"
        >
          <Row gutter={16} style={{ marginBottom: 24 }}>
            <Col span={8}>
              <Card>
                <Statistic
                  title="边缘引擎"
                  value={edge?.healthy ? "运行正常" : "等待连接"}
                  styles={{
                    content: { color: edge?.healthy ? "#167a72" : "#c47d23" },
                  }}
                />
              </Card>
            </Col>
            <Col span={8}>
              <Card>
                <Statistic
                  title="在线反向连接"
                  value={
                    edge?.runtime?.sessions?.filter(
                      (s) => s.kind === "provider" && s.connected,
                    ).length || 0
                  }
                />
              </Card>
            </Col>
            <Col span={8}>
              <Card>
                <Statistic
                  title="已生效策略版本"
                  value={edge?.runtime?.revision || 0}
                  prefix="r"
                />
              </Card>
            </Col>
          </Row>
          {(section === "devices" || section === "tunnels") && (
            <ProTable<Client | Tunnel>
              key={section}
              actionRef={action}
              rowKey="id"
              search={false}
              polling={5000}
              pagination={{ pageSize: 20 }}
              columns={
                (section === "devices"
                  ? deviceColumns
                  : tunnelColumns) as ProColumns<Client | Tunnel>[]
              }
              request={async (params) => {
                const res = await api<List<Client | Tunnel>>(
                  `/${section === "devices" ? "clients" : "tunnels"}?page=${params.current || 1}&page_size=${params.pageSize || 20}`,
                );
                return { data: res.items, total: res.total, success: true };
              }}
              toolbar={{
                title: section === "devices" ? "受管设备" : "服务隧道",
                actions: [
                  <Button
                    key="refresh"
                    icon={<ReloadOutlined />}
                    onClick={() => action.current?.reload()}
                  >
                    刷新
                  </Button>,
                  <Button
                    key="add"
                    type="primary"
                    icon={<PlusOutlined />}
                    onClick={() =>
                      section === "devices"
                        ? setRegisterOpen(true)
                        : newTunnel()
                    }
                  >
                    {section === "devices" ? "注册设备" : "新增隧道"}
                  </Button>,
                ],
              }}
            />
          )}
          {section === "audit" && (
            <ProTable
              rowKey="id"
              search={false}
              polling={5000}
              columns={[
                { title: "时间", dataIndex: "at", valueType: "dateTime" },
                { title: "操作", dataIndex: "action" },
                { title: "操作主体", dataIndex: "actor" },
                { title: "资源", dataIndex: "resource", copyable: true },
              ]}
              request={async (p) => {
                const r = await api<List<Record<string, unknown>>>(
                  `/audit-events?page=${p.current || 1}&page_size=${p.pageSize || 20}`,
                );
                return { data: r.items, total: r.total, success: true };
              }}
            />
          )}
          {section === "edge" && (
            <Card title="引擎运行状态">
              <Space orientation="vertical" style={{ width: "100%" }}>
                {status(edge?.healthy ? "online" : "unknown")}
                {edge?.error && (
                  <Typography.Text type="danger">{edge.error}</Typography.Text>
                )}
                <Typography.Paragraph type="secondary">
                  控制平台短暂不可用时，引擎继续使用最后确认的授权策略。策略撤销以边缘确认结果为准。
                </Typography.Paragraph>
                <Descriptions
                  items={[
                    {
                      key: "revision",
                      label: "已确认策略",
                      children: `r${edge?.runtime?.revision || 0}`,
                    },
                    {
                      key: "updated",
                      label: "最近同步",
                      children: edge?.runtime?.updated_at
                        ? new Date(edge.runtime.updated_at).toLocaleString()
                        : "等待同步",
                    },
                  ]}
                />
                <Table<EdgeSession>
                  rowKey="id"
                  dataSource={edge?.runtime?.sessions || []}
                  pagination={{ pageSize: 10 }}
                  columns={[
                    {
                      title: "TunnelID",
                      dataIndex: "tunnel_id",
                      ellipsis: true,
                    },
                    {
                      title: "连接角色",
                      render: (_, s) =>
                        s.kind === "provider" ? "反向连接" : "访问方",
                    },
                    {
                      title: "状态",
                      render: (_, s) =>
                        status(s.connected ? "connected" : "connecting"),
                    },
                    {
                      title: "建立时间",
                      render: (_, s) => new Date(s.opened_at).toLocaleString(),
                    },
                    { title: "接收", render: (_, s) => bytes(s.input_bytes) },
                    { title: "发送", render: (_, s) => bytes(s.output_bytes) },
                  ]}
                />
                <Typography.Text type="secondary">
                  连接级流量包含协议开销。
                </Typography.Text>
              </Space>
            </Card>
          )}
          <Modal
            title="注册新设备"
            open={registerOpen}
            onCancel={() => {
              setRegisterOpen(false);
              setRegistration(undefined);
            }}
            footer={null}
            destroyOnHidden
          >
            {registration ? (
              <Space orientation="vertical" style={{ width: "100%" }}>
                <Typography.Paragraph>
                  安装客户端并启动系统服务后，在设备上执行：
                </Typography.Paragraph>
                <Typography.Paragraph copyable={{ text: registration.command }}>
                  <pre className="code-block">{registration.command}</pre>
                </Typography.Paragraph>
                <Typography.Text type="secondary">
                  一次性凭据，有效期至{" "}
                  {new Date(registration.expires_at).toLocaleString()}
                  。注册成功后设备会自动出现。
                </Typography.Text>
              </Space>
            ) : (
              <Form
                layout="vertical"
                onFinish={async (v) => {
                  setBusy(true);
                  try {
                    setRegistration(await mutation("/enrollments", v));
                  } catch (e) {
                    message.error((e as Error).message);
                  } finally {
                    setBusy(false);
                  }
                }}
              >
                <Form.Item
                  name="name"
                  label="设备名称"
                  rules={[{ required: true }]}
                >
                  <Input placeholder="例如：研发服务器" maxLength={128} />
                </Form.Item>
                <Button type="primary" htmlType="submit" loading={busy}>
                  生成注册命令
                </Button>
              </Form>
            )}
          </Modal>
          <Modal
            title={editing ? "编辑隧道" : "新增隧道"}
            open={tunnelOpen}
            onCancel={() => setTunnelOpen(false)}
            onOk={() => form.submit()}
            confirmLoading={busy}
            destroyOnHidden
          >
            <Form
              form={form}
              layout="vertical"
              onFinish={async (values) => {
                setBusy(true);
                try {
                  const {
                    client_id,
                    name,
                    protocol,
                    target_ip,
                    target_port,
                    enabled,
                  } = values;
                  const body = {
                    name,
                    protocol,
                    target_ip,
                    target_port,
                    enabled,
                  };
                  if (
                    await operation(() =>
                      editing
                        ? mutation(
                            `/tunnels/${editing.id}`,
                            body,
                            "PATCH",
                            editing.version,
                          )
                        : mutation(`/clients/${client_id}/tunnels`, body),
                    )
                  )
                    setTunnelOpen(false);
                } finally {
                  setBusy(false);
                }
              }}
            >
              <Form.Item name="name" label="名称">
                <Input maxLength={128} />
              </Form.Item>
              {!editing && (
                <Form.Item
                  name="client_id"
                  label="受管设备"
                  rules={[{ required: true }]}
                >
                  <Select
                    showSearch
                    optionFilterProp="label"
                    options={deviceOptions.map((c) => ({
                      label: `${c.name || c.id} · ${c.state}`,
                      value: c.id,
                    }))}
                  />
                </Form.Item>
              )}
              <Form.Item
                name="protocol"
                label="协议"
                rules={[{ required: true }]}
              >
                <Select
                  options={[
                    { label: "TCP", value: "tcp" },
                    { label: "UDP", value: "udp" },
                  ]}
                />
              </Form.Item>
              <Form.Item
                name="target_ip"
                label="目标 IPv4"
                rules={[{ required: true }]}
              >
                <Input placeholder="127.0.0.1" />
              </Form.Item>
              <Form.Item
                name="target_port"
                label="目标端口"
                rules={[{ required: true }]}
              >
                <InputNumber min={1} max={65535} style={{ width: "100%" }} />
              </Form.Item>
              <Form.Item name="enabled" label="启用" valuePropName="checked">
                <Switch />
              </Form.Item>
              <Typography.Text type="secondary">
                目标地址从客户端访问。修改目标或协议会断开此隧道已有业务连接。
              </Typography.Text>
            </Form>
          </Modal>
          <Drawer
            title={`访问凭据 · ${accessTunnel?.name || accessTunnel?.id || ""}`}
            open={!!accessTunnel}
            onClose={() => {
              setAccessTunnel(undefined);
              setCredentials([]);
            }}
            size="large"
            extra={
              <Button
                type="primary"
                onClick={() => {
                  setExpiresAt(undefined);
                  setIssuing(true);
                }}
              >
                发放凭据
              </Button>
            }
          >
            <Typography.Paragraph type="secondary">
              每个凭据仅能访问当前隧道，可独立撤销并断开已有会话。
            </Typography.Paragraph>
            <Table
              rowKey="id"
              dataSource={credentials}
              pagination={false}
              locale={{ emptyText: <Empty description="尚未发放访问凭据" /> }}
              columns={[
                { title: "凭据", dataIndex: "username", ellipsis: true },
                {
                  title: "状态",
                  render: (_, c) =>
                    status(
                      c.revoked
                        ? "revoked"
                        : c.expires_at && Date.parse(c.expires_at) <= Date.now()
                          ? "expired"
                          : "online",
                    ),
                },
                {
                  title: "到期时间",
                  render: (_, c) =>
                    c.expires_at
                      ? new Date(c.expires_at).toLocaleString()
                      : "长期有效",
                },
                {
                  title: "操作",
                  render: (_, c) => (
                    <Space>
                      <Button
                        type="link"
                        disabled={c.revoked}
                        onClick={async () => {
                          try {
                            setSecret(
                              await mutation(
                                `/access-credentials/${c.id}/reveal`,
                              ),
                            );
                          } catch (e) {
                            message.error((e as Error).message);
                          }
                        }}
                      >
                        连接命令
                      </Button>
                      <Popconfirm
                        title="撤销凭据并断开当前会话？"
                        onConfirm={async () => {
                          await operation(() =>
                            mutation(
                              `/access-credentials/${c.id}`,
                              {},
                              "DELETE",
                            ),
                          );
                          if (accessTunnel) await loadCredentials(accessTunnel);
                        }}
                      >
                        <Button type="link" danger disabled={c.revoked}>
                          撤销
                        </Button>
                      </Popconfirm>
                    </Space>
                  ),
                },
              ]}
            />
          </Drawer>
          <Drawer
            title={deviceDetail?.name || "设备详情"}
            open={!!deviceDetail}
            onClose={() => setDeviceDetail(undefined)}
            size="large"
          >
            <Descriptions
              column={1}
              items={[
                {
                  key: "id",
                  label: "ClientID",
                  children: (
                    <Typography.Text copyable>
                      {deviceDetail?.id}
                    </Typography.Text>
                  ),
                },
                {
                  key: "os",
                  label: "系统",
                  children: `${deviceDetail?.os} / ${deviceDetail?.arch}`,
                },
                {
                  key: "status",
                  label: "管理连接",
                  children: status(deviceDetail?.state || "unknown"),
                },
                {
                  key: "seen",
                  label: "最后心跳",
                  children: deviceDetail?.last_seen
                    ? new Date(deviceDetail.last_seen).toLocaleString()
                    : "暂无",
                },
                {
                  key: "revision",
                  label: "配置版本（已应用 / 已接收 / 期望）",
                  children: `${deviceDetail?.applied_revision} / ${deviceDetail?.received_revision} / ${deviceDetail?.revision}`,
                },
              ]}
            />
            <Typography.Title level={5}>最后上报的隧道状态</Typography.Title>
            <Table
              rowKey="tunnel_id"
              dataSource={deviceDetail?.runtime || []}
              size="small"
              pagination={false}
              columns={[
                { title: "TunnelID", dataIndex: "tunnel_id", ellipsis: true },
                { title: "状态", render: (_, t) => status(t.state) },
                { title: "重试次数", dataIndex: "retry_count" },
                { title: "错误", dataIndex: "error", ellipsis: true },
              ]}
            />
          </Drawer>
          <Modal
            title="发放访问凭据"
            open={issuing}
            onCancel={() => setIssuing(false)}
            confirmLoading={busy}
            onOk={async () => {
              if (!accessTunnel) return;
              setBusy(true);
              try {
                if (
                  await operation(() =>
                    mutation(
                      `/tunnels/${accessTunnel.id}/access-credentials`,
                      expiresAt ? { expires_at: expiresAt } : {},
                    ),
                  )
                ) {
                  setIssuing(false);
                  await loadCredentials(accessTunnel);
                }
              } finally {
                setBusy(false);
              }
            }}
          >
            <Typography.Paragraph>
              可设置到期时间；留空表示长期有效。到期后边缘会主动断开会话。
            </Typography.Paragraph>
            <DatePicker
              showTime
              onChange={(value) => setExpiresAt(value?.toISOString())}
              style={{ width: "100%" }}
            />
          </Modal>
          <Modal
            title="原生 gost 连接配置"
            open={!!secret}
            onCancel={() => setSecret(undefined)}
            footer={
              <Button onClick={() => secret && download(secret.yaml)}>
                下载 YAML
              </Button>
            }
            destroyOnHidden
            width={760}
          >
            <Typography.Paragraph type="secondary">
              此命令包含访问秘密。每次展示已记录审计。
            </Typography.Paragraph>
            <Typography.Paragraph copyable={{ text: secret?.command }}>
              <pre className="code-block">{secret?.command}</pre>
            </Typography.Paragraph>
          </Modal>
        </PageContainer>
      </ProLayout>
    </ConfigProvider>
  );
}
