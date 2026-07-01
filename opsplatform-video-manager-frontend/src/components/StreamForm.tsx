// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

import { useState, useEffect } from 'react';
import { Modal, Form, Input, Select, message } from 'antd';
import { streamAPI, providerAPI } from '../lib/api';
import type { Stream, CDNProvider } from '../lib/api';
import { selectSearchableProps } from '../lib/selectSearchProps';
import { getApiErrorMessage, isAntdFormValidateError } from '../lib/httpError';

interface StreamFormProps {
  stream: Stream | null;
  streams?: Stream[]; // 用于前端唯一性验证
  open: boolean;
  onClose: () => void;
  onSubmit: () => void;
}

export default function StreamForm({ stream, streams = [], open, onClose, onSubmit }: StreamFormProps) {
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(false);
  const [providers, setProviders] = useState<CDNProvider[]>([]);
  const [loadingProviders, setLoadingProviders] = useState(false);

  useEffect(() => {
    if (open) {
      loadProviders();
      if (stream) {
        form.setFieldsValue({
          name: stream.name,
          code: stream.code,
          provider_id: stream.provider_id || undefined,
        });
      } else {
        form.resetFields();
      }
    }
  }, [open, stream, form]);

  const loadProviders = async () => {
    try {
      setLoadingProviders(true);
      const data = await providerAPI.getAll();
      setProviders(data || []);
    } catch {
      message.error('加载厂商列表失败');
    } finally {
      setLoadingProviders(false);
    }
  };

  const handleSubmit = async () => {
    try {
      const values = await form.validateFields();
      setLoading(true);

      // Convert empty string to null for provider_id
      const submitData = {
        ...values,
        provider_id: values.provider_id || null,
      };

      if (stream) {
        await streamAPI.update(stream.id, submitData);
        message.success('视频流区域更新成功');
      } else {
        await streamAPI.create(submitData);
        message.success('视频流区域创建成功');
      }

      onSubmit();
      onClose();
    } catch (err: unknown) {
      if (isAntdFormValidateError(err)) {
        return;
      }
      message.error(getApiErrorMessage(err, '保存失败视频流区域'));
    } finally {
      setLoading(false);
    }
  };

  return (
    <Modal
      title={stream ? '编辑视频流区域' : '创建视频流区域'}
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
            { required: true, message: '请输入视频流区域名称' },
            { min: 1, message: '名称不能为空' },
            { max: 255, message: '名称不能超过255个字符' },
            { whitespace: true, message: '名称不能仅为空白字符' },
            {
              validator: async (_, value) => {
                if (!value) return Promise.resolve();
                // 检查名称是否已存在（排除当前编辑的 stream）
                const existing = streams.find(
                  (s) => s.name.toLowerCase() === value.toLowerCase().trim() && s.id !== stream?.id
                );
                if (existing) {
                  return Promise.reject(new Error('视频流区域名称已存在'));
                }
                return Promise.resolve();
              },
            },
          ]}
        >
          <Input placeholder="输入 stream name (e.g., Asia Region)" showCount maxLength={255} />
        </Form.Item>

        <Form.Item
          name="code"
          label="代码"
          rules={[
            { required: true, message: '请输入视频流区域代码' },
            { min: 1, message: '代码不能为空' },
            { max: 100, message: '代码不能超过100个字符' },
            { pattern: /^[a-zA-Z0-9_-]+$/, message: '代码只能包含字母、数字、下划线和连字符' },
            { whitespace: true, message: '代码不能仅为空白字符' },
          ]}
        >
          <Input placeholder="输入 stream code (e.g., kkw, eu2, eu3)" showCount maxLength={100} />
        </Form.Item>

        <Form.Item
          name="provider_id"
          label="关联厂商"
          tooltip="选择特定厂商时，该流区域只会匹配该厂商的线路；不选择时，会匹配所有厂商的线路"
        >
          <Select
            placeholder="选择厂商（可选，留空则匹配所有厂商）"
            allowClear
            loading={loadingProviders}
            {...selectSearchableProps}
          >
            {providers.map((provider) => (
              <Select.Option key={provider.id} value={provider.id} label={`${provider.name} ${provider.code}`}>
                {provider.name} ({provider.code})
              </Select.Option>
            ))}
          </Select>
        </Form.Item>
      </Form>
    </Modal>
  );
}

