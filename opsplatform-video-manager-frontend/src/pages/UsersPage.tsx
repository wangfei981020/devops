// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

import { useCallback, useEffect, useMemo, useState } from 'react';
import { Button, Form, Input, message, Modal, Popconfirm, Space, Switch, Table, Tag } from 'antd';
import { DeleteOutlined, EditOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';
import { getApiErrorMessage, isAntdFormValidateError } from '../lib/httpError';
import { type ManagedUser, userAPI } from '../lib/api';
import { auth } from '../lib/auth';

type UserFormValues = {
  username: string;
  password?: string;
  is_admin: boolean;
};

export default function UsersPage() {
  const [users, setUsers] = useState<ManagedUser[]>([]);
  const [loading, setLoading] = useState(false);
  const [modalOpen, setModalOpen] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [editingUser, setEditingUser] = useState<ManagedUser | null>(null);
  const [form] = Form.useForm();
  const currentUser = auth.getUser();

  const loadUsers = useCallback(async () => {
    setLoading(true);
    try {
      const data = await userAPI.getAll();
      setUsers(data);
    } catch (error: unknown) {
      message.error(getApiErrorMessage(error, '加载用户列表失败'));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadUsers();
  }, [loadUsers]);

  const openCreateModal = () => {
    setEditingUser(null);
    form.setFieldsValue({ username: '', password: '', is_admin: false });
    setModalOpen(true);
  };

  const openEditModal = (user: ManagedUser) => {
    setEditingUser(user);
    form.setFieldsValue({ username: user.username, is_admin: user.is_admin });
    setModalOpen(true);
  };

  const closeModal = () => {
    setModalOpen(false);
    setEditingUser(null);
    form.resetFields();
  };

  const handleSubmit = async () => {
    try {
      const values = await form.validateFields();
      setSubmitting(true);
      if (editingUser) {
        await userAPI.update(editingUser.id, {
          username: values.username,
          is_admin: values.is_admin,
        });
        message.success('用户更新成功');
      } else {
        await userAPI.create({
          username: values.username,
          password: values.password || '',
          is_admin: values.is_admin,
        });
        message.success('用户创建成功');
      }
      closeModal();
      await loadUsers();
    } catch (error: unknown) {
      if (isAntdFormValidateError(error)) {
        return;
      }
      message.error(getApiErrorMessage(error, '保存用户失败'));
    } finally {
      setSubmitting(false);
    }
  };

  const handleDelete = async (user: ManagedUser) => {
    try {
      await userAPI.delete(user.id);
      message.success('用户删除成功');
      await loadUsers();
    } catch (error: unknown) {
      message.error(getApiErrorMessage(error, '删除用户失败'));
    }
  };

  const columns: ColumnsType<ManagedUser> = useMemo(
    () => [
      { title: 'ID', dataIndex: 'id', key: 'id', width: 90 },
      { title: '用户名', dataIndex: 'username', key: 'username' },
      {
        title: '角色',
        dataIndex: 'is_admin',
        key: 'is_admin',
        width: 120,
        render: (isAdmin: boolean) =>
          isAdmin ? <Tag color="gold">管理员</Tag> : <Tag color="blue">普通用户</Tag>,
      },
      {
        title: '创建时间',
        dataIndex: 'created_at',
        key: 'created_at',
        width: 190,
        render: (value: string) => new Date(value).toLocaleString(),
      },
      {
        title: '操作',
        key: 'actions',
        width: 200,
        render: (_, record) => (
          <Space>
            <Button type="link" icon={<EditOutlined />} onClick={() => openEditModal(record)}>
              编辑
            </Button>
            <Popconfirm
              title="删除用户"
              description={`确认删除用户 ${record.username} 吗？`}
              okText="确定"
              cancelText="取消"
              okButtonProps={{ danger: true }}
              onConfirm={() => handleDelete(record)}
            >
              <Button
                type="link"
                danger
                icon={<DeleteOutlined />}
                disabled={currentUser?.id === record.id}
              >
                删除
              </Button>
            </Popconfirm>
          </Space>
        ),
      },
    ],
    [currentUser?.id]
  );

  return (
    <div className="vm-page">
      <div
        style={{
          marginBottom: 16,
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          flexWrap: 'wrap',
          gap: 12,
        }}
      >
        <h2 className="vm-page-title">用户管理</h2>
        <Space>
          <Button icon={<ReloadOutlined />} onClick={loadUsers} loading={loading}>
            刷新
          </Button>
          <Button type="primary" icon={<PlusOutlined />} onClick={openCreateModal}>
            新建用户
          </Button>
        </Space>
      </div>

      <Table
        rowKey="id"
        loading={loading}
        columns={columns}
        dataSource={users}
        pagination={{
          showSizeChanger: true,
          showQuickJumper: true,
          showTotal: (total, range) => `第 ${range[0]}-${range[1]} 条，共 ${total} 个用户`,
        }}
      />

      <Modal
        title={editingUser ? '编辑用户' : '新建用户'}
        open={modalOpen}
        onCancel={closeModal}
        onOk={handleSubmit}
        okText="保存"
        cancelText="取消"
        confirmLoading={submitting}
        destroyOnHidden
      >
        <Form form={form} layout="vertical" initialValues={{ is_admin: false }}>
          <Form.Item
            label="用户名"
            name="username"
            rules={[
              { required: true, message: '请输入用户名' },
              { min: 1, max: 255, message: '用户名长度需在 1-255 之间' },
            ]}
          >
            <Input placeholder="请输入用户名" />
          </Form.Item>
          {!editingUser && (
            <Form.Item
              label="密码"
              name="password"
              rules={[
                { required: true, message: '请输入密码' },
                { min: 6, message: '密码至少 6 位' },
              ]}
            >
              <Input.Password placeholder="请输入密码（至少 6 位）" />
            </Form.Item>
          )}
          <Form.Item label="管理员权限" name="is_admin" valuePropName="checked">
            <Switch checkedChildren="管理员" unCheckedChildren="普通用户" />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
}
