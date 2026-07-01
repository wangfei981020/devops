// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

import { useState, useEffect, useCallback } from 'react';
import { Modal, Form, Select, message, Switch } from 'antd';
import { videoStreamEndpointAPI, streamPathAPI } from '../lib/api';
import type { VideoStreamEndpoint, CDNProvider, CDNLine, Domain, Stream, StreamPath } from '../lib/api';
import { selectSearchableProps } from '../lib/selectSearchProps';
import { getApiErrorMessage, isAntdFormValidateError } from '../lib/httpError';

interface VideoStreamEndpointFormProps {
  endpoint: VideoStreamEndpoint | null;
  providers: CDNProvider[];
  lines: CDNLine[];
  domains: Domain[];
  streams: Stream[];
  open: boolean;
  onClose: () => void;
  onSubmit: () => void;
}

export default function VideoStreamEndpointForm({
  endpoint,
  providers,
  lines,
  domains,
  streams,
  open,
  onClose,
  onSubmit,
}: VideoStreamEndpointFormProps) {
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(false);
  const [availablePaths, setAvailablePaths] = useState<StreamPath[]>([]);
  const [selectedStreamId, setSelectedStreamId] = useState<number | undefined>(undefined);

  const loadPaths = useCallback(
    async (streamId: number) => {
      try {
        const paths = await streamPathAPI.getAll(streamId);
        setAvailablePaths(paths);
        if (paths.length > 0 && !endpoint) {
          form.setFieldsValue({ stream_path_id: paths[0].id });
        }
      } catch (err: unknown) {
        message.error(getApiErrorMessage(err, 'Failed to load stream paths'));
      }
    },
    [endpoint, form]
  );

  useEffect(() => {
    if (open) {
      if (endpoint) {
        form.setFieldsValue({
          provider_id: endpoint.provider_id,
          line_id: endpoint.line_id,
          domain_id: endpoint.domain_id,
          stream_id: endpoint.stream_id,
          stream_path_id: endpoint.stream_path_id,
          status: endpoint.status === 1,
        });
        setSelectedStreamId(endpoint.stream_id);
        loadPaths(endpoint.stream_id);
      } else {
        form.resetFields();
        if (providers.length > 0 && lines.length > 0 && domains.length > 0 && streams.length > 0) {
          form.setFieldsValue({
            provider_id: providers[0].id,
            line_id: lines[0].id,
            domain_id: domains[0].id,
            stream_id: streams[0].id,
            status: true,
          });
          setSelectedStreamId(streams[0].id);
          loadPaths(streams[0].id);
        }
      }
    }
  }, [open, endpoint, providers, lines, domains, streams, form, loadPaths]);

  const handleStreamChange = (streamId: number) => {
    setSelectedStreamId(streamId);
    form.setFieldsValue({ stream_path_id: undefined });
    loadPaths(streamId);
  };

  const handleSubmit = async () => {
    try {
      const values = await form.validateFields();
      setLoading(true);

      const data = {
        provider_id: values.provider_id,
        line_id: values.line_id,
        domain_id: values.domain_id,
        stream_id: values.stream_id,
        stream_path_id: values.stream_path_id,
        status: values.status ? 1 : 0,
      };

      if (endpoint) {
        await videoStreamEndpointAPI.update(endpoint.id, data);
        message.success('Endpoint updated successfully');
      } else {
        await videoStreamEndpointAPI.create(data);
        message.success('Endpoint created successfully');
      }

      onSubmit();
      onClose();
    } catch (err: unknown) {
      if (isAntdFormValidateError(err)) {
        return;
      }
      message.error(getApiErrorMessage(err, 'Failed to save endpoint'));
    } finally {
      setLoading(false);
    }
  };

  // Filter lines by selected provider
  const filteredLines = form.getFieldValue('provider_id')
    ? lines.filter((l) => l.provider_id === form.getFieldValue('provider_id'))
    : lines;

  return (
    <Modal
      title={endpoint ? 'Edit Video Stream Endpoint' : 'Create Video Stream Endpoint'}
      open={open}
      onOk={handleSubmit}
      onCancel={onClose}
      confirmLoading={loading}
      okText="Save"
      cancelText="Cancel"
      width={600}
    >
      <Form
        form={form}
        layout="vertical"
        autoComplete="off"
      >
        <Form.Item
          name="provider_id"
          label="Provider"
          rules={[{ required: true, message: 'Please select a provider' }]}
        >
          <Select
            placeholder="Select a provider"
            onChange={() => {
              form.setFieldsValue({ line_id: undefined });
            }}
            {...selectSearchableProps}
          >
            {providers.map((provider) => (
              <Select.Option
                key={provider.id}
                value={provider.id}
                label={`${provider.name} ${provider.code}`}
              >
                {provider.name} ({provider.code})
              </Select.Option>
            ))}
          </Select>
        </Form.Item>

        <Form.Item
          name="line_id"
          label="CDN Line"
          rules={[{ required: true, message: 'Please select a CDN line' }]}
          dependencies={['provider_id']}
        >
          <Select placeholder="Select a CDN line" {...selectSearchableProps}>
            {filteredLines.map((line) => (
              <Select.Option
                key={line.id}
                value={line.id}
                label={`${line.name} ${line.code}`}
              >
                {line.name} ({line.code})
              </Select.Option>
            ))}
          </Select>
        </Form.Item>

        <Form.Item
          name="domain_id"
          label="Domain"
          rules={[{ required: true, message: 'Please select a domain' }]}
        >
          <Select placeholder="Select a domain" {...selectSearchableProps}>
            {domains.map((domain) => (
              <Select.Option key={domain.id} value={domain.id} label={domain.name}>
                {domain.name}
              </Select.Option>
            ))}
          </Select>
        </Form.Item>

        <Form.Item
          name="stream_id"
          label="Stream"
          rules={[{ required: true, message: 'Please select a stream' }]}
        >
          <Select
            placeholder="Select a stream"
            onChange={handleStreamChange}
            {...selectSearchableProps}
          >
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
          name="stream_path_id"
          label="Stream Path"
          rules={[{ required: true, message: 'Please select a stream path' }]}
        >
          <Select
            placeholder="Select a stream path"
            disabled={!selectedStreamId || availablePaths.length === 0}
            loading={!selectedStreamId}
            {...selectSearchableProps}
          >
            {availablePaths.map((path) => (
              <Select.Option
                key={path.id}
                value={path.id}
                label={`${path.full_path} ${path.table_id}`}
              >
                {path.full_path} ({path.table_id})
              </Select.Option>
            ))}
          </Select>
        </Form.Item>

        <Form.Item
          name="status"
          label="Status"
          valuePropName="checked"
          initialValue={true}
        >
          <Switch checkedChildren="Enabled" unCheckedChildren="Disabled" />
        </Form.Item>
      </Form>
    </Modal>
  );
}

