// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

import { useState, useEffect, type ReactNode } from 'react';
import { useNavigate, useLocation } from 'react-router-dom';
import { Layout, Menu, Space, Typography, Dropdown, Button, Modal, Form, Input, message } from 'antd';
import {
  DashboardOutlined,
  CloudServerOutlined,
  LinkOutlined,
  GlobalOutlined,
  PlayCircleOutlined,
  FileTextOutlined,
  ApiOutlined,
  UserOutlined,
  LogoutOutlined,
  LockOutlined,
  KeyOutlined,
  BookOutlined,
} from '@ant-design/icons';
import Logo from './Logo';
import { auth } from '../lib/auth';
import { authAPI } from '../lib/api';

const { Sider, Content, Footer } = Layout;
const { Text } = Typography;

const ROUTE_TITLES: Record<string, string> = {
  '/dashboard': '仪表板',
  '/providers': 'CDN 厂商',
  '/lines': 'CDN 线路',
  '/domains': '域名',
  '/stream-regions': '视频流区域',
  '/stream-paths': '流路径',
  '/endpoints': '视频流端点',
  '/token-management': 'Token 管理',
  '/users': '用户管理',
  '/system-settings': '系统设置',
  '/swagger': 'API 文档',
};

interface DashboardLayoutProps {
  children: ReactNode;
}

export default function DashboardLayout({ children }: DashboardLayoutProps) {
  const navigate = useNavigate();
  const location = useLocation();
  const [changePasswordVisible, setChangePasswordVisible] = useState(false);
  const [changePasswordLoading, setChangePasswordLoading] = useState(false);
  const [form] = Form.useForm();
  const user = auth.getUser();

  useEffect(() => {
    const checkSystemStatus = async () => {
      try {
        await Promise.resolve();
      } catch {
        /* noop */
      }
    };
    checkSystemStatus();
    const statusInterval = setInterval(checkSystemStatus, 30000);
    return () => clearInterval(statusInterval);
  }, []);

  const menuItems = user?.is_admin
    ? [
        { key: '/dashboard', icon: <DashboardOutlined />, label: '仪表板' },
        { key: '/providers', icon: <CloudServerOutlined />, label: 'CDN 厂商' },
        { key: '/lines', icon: <LinkOutlined />, label: 'CDN 线路' },
        { key: '/domains', icon: <GlobalOutlined />, label: '域名' },
        { key: '/stream-regions', icon: <PlayCircleOutlined />, label: '视频流区域' },
        { key: '/stream-paths', icon: <FileTextOutlined />, label: '流路径' },
        { key: '/endpoints', icon: <ApiOutlined />, label: '视频流端点' },
        { key: '/token-management', icon: <KeyOutlined />, label: 'Token 管理' },
        { key: '/users', icon: <UserOutlined />, label: '用户管理' },
        { key: '/system-settings', icon: <BookOutlined />, label: '系统设置' },
        { key: '/swagger', icon: <BookOutlined />, label: 'API 文档' },
      ]
    : [
        { key: '/dashboard', icon: <DashboardOutlined />, label: '仪表板' },
        { key: '/endpoints', icon: <ApiOutlined />, label: '视频流端点' },
      ];

  const selectedKey =
    menuItems.find(
      (item) => location.pathname === item.key || location.pathname.startsWith(`${item.key}/`)
    )?.key || '/dashboard';

  const pageTitle = ROUTE_TITLES[location.pathname] ?? ROUTE_TITLES[selectedKey] ?? '控制台';

  const handleLogout = () => {
    auth.logout();
    navigate('/login');
  };

  const handleChangePassword = async (values: { old_password: string; new_password: string }) => {
    setChangePasswordLoading(true);
    try {
      await authAPI.changePassword(values.old_password, values.new_password);
      message.success('密码修改成功');
      setChangePasswordVisible(false);
      form.resetFields();
    } catch (error: unknown) {
      const err = error as { response?: { data?: { message?: string } } };
      message.error(err.response?.data?.message || '密码修改失败');
    } finally {
      setChangePasswordLoading(false);
    }
  };

  const userMenuItems = [
    {
      key: 'change-password',
      icon: <LockOutlined />,
      label: '修改密码',
      onClick: () => setChangePasswordVisible(true),
    },
    {
      key: 'logout',
      icon: <LogoutOutlined />,
      label: '退出登录',
      onClick: handleLogout,
    },
  ];

  return (
    <Layout style={{ minHeight: '100vh', background: 'var(--vm-bg)' }}>
      <Sider
        theme="dark"
        width={232}
        style={{
          background: 'linear-gradient(180deg, #0c1222 0%, #0a0f18 100%)',
          borderRight: '1px solid rgba(255,255,255,0.06)',
        }}
      >
        <div
          style={{
            height: 64,
            display: 'flex',
            alignItems: 'center',
            padding: '0 18px',
            borderBottom: '1px solid rgba(255,255,255,0.08)',
            gap: 12,
            cursor: 'pointer',
          }}
          onClick={() => navigate('/dashboard')}
        >
          <Logo size={36} />
          <div style={{ minWidth: 0 }}>
            <div
              style={{
                fontSize: 15,
                fontWeight: 700,
                letterSpacing: '-0.02em',
                color: '#f8fafc',
                lineHeight: 1.2,
                whiteSpace: 'nowrap',
                overflow: 'hidden',
                textOverflow: 'ellipsis',
              }}
            >
              视频管理
            </div>
            <div
              style={{
                fontSize: 11,
                fontWeight: 500,
                color: 'rgba(148, 163, 184, 0.95)',
                marginTop: 2,
                letterSpacing: '0.04em',
              }}
            >
              VIDEO OPS
            </div>
          </div>
        </div>
        <Menu
          mode="inline"
          className="vm-sider-menu"
          selectedKeys={[selectedKey === '/swagger' ? '' : selectedKey]}
          items={menuItems}
          onClick={({ key }) => {
            if (key === '/swagger') {
              window.open('/swagger/index.html', '_blank');
            } else {
              navigate(key);
            }
          }}
          style={{
            borderRight: 0,
            height: 'calc(100vh - 64px)',
            background: 'transparent',
            padding: '12px 8px',
          }}
          theme="dark"
        />
      </Sider>
      <Layout style={{ background: 'transparent' }}>
        <header
          style={{
            position: 'fixed',
            top: 0,
            left: 232,
            right: 0,
            height: 56,
            background: 'rgba(255,255,255,0.85)',
            backdropFilter: 'blur(12px)',
            WebkitBackdropFilter: 'blur(12px)',
            borderBottom: '1px solid var(--vm-border)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            padding: '0 24px',
            zIndex: 100,
            boxShadow: '0 1px 0 rgba(15, 23, 42, 0.04)',
          }}
        >
          <div>
            <Text strong style={{ fontSize: 17, color: 'var(--vm-text)', letterSpacing: '-0.02em' }}>
              {pageTitle}
            </Text>
            <div style={{ fontSize: 12, color: 'var(--vm-muted)', marginTop: 2 }}>控制台 · 配置与端点</div>
          </div>
          <Dropdown menu={{ items: userMenuItems }} placement="bottomRight" trigger={['click']}>
            <Button
              type="text"
              icon={<UserOutlined />}
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: 8,
                height: 40,
                padding: '0 14px',
                borderRadius: 10,
                border: '1px solid var(--vm-border)',
                background: '#fff',
              }}
            >
              {user?.username || '用户'}
            </Button>
          </Dropdown>
        </header>
        <Content
          style={{
            margin: '24px',
            marginTop: 80,
            marginBottom: 56,
            padding: '28px',
            background: '#fff',
            minHeight: 280,
            borderRadius: 14,
            border: '1px solid var(--vm-border)',
            boxShadow: 'var(--vm-card-shadow)',
          }}
        >
          {children}
        </Content>
        <Footer
          style={{
            position: 'fixed',
            bottom: 0,
            left: 232,
            right: 0,
            height: 44,
            padding: '0 24px',
            background: 'rgba(255,255,255,0.92)',
            backdropFilter: 'blur(8px)',
            borderTop: '1px solid var(--vm-border)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            zIndex: 100,
          }}
        >
          <Text style={{ color: 'var(--vm-muted)', fontSize: 12 }}>
            视频管理系统 · 内部运营配置台
          </Text>
        </Footer>
      </Layout>

      <Modal
        title="修改密码"
        open={changePasswordVisible}
        destroyOnHidden
        onCancel={() => {
          setChangePasswordVisible(false);
          form.resetFields();
        }}
        footer={null}
      >
        <Form form={form} onFinish={handleChangePassword} layout="vertical">
          <Form.Item
            name="old_password"
            label="当前密码"
            rules={[{ required: true, message: '请输入当前密码' }]}
          >
            <Input.Password placeholder="请输入当前密码" />
          </Form.Item>
          <Form.Item
            name="new_password"
            label="新密码"
            rules={[
              { required: true, message: '请输入新密码' },
              { min: 6, message: '密码长度至少6位' },
            ]}
          >
            <Input.Password placeholder="请输入新密码（至少6位）" />
          </Form.Item>
          <Form.Item
            name="confirm_password"
            label="确认新密码"
            dependencies={['new_password']}
            rules={[
              { required: true, message: '请确认新密码' },
              ({ getFieldValue }) => ({
                validator(_, value) {
                  if (!value || getFieldValue('new_password') === value) {
                    return Promise.resolve();
                  }
                  return Promise.reject(new Error('两次输入的密码不一致'));
                },
              }),
            ]}
          >
            <Input.Password placeholder="请再次输入新密码" />
          </Form.Item>
          <Form.Item>
            <Space style={{ width: '100%', justifyContent: 'flex-end' }}>
              <Button
                onClick={() => {
                  setChangePasswordVisible(false);
                  form.resetFields();
                }}
              >
                取消
              </Button>
              <Button type="primary" htmlType="submit" loading={changePasswordLoading}>
                确定
              </Button>
            </Space>
          </Form.Item>
        </Form>
      </Modal>
    </Layout>
  );
}
