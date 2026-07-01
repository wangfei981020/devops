// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

import { useState, useEffect } from 'react';
import { Modal, Form, Input, message } from 'antd';
import { providerAPI } from '../lib/api';
import type { CDNProvider } from '../lib/api';
import { getApiErrorMessage, isAntdFormValidateError } from '../lib/httpError';

interface ProviderFormProps {
  provider: CDNProvider | null;
  open: boolean;
  onClose: () => void;
  onSubmit: () => void;
  providers?: CDNProvider[]; // 用于前端唯一性验证
}

export default function ProviderForm({ provider, open, onClose, onSubmit, providers = [] }: ProviderFormProps) {
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (open) {
      if (provider) {
        form.setFieldsValue({
          name: provider.name,
          code: provider.code,
        });
      } else {
        form.resetFields();
      }
    }
  }, [open, provider, form]);

  const handleSubmit = async () => {
    try {
      const values = await form.validateFields();
      setLoading(true);

      if (provider) {
        await providerAPI.update(provider.id, values);
        message.success('Provider 更新成功');
      } else {
        await providerAPI.create(values);
        message.success('Provider 创建成功');
      }

      onSubmit();
      onClose();
    } catch (err: unknown) {
      if (isAntdFormValidateError(err)) {
        return;
      }
      message.error(getApiErrorMessage(err, '保存失败 provider'));
    } finally {
      setLoading(false);
    }
  };

  return (
    <Modal
      title={provider ? '编辑 Provider' : '创建 Provider'}
      open={open}
      onOk={handleSubmit}
      onCancel={onClose}
      confirmLoading={loading}
      okText="保存"
      cancelText="取消"
    >
      <Form
        form={form}
        layout="vertical"
        autoComplete="off"
      >
        <Form.Item
          name="name"
          label="名称"
          rules={[
            { required: true, message: '请输入厂商名称' },
            { min: 1, message: '名称不能为空' },
            { max: 255, message: '名称不能超过255个字符' },
            { whitespace: true, message: '名称不能仅为空白字符' },
            {
              validator: async (_, value) => {
                if (!value) return Promise.resolve();
                // 检查名称是否已存在（排除当前编辑的 provider）
                const existing = providers.find(
                  (p) => p.name.toLowerCase() === value.toLowerCase().trim() && p.id !== provider?.id
                );
                if (existing) {
                  return Promise.reject(new Error('厂商名称已存在'));
                }
                return Promise.resolve();
              },
            },
          ]}
        >
          <Input placeholder="输入 provider name" showCount maxLength={255} />
        </Form.Item>

        <Form.Item
          name="code"
          label="代码"
          rules={[
            { required: true, message: '请输入厂商代码' },
            { min: 1, message: '代码不能为空' },
            { max: 100, message: '代码不能超过100个字符' },
            { pattern: /^[a-zA-Z0-9_-]+$/, message: '代码只能包含字母、数字、下划线和连字符' },
            { whitespace: true, message: '代码不能仅为空白字符' },
          ]}
        >
          <Input placeholder="输入 provider code (alphanumeric, underscore, hyphen)" showCount maxLength={100} />
        </Form.Item>
      </Form>
    </Modal>
  );
}
