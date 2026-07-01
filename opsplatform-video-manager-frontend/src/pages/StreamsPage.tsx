// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

import { useState, useEffect, useMemo, useCallback } from 'react';
import { Button, Table, Space, message, Popconfirm, Input, Card, Statistic, Row, Col, Modal, Descriptions } from 'antd';
import { PlusOutlined, EditOutlined, DeleteOutlined, SearchOutlined, ReloadOutlined, EyeOutlined, ExportOutlined } from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';
import { streamAPI } from '../lib/api';
import type { Stream } from '../lib/api';
import StreamForm from '../components/StreamForm';
import { getApiErrorMessage } from '../lib/httpError';

const { Search } = Input;

export default function StreamsPage() {
  const [streams, setStreams] = useState<Stream[]>([]);
  const [filteredStreams, setFilteredStreams] = useState<Stream[]>([]);
  const [loading, setLoading] = useState(true);
  const [showForm, setShowForm] = useState(false);
  const [editingStream, setEditingStream] = useState<Stream | null>(null);
  const [viewingStream, setViewingStream] = useState<Stream | null>(null);
  const [searchText, setSearchText] = useState('');
  const [selectedRowKeys, setSelectedRowKeys] = useState<React.Key[]>([]);
  const [pagination, setPagination] = useState({ current: 1, pageSize: 10 });

  const loadStreams = useCallback(async () => {
    try {
      setLoading(true);
      const data = await streamAPI.getAll();
      setStreams(data || []);
    } catch (err: unknown) {
      message.error(getApiErrorMessage(err, '加载失败 streams'));
      setStreams([]);
    } finally {
      setLoading(false);
    }
  }, []);

  const filterStreams = useCallback(() => {
    if (!searchText.trim()) {
      setFilteredStreams(streams || []);
      return;
    }

    const filtered = (streams || []).filter(
      (stream) =>
        stream.name.toLowerCase().includes(searchText.toLowerCase()) ||
        stream.code.toLowerCase().includes(searchText.toLowerCase()) ||
        stream.provider?.name.toLowerCase().includes(searchText.toLowerCase()) ||
        stream.provider?.code.toLowerCase().includes(searchText.toLowerCase())
    );
    setFilteredStreams(filtered);
  }, [searchText, streams]);

  useEffect(() => {
    void loadStreams();
  }, [loadStreams]);

  useEffect(() => {
    filterStreams();
  }, [filterStreams]);

  const handleCreate = () => {
    setEditingStream(null);
    setShowForm(true);
  };

  const handleEdit = (stream: Stream) => {
    setEditingStream(stream);
    setShowForm(true);
  };

  const handleView = async (id: number) => {
    try {
      const stream = await streamAPI.getById(id);
      setViewingStream(stream);
    } catch (err: unknown) {
      message.error(getApiErrorMessage(err, '加载失败 stream details'));
    }
  };

  const handleDelete = async (id: number) => {
    try {
      await streamAPI.delete(id);
      message.success('Stream 删除成功');
      await loadStreams();
    } catch (err: unknown) {
      message.error(getApiErrorMessage(err, '删除失败 stream'));
    }
  };

  const handleBatchDelete = async () => {
    if (selectedRowKeys.length === 0) {
      message.warning('请至少选择一个视频流区域');
      return;
    }

    let successCount = 0;
    let failCount = 0;
    const errors: string[] = [];

    for (const key of selectedRowKeys) {
      try {
        await streamAPI.delete(Number(key));
        successCount++;
      } catch (err: unknown) {
        failCount++;
        errors.push(`ID ${key}: ${getApiErrorMessage(err, '删除失败')}`);
      }
    }

    if (successCount > 0) {
      message.success(`成功删除 ${successCount} 个视频流区域`);
    }
    if (failCount > 0) {
      message.error(`删除失败 ${failCount} 个视频流区域：${errors.join('; ')}`);
    }

    setSelectedRowKeys([]);
    await loadStreams();
  };

  const handleFormSubmit = async () => {
    setShowForm(false);
    setEditingStream(null);
    await loadStreams();
  };

  const handleExport = () => {
    const csvContent = [
      ['编号', '名称', '代码', '提供商', '创建时间', '更新时间'].join(','),
      ...(filteredStreams || []).map((s) =>
        [
          s.id,
          `"${s.name}"`,
          `"${s.code}"`,
          `"${s.provider?.name || '所有厂商'}"`,
          new Date(s.created_at).toISOString(),
          new Date(s.updated_at).toISOString(),
        ].join(',')
      ),
    ].join('\n');

    // Add UTF-8 BOM to fix Chinese character encoding issues in Excel
    const blob = new Blob(['\uFEFF' + csvContent], { type: 'text/csv;charset=utf-8;' });
    const link = document.createElement('a');
    const url = URL.createObjectURL(blob);
    link.setAttribute('href', url);
    link.setAttribute('download', `streams-${new Date().toISOString().split('T')[0]}.csv`);
    link.style.visibility = 'hidden';
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    message.success('Data 导出成功');
  };

  const rowSelection = {
    selectedRowKeys,
    onChange: (selectedKeys: React.Key[]) => {
      setSelectedRowKeys(selectedKeys);
    },
  };

  const columns: ColumnsType<Stream> = [
    {
      title: 'ID',
      dataIndex: 'id',
      key: 'id',
      width: 80,
      sorter: (a, b) => a.id - b.id,
    },
    {
      title: '名称',
      dataIndex: 'name',
      key: 'name',
      sorter: (a, b) => a.name.localeCompare(b.name),
    },
    {
      title: '代码',
      dataIndex: 'code',
      key: 'code',
      sorter: (a, b) => a.code.localeCompare(b.code),
    },
    {
      title: '关联厂商',
      dataIndex: 'provider',
      key: 'provider',
      render: (provider: Stream['provider']) => {
        if (provider) {
          return provider.name;
        }
        return <span style={{ color: '#999' }}>所有厂商</span>;
      },
      sorter: (a, b) => {
        const aName = a.provider?.name || '';
        const bName = b.provider?.name || '';
        return aName.localeCompare(bName);
      },
    },
    {
      title: '创建时间',
      dataIndex: 'created_at',
      key: 'created_at',
      render: (date: string) => new Date(date).toLocaleString(),
      sorter: (a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime(),
    },
    {
      title: '操作',
      key: 'actions',
      width: 200,
      render: (_, record) => (
        <Space>
          <Button
            type="link"
            icon={<EyeOutlined />}
            onClick={() => handleView(record.id)}
          >
            查看
          </Button>
          <Button
            type="link"
            icon={<EditOutlined />}
            onClick={() => handleEdit(record)}
          >
            编辑
          </Button>
          <Popconfirm
            title="删除视频流区域"
            description={`确定要删除 "${record.name}" (${record.code})?`}
            onConfirm={() => handleDelete(record.id)}
            okText="确定"
            cancelText="取消"
            okButtonProps={{ danger: true }}
          >
            <Button
              type="link"
              danger
              icon={<DeleteOutlined />}
            >
              删除
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  const stats = useMemo(() => {
    return {
      total: (streams || []).length,
      filtered: (filteredStreams || []).length,
    };
  }, [streams, filteredStreams]);

  return (
    <div className="vm-page">
      <Row gutter={16} className="vm-stat-row" style={{ marginBottom: 16 }}>
        <Col span={6}>
          <Card>
            <Statistic title="视频流区域总数" value={stats.total} />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic title="筛选结果" value={stats.filtered} />
          </Card>
        </Col>
      </Row>

      <div
        className="vm-toolbar-panel"
        style={{
          marginBottom: 16,
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          flexWrap: 'wrap',
          gap: 12,
        }}
      >
        <h2 className="vm-page-title">视频流区域</h2>
        <Space wrap>
          <Search
            placeholder="搜索名称、代码或厂商"
            allowClear
            style={{ width: 250 }}
            prefix={<SearchOutlined />}
            onSearch={setSearchText}
            onChange={(e) => setSearchText(e.target.value)}
          />
          <Button
            icon={<ReloadOutlined />}
            onClick={loadStreams}
            loading={loading}
          >
            刷新
          </Button>
          <Button
            icon={<ExportOutlined />}
            onClick={handleExport}
            disabled={(filteredStreams || []).length === 0}
          >
            导出
          </Button>
          {selectedRowKeys.length > 0 && (
            <Popconfirm
              title={`删除 ${selectedRowKeys.length} 个视频流区域?`}
              description="此操作无法撤销."
              onConfirm={handleBatchDelete}
              okText="是"
              cancelText="否"
              okButtonProps={{ danger: true }}
            >
              <Button danger icon={<DeleteOutlined />}>
                删除 Selected ({selectedRowKeys.length})
              </Button>
            </Popconfirm>
          )}
          <Button
            type="primary"
            icon={<PlusOutlined />}
            onClick={handleCreate}
          >
            创建 Stream
          </Button>
        </Space>
      </div>

      <Table
        columns={columns}
        dataSource={filteredStreams || []}
        rowKey="id"
        loading={loading}
        rowSelection={rowSelection}
        pagination={{
          current: pagination.current,
          pageSize: pagination.pageSize,
          showSizeChanger: true,
          showQuickJumper: true,
          showTotal: (total, range) => `第 ${range[0]}-${range[1]} 条，共 ${total} 个视频流区域`,
          pageSizeOptions: ['10', '20', '50', '100'],
          onChange: (page, size) => {
            setPagination({ current: page, pageSize: size });
          },
          onShowSizeChange: (_current, size) => {
            setPagination({ current: 1, pageSize: size });
          },
        }}
        scroll={{ x: 'max-content' }}
      />

      {showForm && (
        <StreamForm
          stream={editingStream}
          streams={streams}
          open={showForm}
          onClose={() => {
            setShowForm(false);
            setEditingStream(null);
          }}
          onSubmit={handleFormSubmit}
        />
      )}

      <Modal
        title="视频流区域详情"
        open={viewingStream !== null}
        onCancel={() => setViewingStream(null)}
        footer={[
          <Button key="close" onClick={() => setViewingStream(null)}>
            关闭
          </Button>,
          <Button
            key="edit"
            type="primary"
            onClick={() => {
              if (viewingStream) {
                setViewingStream(null);
                handleEdit(viewingStream);
              }
            }}
          >
            编辑
          </Button>,
        ]}
      >
        {viewingStream && (
          <Descriptions column={1} bordered>
            <Descriptions.Item label="ID">{viewingStream.id}</Descriptions.Item>
            <Descriptions.Item label="名称">{viewingStream.name}</Descriptions.Item>
            <Descriptions.Item label="代码">{viewingStream.code}</Descriptions.Item>
            <Descriptions.Item label="关联厂商">
              {viewingStream.provider ? (
                `${viewingStream.provider.name} (${viewingStream.provider.code})`
              ) : (
                <span style={{ color: '#999' }}>所有厂商（匹配所有厂商的相同代码线路）</span>
              )}
            </Descriptions.Item>
            <Descriptions.Item label="创建时间">
              {new Date(viewingStream.created_at).toLocaleString()}
            </Descriptions.Item>
            <Descriptions.Item label="更新时间">
              {new Date(viewingStream.updated_at).toLocaleString()}
            </Descriptions.Item>
          </Descriptions>
        )}
      </Modal>
    </div>
  );
}

