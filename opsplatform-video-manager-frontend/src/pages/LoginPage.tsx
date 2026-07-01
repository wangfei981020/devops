// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { App, Form, Input, Button, Typography } from 'antd';
import { UserOutlined, LockOutlined } from '@ant-design/icons';
import { authAPI } from '../lib/api';
import { auth } from '../lib/auth';
import Logo from '../components/Logo';

const { Title, Text } = Typography;

export default function LoginPage() {
  const { message } = App.useApp();
  const navigate = useNavigate();
  const [loading, setLoading] = useState(false);
  const [ssoLoading, setSsoLoading] = useState(false);
  const [oidcReady, setOidcReady] = useState(false);

  useEffect(() => {
    const loadOIDCStatus = async () => {
      try {
        const status = await authAPI.getOIDCStatus();
        setOidcReady(status.ready);
      } catch {
        setOidcReady(false);
      }
    };
    void loadOIDCStatus();
  }, []);

  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const ssoError = params.get('sso_error');
    if (ssoError) {
      message.error(ssoError);
      window.history.replaceState({}, '', window.location.pathname);
      return;
    }

    const ssoToken = params.get('sso_token');
    if (!ssoToken) return;

    const completeSSOLogin = async () => {
      setSsoLoading(true);
      try {
        auth.setToken(ssoToken);
        const me = await authAPI.getCurrentUser();
        auth.setUser(me);
        message.success('SSO 登录成功');
        navigate('/dashboard', { replace: true });
      } catch (error: unknown) {
        auth.logout();
        const err = error as { response?: { data?: { message?: string } } };
        message.error(err.response?.data?.message || 'SSO 登录失败');
      } finally {
        setSsoLoading(false);
        const cleanURL = window.location.pathname;
        window.history.replaceState({}, '', cleanURL);
      }
    };

    void completeSSOLogin();
  }, [message, navigate]);

  const onFinish = async (values: { username: string; password: string }) => {
    setLoading(true);
    try {
      const response = await authAPI.login(values.username, values.password);
      auth.setToken(response.token);
      auth.setUser({
        id: 0,
        username: response.username,
        is_admin: response.is_admin,
      });
      message.success('登录成功');
      navigate('/dashboard');
    } catch (error: unknown) {
      console.error('Login error:', error);
      const err = error as { response?: { data?: { message?: string } }; message?: string };
      const errorMessage =
        err.response?.data?.message || err.message || '登录失败，请检查用户名和密码';
      message.error(errorMessage);
    } finally {
      setLoading(false);
    }
  };

  const handleOIDCLogin = () => {
    const apiBase = import.meta.env.VITE_API_BASE_URL || '/api';
    window.location.href = `${apiBase}/auth/oidc/login`;
  };

  return (
    <div className="vm-login-bg" style={{ display: 'flex', minHeight: '100vh' }}>
      <div
        style={{
          flex: '1 1 42%',
          minWidth: 280,
          padding: '48px 40px',
          display: 'flex',
          flexDirection: 'column',
          justifyContent: 'center',
          color: '#e2e8f0',
        }}
      >
        <div style={{ maxWidth: 400 }}>
          <Logo size={56} />
          <Title
            level={2}
            style={{
              color: '#f8fafc',
              marginTop: 28,
              marginBottom: 12,
              fontWeight: 700,
              letterSpacing: '-0.03em',
            }}
          >
            视频管理系统
          </Title>
          <Text style={{ color: 'rgba(148, 163, 184, 0.95)', fontSize: 15, lineHeight: 1.7 }}>
            统一管理 CDN、域名、流路径与播放端点。面向运营与工程团队的内部控制台。
          </Text>
          <div
            style={{
              marginTop: 36,
              paddingTop: 28,
              borderTop: '1px solid rgba(255,255,255,0.08)',
              fontSize: 12,
              color: 'rgba(148, 163, 184, 0.85)',
              letterSpacing: '0.06em',
              textTransform: 'uppercase',
            }}
          >
            Secure ops console
          </div>
        </div>
      </div>

      <div
        style={{
          flex: '1 1 58%',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          padding: '32px 24px',
        }}
      >
        <div
          style={{
            width: '100%',
            maxWidth: 420,
            padding: '40px 36px',
            borderRadius: 16,
            background: 'rgba(255,255,255,0.96)',
            border: '1px solid rgba(255,255,255,0.65)',
            boxShadow:
              '0 4px 24px rgba(15, 23, 42, 0.12), 0 0 0 1px rgba(15, 23, 42, 0.04)',
            backdropFilter: 'blur(12px)',
          }}
        >
          <div style={{ marginBottom: 28 }}>
            <Text strong style={{ fontSize: 18, color: 'var(--vm-text)' }}>
              登录
            </Text>
            <div style={{ marginTop: 6, fontSize: 13, color: 'var(--vm-muted)' }}>
              使用管理员账号进入控制台
            </div>
          </div>

          <Form name="login" onFinish={onFinish} autoComplete="off" layout="vertical" size="large">
            <Form.Item name="username" rules={[{ required: true, message: '请输入用户名' }]}>
              <Input prefix={<UserOutlined style={{ color: 'var(--vm-muted)' }} />} placeholder="用户名" />
            </Form.Item>

            <Form.Item name="password" rules={[{ required: true, message: '请输入密码' }]}>
              <Input.Password
                prefix={<LockOutlined style={{ color: 'var(--vm-muted)' }} />}
                placeholder="密码"
              />
            </Form.Item>

            <Form.Item style={{ marginBottom: 0, marginTop: 8 }}>
              <Button type="primary" htmlType="submit" loading={loading} block size="large">
                进入控制台
              </Button>
            </Form.Item>
            {oidcReady && (
              <Form.Item style={{ marginBottom: 0, marginTop: 12 }}>
                <Button onClick={handleOIDCLogin} loading={ssoLoading} block size="large">
                  使用 SSO 登录
                </Button>
              </Form.Item>
            )}
          </Form>
        </div>
      </div>
    </div>
  );
}
