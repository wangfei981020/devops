// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

import { useState, useEffect } from 'react';
import { Card, Tag, Button, Space, message, Alert, Modal, Form, Input, Switch, InputNumber, Table, Popconfirm } from 'antd';
import { KeyOutlined, CopyOutlined, ReloadOutlined, PlusOutlined, DeleteOutlined } from '@ant-design/icons';
import { authAPI } from '../lib/api';
import { getApiErrorMessage } from '../lib/httpError';
import type { Token } from '../lib/api';

export default function TokenManagementPage() {
  const [tokens, setTokens] = useState<Token[]>([]);
  const [tokensLoading, setTokensLoading] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [createModalVisible, setCreateModalVisible] = useState(false);
  const [creating, setCreating] = useState(false);
  const [newToken, setNewToken] = useState<string | null>(null);
  const [form] = Form.useForm();
  const [pagination, setPagination] = useState({ current: 1, pageSize: 10 });

  const fetchTokens = async () => {
    try {
      setTokensLoading(true);
      const data = await authAPI.getTokens();
      // 过滤掉默认的管理员 token（通过登录获得的 token 不会在列表中，这里过滤掉可能的默认 token）
      // 如果 token 名称包含 "admin" 或 "默认" 或 "default"，则过滤掉
      const filteredTokens = (data || []).filter(
        (token) =>
          !token.name.toLowerCase().includes('admin') &&
          !token.name.toLowerCase().includes('默认') &&
          !token.name.toLowerCase().includes('default') &&
          !token.name.toLowerCase().includes('login')
      );
      setTokens(filteredTokens);
    } catch (error: unknown) {
      message.error(getApiErrorMessage(error, '获取 Token 列表失败'));
      setTokens([]);
    } finally {
      setTokensLoading(false);
    }
  };

  useEffect(() => {
    fetchTokens();
  }, []);

  const handleRefresh = async () => {
    setRefreshing(true);
    await fetchTokens();
    setRefreshing(false);
    message.success('Token 列表已刷新');
  };

  const handleCreateToken = async (values: {
    name: string;
    never_expire: boolean;
    expires_in?: number;
  }) => {
    setCreating(true);
    try {
      const result = await authAPI.createToken(
        values.name,
        values.never_expire,
        values.never_expire ? undefined : values.expires_in
      );
      setNewToken(result.token);
      message.success('Token 创建成功');
      form.resetFields();
      await fetchTokens(); // Refresh token list
    } catch (error: unknown) {
      message.error(getApiErrorMessage(error, '创建 Token 失败'));
    } finally {
      setCreating(false);
    }
  };

  const handleCopyNewToken = () => {
    if (newToken) {
      navigator.clipboard.writeText(newToken);
      message.success('新 Token 已复制到剪贴板');
    }
  };

  const handleCloseCreateModal = () => {
    setCreateModalVisible(false);
    setNewToken(null);
    form.resetFields();
  };

  const handleDeleteToken = async (id: number) => {
    try {
      await authAPI.deleteToken(id);
      message.success('Token 删除成功');
      await fetchTokens();
    } catch (error: unknown) {
      message.error(getApiErrorMessage(error, '删除 Token 失败'));
    }
  };

  const formatDuration = (seconds: number) => {
    const days = Math.floor(seconds / 86400);
    const hours = Math.floor((seconds % 86400) / 3600);
    const minutes = Math.floor((seconds % 3600) / 60);
    if (days > 0) {
      return `${days}天 ${hours}小时`;
    } else if (hours > 0) {
      return `${hours}小时 ${minutes}分钟`;
    } else {
      return `${minutes}分钟`;
    }
  };

  return (
    <div className="vm-page">
      <h2 className="vm-page-title">
        <KeyOutlined style={{ marginRight: 8 }} />
        Token 管理
      </h2>

      <Card
        className="vm-panel-card"
        style={{ marginTop: 16 }}
        title="已创建的 Token 列表"
        extra={
          <Space>
            <Button
              type="primary"
              icon={<PlusOutlined />}
              onClick={() => setCreateModalVisible(true)}
            >
              创建新 Token
            </Button>
            <Button
              icon={<ReloadOutlined />}
              onClick={handleRefresh}
              loading={refreshing}
            >
              刷新
            </Button>
          </Space>
        }
      >
        <Table
          columns={[
            {
              title: 'ID',
              dataIndex: 'id',
              key: 'id',
              width: 80,
            },
            {
              title: '名称',
              dataIndex: 'name',
              key: 'name',
            },
            {
              title: '永不过期',
              dataIndex: 'never_expire',
              key: 'never_expire',
              render: (neverExpire: boolean) => (
                <Tag color={neverExpire ? 'red' : 'green'}>
                  {neverExpire ? '是' : '否'}
                </Tag>
              ),
            },
            {
              title: '过期时间',
              dataIndex: 'expires_at',
              key: 'expires_at',
              render: (expiresAt: string | null, record: Token) => {
                if (record.never_expire) {
                  return <Tag color="blue">永不过期</Tag>;
                }
                if (!expiresAt) {
                  return '-';
                }
                const date = new Date(expiresAt);
                const now = new Date();
                const isExpired = date < now;
                return (
                  <Space>
                    <span>{date.toLocaleString('zh-CN')}</span>
                    {isExpired ? (
                      <Tag color="red">已过期</Tag>
                    ) : (
                      <Tag color="green">有效</Tag>
                    )}
                  </Space>
                );
              },
            },
            {
              title: '创建时间',
              dataIndex: 'created_at',
              key: 'created_at',
              render: (date: string) => new Date(date).toLocaleString('zh-CN'),
            },
            {
              title: '操作',
              key: 'actions',
              width: 100,
              render: (_, record: Token) => (
                <Popconfirm
                  title="删除 Token"
                  description="确定要删除这个 Token 吗？此操作无法撤销。"
                  onConfirm={() => handleDeleteToken(record.id)}
                  okText="确定"
                  cancelText="取消"
                  okButtonProps={{ danger: true }}
                >
                  <Button
                    type="link"
                    danger
                    icon={<DeleteOutlined />}
                    size="small"
                  >
                    删除
                  </Button>
                </Popconfirm>
              ),
            },
          ]}
          dataSource={tokens || []}
          rowKey="id"
          loading={tokensLoading}
          pagination={{
            current: pagination.current,
            pageSize: pagination.pageSize,
            showSizeChanger: true,
            showQuickJumper: true,
            showTotal: (total, range) => `第 ${range[0]}-${range[1]} 条，共 ${total} 个 Token`,
            pageSizeOptions: ['10', '20', '50', '100'],
            onChange: (page, size) => {
              setPagination({ current: page, pageSize: size });
            },
            onShowSizeChange: (_current, size) => {
              setPagination({ current: 1, pageSize: size });
            },
          }}
        />
      </Card>

      <Card style={{ marginTop: 24 }} title="使用说明">
        <ul>
          <li>您可以创建多个 Token，每个 Token 可以设置不同的过期时间</li>
          <li>Token 存储在浏览器的 localStorage 中</li>
          <li>复制 Token 后可以在 API 客户端中使用（如 Postman、curl 等）</li>
          <li>建议为不同用途创建不同的 Token，便于管理和撤销</li>
          <li>永不过期的 Token 请妥善保管，建议定期更换</li>
          <li>删除 Token 只会从列表中移除，已签发的 Token 仍然有效直到过期</li>
        </ul>
      </Card>

      <Modal
        title="创建新 Token"
        open={createModalVisible}
        onCancel={handleCloseCreateModal}
        footer={null}
        width={600}
      >
        {newToken ? (
          <div>
            <Alert
              title="Token 创建成功"
              description="请妥善保管此 Token，它只会显示一次。"
              type="success"
              showIcon
              style={{ marginBottom: 24 }}
            />
            <Form.Item label="Token 名称">
              <Input value={form.getFieldValue('name')} disabled />
            </Form.Item>
            <Form.Item label="Token 值">
              <Input.TextArea
                value={newToken}
                rows={4}
                readOnly
                style={{ fontFamily: 'monospace', fontSize: 12 }}
              />
            </Form.Item>
            <Space style={{ width: '100%', justifyContent: 'flex-end' }}>
              <Button onClick={handleCloseCreateModal}>关闭</Button>
              <Button type="primary" icon={<CopyOutlined />} onClick={handleCopyNewToken}>
                复制 Token
              </Button>
            </Space>
          </div>
        ) : (
          <Form
            form={form}
            onFinish={handleCreateToken}
            layout="vertical"
            initialValues={{
              never_expire: false,
              expires_in: 86400, // 24 hours in seconds
            }}
          >
            <Form.Item
              name="name"
              label="Token 名称"
              rules={[
                { required: true, message: '请输入 Token 名称' },
                {
                  validator: async (_, value) => {
                    if (!value) return Promise.resolve();
                    // 检查名称是否已存在（同一用户下）
                    const existing = tokens.find(
                      (t) => t.name.toLowerCase() === value.toLowerCase().trim()
                    );
                    if (existing) {
                      return Promise.reject(new Error('Token 名称已存在'));
                    }
                    return Promise.resolve();
                  },
                },
              ]}
              tooltip="为 Token 起一个便于识别的名称，如：开发环境、生产环境等"
            >
              <Input placeholder="例如：开发环境 Token" />
            </Form.Item>

            <Form.Item
              name="never_expire"
              label="永不过期"
              valuePropName="checked"
            >
              <Switch />
            </Form.Item>

            <Form.Item
              noStyle
              shouldUpdate={(prevValues, currentValues) =>
                prevValues.never_expire !== currentValues.never_expire
              }
            >
              {({ getFieldValue }) =>
                !getFieldValue('never_expire') ? (
                  <Form.Item
                    name="expires_in"
                    label="过期时长（秒）"
                    rules={[
                      { required: true, message: '请输入过期时长' },
                      { type: 'number', min: 60, message: '过期时长至少为 60 秒' },
                    ]}
                    tooltip="Token 的有效期，单位为秒。例如：3600 = 1小时，86400 = 1天"
                  >
                    <InputNumber
                      style={{ width: '100%' }}
                      min={60}
                      step={3600}
                      placeholder="例如：86400（24小时）"
                      addonAfter="秒"
                      formatter={(value) => {
                        if (!value) return '';
                        const seconds = Number(value);
                        return `${value} (${formatDuration(seconds)})`;
                      }}
                    />
                  </Form.Item>
                ) : null
              }
            </Form.Item>

            <Form.Item>
              <Space style={{ width: '100%', justifyContent: 'flex-end' }}>
                <Button onClick={handleCloseCreateModal}>取消</Button>
                <Button type="primary" htmlType="submit" loading={creating}>
                  创建 Token
                </Button>
              </Space>
            </Form.Item>
          </Form>
        )}
      </Modal>
    </div>
  );
}

