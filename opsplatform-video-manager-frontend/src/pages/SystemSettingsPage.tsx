import { useEffect, useState } from 'react';
import { Alert, Button, Card, Form, Input, Space, Switch, Typography, message } from 'antd';
import { auth } from '../lib/auth';
import {
  systemSettingsAPI,
  type OIDCProbeResult,
  type OIDCSettings,
  type UpdateOIDCSettingsRequest,
} from '../lib/api';

const { Text } = Typography;

export default function SystemSettingsPage() {
  const user = auth.getUser();
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [probing, setProbing] = useState(false);
  const [probeResult, setProbeResult] = useState<OIDCProbeResult | null>(null);
  const [hasClientSecret, setHasClientSecret] = useState(false);

  const loadSettings = async () => {
    setLoading(true);
    try {
      const data: OIDCSettings = await systemSettingsAPI.getOIDCSettings();
      form.setFieldsValue({
        enabled: data.enabled,
        issuer_url: data.issuer_url,
        client_id: data.client_id,
        redirect_url: data.redirect_url,
        scopes: data.scopes,
        frontend_success_url: data.frontend_success_url,
      });
      setHasClientSecret(data.has_client_secret);
    } catch (error: unknown) {
      const err = error as { response?: { data?: { message?: string } } };
      message.error(err.response?.data?.message || '加载系统设置失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void loadSettings();
  }, []);

  const handleProbe = async () => {
    setProbing(true);
    try {
      const result = await systemSettingsAPI.probeOIDCSettings();
      setProbeResult(result);
      if (result.probe_result === 'client_credentials_ok') {
        message.success('Client Secret 校验通过');
      } else {
        message.warning(result.hint || 'OIDC 探测未通过');
      }
    } catch (error: unknown) {
      const err = error as { response?: { data?: { message?: string } } };
      message.error(err.response?.data?.message || 'OIDC 探测失败');
    } finally {
      setProbing(false);
    }
  };

  const handleSubmit = async (values: UpdateOIDCSettingsRequest) => {
    if (values.enabled && !hasClientSecret && !values.client_secret?.trim()) {
      message.error('启用 SSO 前请先填写 Client Secret');
      return;
    }
    setSaving(true);
    try {
      await systemSettingsAPI.updateOIDCSettings(values);
      message.success('系统设置已保存');
      await loadSettings();
      form.setFieldValue('client_secret', '');
    } catch (error: unknown) {
      const err = error as { response?: { data?: { message?: string } } };
      message.error(err.response?.data?.message || '保存系统设置失败');
    } finally {
      setSaving(false);
    }
  };

  if (!user?.is_admin) {
    return <Alert type="warning" message="仅管理员可访问系统设置页面" showIcon />;
  }

  return (
    <Card title="系统设置">
      <Space direction="vertical" size={16} style={{ width: '100%' }}>
        <Alert
          type="info"
          showIcon
          message="OIDC SSO 配置"
          description="保存后立即生效。SSO 登录用户会自动创建为普通用户（非管理员）。"
        />

        {!hasClientSecret && (
          <Alert
            type="warning"
            showIcon
            message="尚未配置 Client Secret"
            description="启用 SSO 前必须在下方填写 Client Secret 并保存。该值来自 IdP 应用配置，不会回显。"
          />
        )}

        <Form
          form={form}
          layout="vertical"
          onFinish={handleSubmit}
          initialValues={{
            enabled: false,
            scopes: 'openid profile email',
          }}
        >
          <Form.Item name="enabled" label="启用 OIDC SSO" valuePropName="checked">
            <Switch />
          </Form.Item>

          <Form.Item
            name="issuer_url"
            label="Issuer URL"
            rules={[{ required: true, message: '请输入 Issuer URL' }, { type: 'url', message: '请输入合法 URL' }]}
          >
            <Input placeholder="https://idp.example.com/realms/myrealm" />
          </Form.Item>

          <Form.Item name="client_id" label="Client ID" rules={[{ required: true, message: '请输入 Client ID' }]}>
            <Input />
          </Form.Item>

          <Form.Item
            name="client_secret"
            label="Client Secret"
            rules={
              hasClientSecret
                ? []
                : [{ required: true, message: '首次配置时必须填写 Client Secret' }]
            }
            extra={
              hasClientSecret
                ? '已配置。留空保存表示不修改；填写新值保存将覆盖旧 Secret。'
                : '必填。从 IdP 控制台复制 Client Secret。'
            }
          >
            <Input.Password placeholder={hasClientSecret ? '留空则不修改，填写则覆盖' : '粘贴 Client Secret'} />
          </Form.Item>

          <Form.Item
            name="redirect_url"
            label="Redirect URL"
            rules={[{ required: true, message: '请输入 Redirect URL' }, { type: 'url', message: '请输入合法 URL' }]}
          >
            <Input placeholder="http://localhost:8080/api/auth/oidc/callback" />
          </Form.Item>

          <Form.Item name="scopes" label="Scopes">
            <Input placeholder="openid profile email" />
          </Form.Item>

          <Form.Item
            name="frontend_success_url"
            label="Frontend Success URL"
            rules={[{ required: true, message: '请输入前端成功跳转 URL' }, { type: 'url', message: '请输入合法 URL' }]}
          >
            <Input placeholder="https://video-manager.slileisure.com/login" />
          </Form.Item>

          <Space>
            <Button type="primary" htmlType="submit" loading={saving || loading}>
              保存设置
            </Button>
            <Button onClick={() => void loadSettings()} loading={loading}>
              刷新
            </Button>
            <Button onClick={() => void handleProbe()} loading={probing}>
              探测 Client Secret
            </Button>
          </Space>
        </Form>

        {probeResult && (
          <Alert
            type={probeResult.probe_result === 'client_credentials_ok' ? 'success' : 'warning'}
            showIcon
            message={`探测结果: ${probeResult.probe_result}`}
            description={
              <>
                <div>{probeResult.hint}</div>
                <div>Redirect URL: {probeResult.redirect_url}</div>
                <div>Token endpoint: {probeResult.token_endpoint}</div>
              </>
            }
          />
        )}

        <Text type="secondary">
          提示：SSO 入口 `/api/auth/oidc/login`，IdP 回调 `/api/auth/oidc/callback`；成功后跳转到
          Frontend Success URL（如 `https://video-manager.slileisure.com/login?sso_token=...`），不要填
          OIDC API 地址。
        </Text>
      </Space>
    </Card>
  );
}
