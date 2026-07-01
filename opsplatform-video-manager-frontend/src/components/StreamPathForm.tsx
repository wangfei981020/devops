// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

import { useState, useEffect } from 'react';
import { Modal, Form, Input, Select, message } from 'antd';
import { streamPathAPI } from '../lib/api';
import type { StreamPath, Stream } from '../lib/api';
import { selectSearchableProps } from '../lib/selectSearchProps';
import { streamSeriesLabel } from '../lib/streamSeries';
import { getApiErrorMessage, isAntdFormValidateError } from '../lib/httpError';

interface StreamPathFormProps {
  path: StreamPath | null;
  streams: Stream[];
  paths?: StreamPath[]; // 用于前端唯一性验证
  open: boolean;
  onClose: () => void;
  onSubmit: () => void;
}

export default function StreamPathForm({ path, streams, paths = [], open, onClose, onSubmit }: StreamPathFormProps) {
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (open) {
      if (path) {
        form.setFieldsValue({
          stream_id: path.stream_id,
          table_id: path.table_id,
          full_path: path.full_path,
        });
      } else {
        form.resetFields();
        if (streams.length > 0) {
          form.setFieldsValue({ stream_id: streams[0].id });
        }
      }
    }
  }, [open, path, streams, form]);

  const handleSubmit = async () => {
    try {
      const values = await form.validateFields();
      setLoading(true);

      if (path) {
        await streamPathAPI.update(path.id, {
          stream_id: values.stream_id,
          table_id: values.table_id,
          full_path: values.full_path,
        });
        message.success('Stream path 更新成功');
      } else {
        await streamPathAPI.create({
          stream_id: values.stream_id,
          table_id: values.table_id,
          full_path: values.full_path,
        });
        message.success('Stream path 创建成功');
      }

      onSubmit();
      onClose();
    } catch (err: unknown) {
      if (isAntdFormValidateError(err)) {
        return;
      }
      message.error(getApiErrorMessage(err, '保存失败 stream path'));
    } finally {
      setLoading(false);
    }
  };

  return (
    <Modal
      title={path ? '编辑 Stream Path' : '创建 Stream Path'}
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
          name="stream_id"
          label="视频流区域"
          rules={[{ required: true, message: '请选择视频流区域' }]}
        >
          <Select placeholder="选择视频流区域" {...selectSearchableProps}>
            {streams.map((stream) => (
              <Select.Option
                key={stream.id}
                value={stream.id}
                label={`${stream.name} ${stream.code}`}
              >
                {stream.name} ({stream.code})
              </Select.Option>
            ))}
          </Select>
        </Form.Item>

        <Form.Item
          name="table_id"
          label="桌台号"
          rules={[
            { required: true, message: '请输入桌台号' },
            { min: 1, message: '桌台号不能为空' },
            { max: 255, message: '桌台号不能超过255个字符' },
            { whitespace: true, message: '桌台号不能仅为空白字符' },
            {
              validator: async (_, value) => {
                if (!value) return Promise.resolve();
                // 检查桌台号是否已存在（排除当前编辑的 path）
                const existing = paths.find(
                  (p) => p.table_id.toLowerCase() === value.toLowerCase().trim() && p.id !== path?.id
                );
                if (existing) {
                  return Promise.reject(new Error('桌台号已存在'));
                }
                return Promise.resolve();
              },
            },
          ]}
        >
          <Input placeholder="输入桌台号 (e.g., L001, D001)" showCount maxLength={255} />
        </Form.Item>

        <Form.Item noStyle shouldUpdate={(prev, cur) => prev.table_id !== cur.table_id}>
          {() => {
            const tid = form.getFieldValue('table_id') as string | undefined;
            const text = streamSeriesLabel(String(tid ?? '').trim()) || '—';
            return (
              <Form.Item
                label="系列"
                tooltip="由桌台号自动推导：开头连续字母 +「系列」（如 L001 → L系列）。无需手动填写。"
              >
                <Input readOnly value={text} tabIndex={-1} />
              </Form.Item>
            );
          }}
        </Form.Item>

        <Form.Item
          name="full_path"
          label="路径"
          rules={[
            { required: true, message: '请输入路径' },
            { min: 1, message: '路径不能为空' },
            { max: 500, message: '路径不能超过500个字符' },
            { whitespace: true, message: '路径不能仅为空白字符' },
          ]}
        >
          <Input placeholder="输入 path (e.g., k03/k001)" showCount maxLength={500} />
        </Form.Item>
      </Form>
    </Modal>
  );
}

