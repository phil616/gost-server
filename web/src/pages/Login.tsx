import { useState } from "react";
import { Button, Form, Input, Typography, Alert } from "antd";
import { login } from "../api";
import "./console.css";
export default function Login() {
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  return (
    <main className="login-shell">
      <section className="login-card">
        <span className="eyebrow">PRIVATE NETWORK INFRASTRUCTURE</span>
        <Typography.Title level={2}>AltasCI Tunnel</Typography.Title>
        <Typography.Paragraph type="secondary">
          连接内部服务，统一管理每一条隧道。
        </Typography.Paragraph>
        {error && (
          <Alert type="error" title={error} style={{ marginBottom: 20 }} />
        )}
        <Form
          layout="vertical"
          onFinish={async (values) => {
            setBusy(true);
            setError("");
            try {
              await login(values.username, values.password);
              window.location.assign("/");
            } catch (e) {
              setError((e as Error).message);
            } finally {
              setBusy(false);
            }
          }}
        >
          <Form.Item
            name="username"
            label="管理员账号"
            rules={[{ required: true }]}
          >
            <Input autoComplete="username" size="large" />
          </Form.Item>
          <Form.Item name="password" label="密码" rules={[{ required: true }]}>
            <Input.Password autoComplete="current-password" size="large" />
          </Form.Item>
          <Button
            type="primary"
            htmlType="submit"
            loading={busy}
            size="large"
            block
          >
            登录控制台
          </Button>
        </Form>
        <Typography.Text type="secondary">
          仅供授权管理员使用 · {process.env.APP_VERSION}
        </Typography.Text>
      </section>
    </main>
  );
}
