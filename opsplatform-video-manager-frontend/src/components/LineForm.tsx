// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

import { useState, useEffect } from 'react';
import { Modal, Form, Input, Select, message } from 'antd';
import { lineAPI } from '../lib/api';
import type { CDNLine, CDNProvider } from '../lib/api';
import { selectSearchableProps } from '../lib/selectSearchProps';
import { getApiErrorMessage, isAntdFormValidateError } from '../lib/httpError';

interface LineFormProps {
  line: CDNLine | null;
  providers: CDNProvider[];
  lines?: CDNLine[]; // 用于前端唯一性验证
  open: boolean;
  onClose: () => void;
  onSubmit: () => void;
}

export default function LineForm({ line, providers, lines = [], open, onClose, onSubmit }: LineFormProps) {
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (open) {
      if (line) {
        form.setFieldsValue({
          provider_id: line.provider_id,
          name: line.name,
          code: line.code,
        });
      } else {
        form.resetFields();
        if (providers.length > 0) {
          form.setFieldsValue({ provider_id: providers[0].id });
        }
      }
    }
  }, [open, line, providers, form]);

  const handleSubmit = async () => {
    try {
      const values = await form.validateFields();
      setLoading(true);

      if (line) {
        await lineAPI.update(line.id, {
          provider_id: values.provider_id,
          name: values.name,
          code: values.code,
        });
        message.success('Line 更新成功');
      } else {
        await lineAPI.create({
          provider_id: values.provider_id,
          name: values.name,
          code: values.code,
        });
        message.success('Line 创建成功');
      }

      onSubmit();
      onClose();
    } catch (err: unknown) {
      if (isAntdFormValidateError(err)) {
        return;
      }
      message.error(getApiErrorMessage(err, '保存失败 line'));
    } finally {
      setLoading(false);
    }
  };

  return (
    <Modal
      title={line ? '编辑 Line' : '创建 Line'}
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
          name="provider_id"
          label="厂商"
          rules={[{ required: true, message: '请选择厂商' }]}
        >
          <Select placeholder="选择 a provider" {...selectSearchableProps}>
            {providers.map((provider) => (
              <Select.Option key={provider.id} value={provider.id} label={provider.name}>
                {provider.name}
              </Select.Option>
            ))}
          </Select>
        </Form.Item>

        <Form.Item
          name="code"
          label="代码"
          rules={[
            { required: true, message: '请输入线路代码' },
            { min: 1, message: '代码不能为空' },
            { max: 255, message: '代码不能超过255个字符' },
            { whitespace: true, message: '代码不能仅为空白字符' },
          ]}
        >
          <Input placeholder="输入线路代码" showCount maxLength={255} />
        </Form.Item>

        <Form.Item
          name="name"
          label="名称"
          rules={[
            { required: true, message: '请输入名称' },
            { min: 1, message: '名称不能为空' },
            { max: 255, message: '名称不能超过255个字符' },
            { whitespace: true, message: '名称不能仅为空白字符' },
            {
              validator: async (_, value) => {
                if (!value) return Promise.resolve();
                // 检查名称是否已存在（排除当前编辑的 line）
                const existing = lines.find(
                  (l) => l.name.toLowerCase() === value.toLowerCase().trim() && l.id !== line?.id
                );
                if (existing) {
                  return Promise.reject(new Error('线路名称已存在'));
                }
                return Promise.resolve();
              },
            },
          ]}
        >
          <Input placeholder="输入线路名称" showCount maxLength={255} />
        </Form.Item>
      </Form>
    </Modal>
  );
}
